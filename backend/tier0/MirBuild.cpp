// MirBuild.cpp: the llvm dialect function to MIR (Mir.h).
//
// One walk over the blocks in order. Every llvm value maps to its MIR
// pieces: one value for a scalar, the flatten pieces for an aggregate, two
// 64-bit words (low first) for an i128. On the way: copies are forwarded,
// constants folded, simple identities applied, pure operations with equal
// operands in a block shared (CSE), constant offsets folded into loads and
// stores; at the end unused pure operations are removed.
#include "Abi.h"
#include "Mir.h"

#include "mlir/Interfaces/SideEffectInterfaces.h"
#include "llvm/ADT/Hashing.h"

#include <cstdlib>
#include <cstring>
#include <functional>
#include <tuple>
#include <unordered_set>

namespace marmot {
namespace tier0 {
namespace mir {
namespace {

using Pieces = llvm::SmallVector<VId, 4>;

// Pieces of a type: (offset, scalar type); i128 is two i64 words.
void pieceTypes(MLIRContext *ctx, Type t, uint64_t base, std::vector<std::pair<uint64_t, Type>> &out) {
  if (auto s = dyn_cast<LL::LLVMStructType>(t)) {
    auto body = s.getBody();
    for (unsigned i = 0; i < body.size(); i++) pieceTypes(ctx, body[i], base + fieldOffset(s, i), out);
    return;
  }
  if (auto a = dyn_cast<LL::LLVMArrayType>(t)) {
    uint64_t es = sizeOf(a.getElementType());
    for (unsigned i = 0; i < a.getNumElements(); i++) pieceTypes(ctx, a.getElementType(), base + i * es, out);
    return;
  }
  if (isWide(t)) {
    Type w = IntegerType::get(ctx, 64);
    out.push_back({base, w});
    out.push_back({base + 8, w});
    return;
  }
  if (sizeOf(t) > 8) unsupported("mir: scalar wider than 64 bits");
  out.push_back({base, t});
}

constexpr size_t kMaxPieces = 32;

struct CseKey {
  uint8_t op, w;
  uint16_t aux;
  VId a, b, c;
  int64_t imm;
  uint32_t sym;
  bool operator==(const CseKey &o) const {
    return op == o.op && w == o.w && aux == o.aux && a == o.a && b == o.b && c == o.c && imm == o.imm && sym == o.sym;
  }
};

struct CseInfo {
  static CseKey getEmptyKey() { return {255, 0, 0, None, None, None, 0, 0}; }
  static CseKey getTombstoneKey() { return {254, 0, 0, None, None, None, 0, 0}; }
  static unsigned getHashValue(const CseKey &k) {
    return unsigned(llvm::hash_combine(k.op, k.w, k.aux, k.a, k.b, k.c, k.imm, k.sym));
  }
  static bool isEqual(const CseKey &a, const CseKey &b) { return a == b; }
};

uint64_t mask(unsigned w) { return w >= 64 ? ~uint64_t(0) : (uint64_t(1) << w) - 1; }
int64_t sext(uint64_t v, unsigned w) { return w >= 64 ? int64_t(v) : int64_t(v << (64 - w)) >> (64 - w); }

class Builder {
public:
  Builder(ModuleGen &mg, LL::LLVMFuncOp fn, Func &f, bool x64) : mg(mg), fn(fn), f(f), x64(x64) {
    ctx = fn.getContext();
  }
  void run();

private:
  ModuleGen &mg;
  LL::LLVMFuncOp fn;
  Func &f;
  bool x64;
  MLIRContext *ctx;

  llvm::DenseMap<Value, VId> scalar;                  // scalar llvm values
  llvm::DenseMap<Value, Pieces> agg;                  // aggregates and i128
  llvm::DenseMap<Block *, uint32_t> bidx;
  llvm::DenseMap<CseKey, VId, CseInfo> cse;           // this block's pure operations
  llvm::DenseSet<Operation *> skip;                   // loads made at their only user
  mlir::Operation *cur = nullptr;                     // the llvm operation being translated

  // ---- values and instructions ----
  VId newVal(Cls c, unsigned w, bool ptr) {
    Val v;
    v.cls = c;
    v.width = uint8_t(w);
    v.ptr = ptr;
    f.vals.push_back(v);
    return VId(f.vals.size() - 1);
  }
  static void clsOf(Type t, Cls &c, unsigned &w, bool &ptr) {
    ptr = isa<LL::LLVMPointerType>(t);
    if (t.isF32()) {
      c = Cls::F32;
      w = 32;
    } else if (t.isF64()) {
      c = Cls::F64;
      w = 64;
    } else {
      c = Cls::Int;
      w = intWidth(t);
      if (w > 64) unsupported("mir: integer wider than 64 bits");
    }
  }
  void use(VId v) {
    if (v != None) f.vals[v].uses++;
  }
  // Appends inst (operands counted); a pure one with a result is shared
  // with an equal one earlier in the block.
  VId emit(Inst in, Cls c = Cls::Int, unsigned w = 64, bool ptr = false) {
    in.src = cur;
    bool pure = isPure(in.op) && in.op != Op::Copy;
    CseKey key{};
    if (pure) {
      key = {uint8_t(in.op), in.w, in.aux, in.a, in.b, in.c, in.imm, in.sym};
      if (auto it = cse.find(key); it != cse.end()) return it->second;
    }
    use(in.a);
    use(in.b);
    use(in.c);
    if (in.dst == None && (pure || in.op == Op::Load || in.op == Op::AtomicRMW || in.op == Op::CmpXchg || in.op == Op::FrameAddr)) {
      in.dst = newVal(c, w, ptr);
    }
    if (in.dst != None) f.vals[in.dst].def = uint32_t(f.insts.size());
    f.insts.push_back(in);
    if (pure) cse[key] = in.dst;
    return in.dst;
  }
  VId konst(uint64_t bits, unsigned w = 64, Cls c = Cls::Int) {
    Inst in{Op::Const};
    in.w = uint8_t(w);
    in.imm = int64_t(c == Cls::F32 ? bits & 0xFFFFFFFFu : bits & mask(w));
    in.aux = uint16_t(c);
    return emit(in, c, w);
  }
  bool isConst(VId v, uint64_t &bits) const {
    const Val &x = f.vals[v];
    if (x.def == ~0u) return false;
    const Inst &d = f.insts[x.def];
    if (d.op != Op::Const) return false;
    bits = uint64_t(d.imm);
    return true;
  }
  const Inst *defOf(VId v) const {
    const Val &x = f.vals[v];
    return x.def == ~0u ? nullptr : &f.insts[x.def];
  }

  // Integer binary operation of width w, folded where both are constants.
  VId bin(Op op, unsigned w, VId a, VId b, bool ptr = false) {
    uint64_t x, y;
    bool ca = isConst(a, x), cb = isConst(b, y);
    bool comm = op == Op::Add || op == Op::Mul || op == Op::And || op == Op::Or || op == Op::Xor;
    if (ca && !cb && comm) {
      std::swap(a, b);
      std::swap(x, y);
      std::swap(ca, cb);
    }
    uint64_t m = mask(w);
    if (ca && cb) {
      uint64_t r;
      switch (op) {
      case Op::Add: r = x + y; break;
      case Op::Sub: r = x - y; break;
      case Op::Mul: r = x * y; break;
      case Op::And: r = x & y; break;
      case Op::Or: r = x | y; break;
      case Op::Xor: r = x ^ y; break;
      case Op::Shl: r = x << (y & 63); break;
      case Op::LShr: r = x >> (y & 63); break;
      case Op::AShr: r = uint64_t(sext(x, w) >> (y & 63)); break;
      default: goto noFold;
      }
      return konst(r & m, w);
    }
  noFold:
    if (cb) {
      y &= m;
      switch (op) {
      case Op::Add:
      case Op::Sub:
      case Op::Or:
      case Op::Xor:
      case Op::Shl:
      case Op::LShr:
      case Op::AShr:
        if (y == 0) return a;
        break;
      case Op::Mul:
        if (y == 1) return a;
        if (y == 0) return konst(0, w);
        break;
      case Op::And:
        if (y == m) return a;
        if (y == 0) return konst(0, w);
        break;
      default: break;
      }
    }
    Inst in{op};
    in.w = uint8_t(w);
    in.a = a;
    in.b = b;
    return emit(in, Cls::Int, w, ptr);
  }

  VId icmp(LL::ICmpPredicate p, unsigned w, VId a, VId b) {
    uint64_t x, y;
    if (isConst(a, x) && isConst(b, y)) {
      x &= mask(w);
      y &= mask(w);
      int64_t sx = sext(x, w), sy = sext(y, w);
      bool r = false;
      using P = LL::ICmpPredicate;
      switch (p) {
      case P::eq: r = x == y; break;
      case P::ne: r = x != y; break;
      case P::ult: r = x < y; break;
      case P::ule: r = x <= y; break;
      case P::ugt: r = x > y; break;
      case P::uge: r = x >= y; break;
      case P::slt: r = sx < sy; break;
      case P::sle: r = sx <= sy; break;
      case P::sgt: r = sx > sy; break;
      case P::sge: r = sx >= sy; break;
      }
      return konst(r, 1);
    }
    Inst in{Op::ICmp};
    in.w = uint8_t(w);
    in.aux = uint16_t(p);
    in.a = a;
    in.b = b;
    return emit(in, Cls::Int, 1);
  }

  VId select(VId c, VId a, VId b) {
    uint64_t x;
    if (isConst(c, x)) return x & 1 ? a : b;
    if (a == b) return a;
    const Val &va = f.vals[a];
    Inst in{Op::Select};
    in.w = va.width;
    in.a = c;
    in.b = a;
    in.c = b;
    return emit(in, va.cls, va.width, va.ptr);
  }

  VId unary(Op op, VId a, Cls c, unsigned w, unsigned opw = 64, uint16_t aux = 0) {
    Inst in{op};
    in.w = uint8_t(opw);
    in.aux = aux;
    in.a = a;
    return emit(in, c, w);
  }

  VId trunc(VId a, unsigned w) {
    uint64_t x;
    if (isConst(a, x)) return konst(x & mask(w), w);
    if (w >= 64) return a;
    return unary(Op::Trunc, a, Cls::Int, w, w);
  }
  VId sextTo(VId a, unsigned from, unsigned to) {
    uint64_t x;
    if (isConst(a, x)) return konst(uint64_t(sext(x, from)) & mask(to), to);
    if (from >= to) return a;
    return unary(Op::SExt, a, Cls::Int, to, to, uint16_t(from));
  }

  // The address of base + off as a register value and a constant offset:
  // an Add of a constant folds into the offset.
  std::pair<VId, int64_t> addr(VId p) {
    if (const Inst *d = defOf(p); d && d->op == Op::Add && d->w == 64) {
      uint64_t k;
      if (isConst(d->b, k) && int64_t(k) >= INT32_MIN && int64_t(k) <= INT32_MAX) return {d->a, int64_t(k)};
    }
    return {p, 0};
  }

  VId load(VId p, int64_t off, Type t, bool atomic) {
    Cls c;
    unsigned w;
    bool ptr;
    clsOf(t, c, w, ptr);
    Inst in{Op::Load};
    in.w = uint8_t(w);
    in.aux = uint16_t(sizeOf(t));
    in.flags = atomic ? Atomic : 0;
    if (!atomic) {
      auto [b, o] = addr(p);
      in.a = b;
      in.imm = o + off;
    } else {
      if (off) p = bin(Op::Add, 64, p, konst(uint64_t(off)), true);
      in.a = p;
    }
    return emit(in, c, w, ptr);
  }
  void store(VId p, int64_t off, VId v, uint64_t size, bool atomic) {
    Inst in{Op::Store};
    in.aux = uint16_t(size);
    in.flags = atomic ? Atomic : 0;
    if (!atomic) {
      auto [b, o] = addr(p);
      in.a = b;
      in.imm = o + off;
    } else {
      if (off) p = bin(Op::Add, 64, p, konst(uint64_t(off)), true);
      in.a = p;
    }
    in.b = v;
    emit(in);
  }

  // ---- llvm values ----
  std::vector<std::pair<uint64_t, Type>> piecesOf(Type t) {
    std::vector<std::pair<uint64_t, Type>> out;
    pieceTypes(ctx, t, 0, out);
    if (out.size() > kMaxPieces) unsupported("mir: aggregate of more than 32 pieces");
    return out;
  }
  bool isAggOrWide(Type t) { return isAggregate(t) || isWide(t); }
  VId val(Value v) {
    if (auto it = scalar.find(v); it != scalar.end()) return it->second;
    // Constants used before their definition in block order cannot happen
    // (SSA dominance), but operands of skipped operations are still here.
    unsupported("mir: value without a definition");
  }
  const Pieces &pieces(Value v) {
    auto it = agg.find(v);
    if (it == agg.end()) unsupported("mir: aggregate without a definition");
    return it->second;
  }
  void set(Value v, VId x) { scalar[v] = x; }
  void setPieces(Value v, Pieces p) { agg[v] = std::move(p); }
  // Pieces of any value (a scalar is one piece).
  Pieces any(Value v) {
    if (isAggOrWide(v.getType())) return pieces(v);
    return Pieces{val(v)};
  }

  void edge(Block *dest, OperandRange args, uint32_t &out) {
    Edge e;
    e.block = bidx.lookup(dest);
    e.argBegin = uint32_t(f.edgeArgs.size());
    for (Value a : args)
      for (VId p : any(a)) {
        use(p);
        f.edgeArgs.push_back(p);
      }
    e.argEnd = uint32_t(f.edgeArgs.size());
    out = uint32_t(f.edges.size());
    f.edges.push_back(e);
  }

  void op(Operation &o);
  void gep(LL::GEPOp g);
  void call(LL::CallOp c);
  void intrinsic(LL::CallIntrinsicOp c);
  void wide(Operation &o);
  bool fuseLoadStore(LL::LoadOp l);
  void decideNilChecks();
  bool decideByDominators();
  void removeDead();
  void dce();
};

void Builder::run() {
  if (fn.isVarArg()) unsupported("variadic function");
  // Blocks and their parameters first (branches name later blocks).
  for (Block &b : fn.getBody()) {
    bidx[&b] = uint32_t(f.blocks.size());
    f.blocks.emplace_back();
  }
  for (Block &b : fn.getBody()) {
    BB &mb = f.blocks[bidx[&b]];
    for (BlockArgument a : b.getArguments()) {
      Pieces ps;
      for (auto &[off, t] : piecesOf(a.getType())) {
        Cls c;
        unsigned w;
        bool ptr;
        clsOf(t, c, w, ptr);
        VId v = newVal(c, w, ptr);
        mb.params.push_back(v);
        ps.push_back(v);
      }
      if (isAggOrWide(a.getType()))
        setPieces(a, ps);
      else
        set(a, ps[0]);
    }
    for (Block *p : b.getPredecessors()) mb.preds.push_back(bidx[p]);
  }
  f.params = f.blocks[0].params;
  for (Block &b : fn.getBody()) {
    BB &mb = f.blocks[bidx[&b]];
    mb.begin = uint32_t(f.insts.size());
    cse.clear();
    for (Operation &o : b) {
      cur = &o;
      op(o);
    }
    f.blocks[bidx[&b]].end = uint32_t(f.insts.size());
  }
  decideNilChecks();
  dce();
}

// A nil check of a pointer that a check on the only path into its block
// already found non-nil goes straight to the non-nil side; blocks then
// reached by no live edge are dead (their instructions removed).
void Builder::decideNilChecks() {
  static const bool dom = [] {
    const char *e = getenv("MARMOT_TIER0_DOMCHECKS");
    return e && *e == '1';
  }();
  if (dom) {
    if (decideByDominators()) removeDead();
    return;
  }
  const uint32_t nb = uint32_t(f.blocks.size());
  // The pointer a block's terminator checks against nil: (value, is eq,
  // non-nil edge).
  auto nilCheck = [&](uint32_t b, VId &p, uint32_t &nonNil) {
    const BB &bl = f.blocks[b];
    if (bl.end == bl.begin) return false;
    const Inst &t = f.insts[bl.end - 1];
    if (t.op != Op::CondBr) return false;
    const Edge &te = f.edges[t.extra], &fe = f.edges[t.aux2];
    if (te.argEnd != te.argBegin || fe.argEnd != fe.argBegin || te.block == fe.block) return false;
    const Inst *c = defOf(t.a);
    uint64_t k;
    if (!c || c->op != Op::ICmp || c->w != 64 || !isConst(c->b, k) || k != 0) return false;
    auto pr = LL::ICmpPredicate(c->aux);
    if (pr != LL::ICmpPredicate::eq && pr != LL::ICmpPredicate::ne) return false;
    p = c->a;
    nonNil = pr == LL::ICmpPredicate::eq ? t.aux2 : t.extra;
    return true;
  };
  std::vector<llvm::SmallVector<VId, 4>> facts(nb);
  bool any = false;
  for (uint32_t b = 0; b < nb; b++) {
    const BB &bl = f.blocks[b];
    if (bl.preds.size() != 1) continue;
    uint32_t pb = bl.preds[0];
    if (pb >= b) continue;  // facts flow forward in block order only
    facts[b] = facts[pb];
    VId p;
    uint32_t e;
    if (nilCheck(pb, p, e) && f.edges[e].block == b && !llvm::is_contained(facts[b], p)) facts[b].push_back(p);
  }
  for (uint32_t b = 0; b < nb; b++) {
    VId p;
    uint32_t e;
    if (!nilCheck(b, p, e) || !llvm::is_contained(facts[b], p)) continue;
    Inst &t = f.insts[f.blocks[b].end - 1];
    f.vals[t.a].uses--;
    t.op = Op::Br;
    t.a = None;
    t.extra = e;
    any = true;
  }
  if (any) removeDead();
}

// Facts from dominating compares (MARMOT_TIER0_DOMCHECKS=1): a compare
// with the same predicate and operands as one whose branch dominates this
// block on one side is decided. A block with one predecessor learns the
// compare of the edge into it; every block knows what its dominators knew.
// Compares are over SSA values, so a fact holds wherever its block
// dominates. Constants match by value. Returns whether any branch changed.
bool Builder::decideByDominators() {
  using P = LL::ICmpPredicate;
  const uint32_t nb = uint32_t(f.blocks.size());
  // Reverse postorder from the entry over the terminators' edges.
  auto succs = [&](uint32_t b, llvm::SmallVectorImpl<uint32_t> &out) {
    const BB &bl = f.blocks[b];
    if (bl.end == bl.begin) return;
    const Inst &t = f.insts[bl.end - 1];
    if (t.op == Op::Br) out.push_back(f.edges[t.extra].block);
    if (t.op == Op::CondBr) {
      out.push_back(f.edges[t.extra].block);
      out.push_back(f.edges[t.aux2].block);
    }
    if (t.op == Op::Switch)
      for (uint32_t e : f.switches[t.extra].edges) out.push_back(f.edges[e].block);
  };
  std::vector<uint32_t> rpo, order(nb, ~0u);
  {
    std::vector<char> seen(nb, 0);
    std::vector<std::pair<uint32_t, uint32_t>> stack{{0, 0}};
    std::vector<llvm::SmallVector<uint32_t, 4>> ss(nb);
    seen[0] = 1;
    succs(0, ss[0]);
    while (!stack.empty()) {
      auto &[b, i] = stack.back();
      if (i < ss[b].size()) {
        uint32_t s = ss[b][i++];
        if (!seen[s]) {
          seen[s] = 1;
          succs(s, ss[s]);
          stack.push_back({s, 0});
        }
        continue;
      }
      rpo.push_back(b);
      stack.pop_back();
    }
    std::reverse(rpo.begin(), rpo.end());
    for (uint32_t i = 0; i < rpo.size(); i++) order[rpo[i]] = i;
  }
  // Immediate dominators (Cooper, Harvey and Kennedy), as rpo indices.
  std::vector<uint32_t> idom(rpo.size(), ~0u);
  idom[0] = 0;
  for (bool changed = true; changed;) {
    changed = false;
    for (uint32_t i = 1; i < rpo.size(); i++) {
      uint32_t nd = ~0u;
      for (uint32_t p : f.blocks[rpo[i]].preds) {
        uint32_t pi = order[p];
        if (pi == ~0u || idom[pi] == ~0u) continue;
        if (nd == ~0u) {
          nd = pi;
          continue;
        }
        uint32_t a = pi, b = nd;
        while (a != b) {
          while (a > b) a = idom[a];
          while (b > a) b = idom[b];
        }
        nd = a;
      }
      if (nd != idom[i]) {
        idom[i] = nd;
        changed = true;
      }
    }
  }
  // A fact: the compare (pred, width, a, b) is true. An operand is a value
  // or a constant (by its bits).
  struct Key {
    uint8_t pred, w, ka, kb;
    uint64_t a, b;
    bool operator==(const Key &o) const {
      return pred == o.pred && w == o.w && ka == o.ka && kb == o.kb && a == o.a && b == o.b;
    }
  };
  struct KeyHash {
    size_t operator()(const Key &k) const { return llvm::hash_combine(k.pred, k.w, k.ka, k.kb, k.a, k.b); }
  };
  auto inv = [](P p) {
    switch (p) {
    case P::eq: return P::ne;
    case P::ne: return P::eq;
    case P::slt: return P::sge;
    case P::sge: return P::slt;
    case P::sle: return P::sgt;
    case P::sgt: return P::sle;
    case P::ult: return P::uge;
    case P::uge: return P::ult;
    case P::ule: return P::ugt;
    case P::ugt: return P::ule;
    }
    return p;
  };
  auto swp = [](P p) {
    switch (p) {
    case P::slt: return P::sgt;
    case P::sgt: return P::slt;
    case P::sle: return P::sge;
    case P::sge: return P::sle;
    case P::ult: return P::ugt;
    case P::ugt: return P::ult;
    case P::ule: return P::uge;
    case P::uge: return P::ule;
    default: return p;
    }
  };
  auto opnd = [&](VId v, uint8_t &k, uint64_t &x) {
    uint64_t c;
    if (isConst(v, c)) {
      k = 1;
      x = c;
    } else {
      k = 0;
      x = v;
    }
  };
  auto key = [&](P p, const Inst &c, bool swapped) {
    Key k;
    k.pred = uint8_t(swapped ? swp(p) : p);
    k.w = c.w;
    opnd(swapped ? c.b : c.a, k.ka, k.a);
    opnd(swapped ? c.a : c.b, k.kb, k.b);
    return k;
  };
  // The compare a block's conditional branch tests, or null.
  auto condOf = [&](uint32_t b) -> const Inst * {
    const BB &bl = f.blocks[b];
    if (bl.end == bl.begin) return nullptr;
    const Inst &t = f.insts[bl.end - 1];
    if (t.op != Op::CondBr) return nullptr;
    const Inst *c = defOf(t.a);
    return c && c->op == Op::ICmp ? c : nullptr;
  };
  std::unordered_set<Key, KeyHash> facts;
  std::vector<Key> undo;
  auto add = [&](const Key &k) {
    if (facts.insert(k).second) undo.push_back(k);
  };
  // The edge fact of block b: the compare its only predecessor branched
  // on, as the side that leads here.
  auto edgeKey = [&](uint32_t b, Key &out) {
    const BB &bl = f.blocks[b];
    uint32_t pb = ~0u;
    for (uint32_t p : bl.preds)
      if (order[p] != ~0u) {
        if (pb != ~0u) return false;
        pb = p;
      }
    if (pb == ~0u) return false;
    const Inst *c = condOf(pb);
    if (!c) return false;
    const Inst &t = f.insts[f.blocks[pb].end - 1];
    uint32_t tb = f.edges[t.extra].block, fb = f.edges[t.aux2].block;
    if (tb == fb) return false;
    P p = P(c->aux);
    if (b == fb) p = inv(p);
    else if (b != tb) return false;
    out = key(p, *c, false);
    return true;
  };
  // Values known to be at least 0 as signed 64-bit integers: constants
  // with the sign bit clear, values narrower than 64 bits (canonical, zero
  // extended), and a block parameter all of whose incoming values are such
  // constants, the parameter itself, or the parameter plus 1 computed where
  // the parameter is signed less than something (so the add cannot wrap).
  std::vector<uint32_t> paramBlock;
  llvm::DenseMap<VId, char> nn;  // 1 yes, 0 no, 2 in progress (assumed yes)
  std::vector<VId> trail;        // parameters in the order first asked
  auto blockOfInst = [&](uint32_t ix) {
    uint32_t lo = 0, hi = nb;
    while (hi - lo > 1) {
      uint32_t mid = (lo + hi) / 2;
      if (f.blocks[mid].begin <= ix) lo = mid;
      else hi = mid;
    }
    return lo;
  };
  // Whether v <s something holds on entry to block b: a fact on the edge
  // into b or into one of its dominators (at most 64 up).
  auto guardedBelow = [&](VId v, uint32_t b) {
    uint32_t i = order[b];
    for (int n = 0; i != ~0u && n < 64; n++) {
      Key k;
      if (edgeKey(rpo[i], k) && k.w == 64 &&
          ((P(k.pred) == P::slt && k.ka == 0 && k.a == v) || (P(k.pred) == P::sgt && k.kb == 0 && k.b == v)))
        return true;
      if (i == 0) break;
      i = idom[i];
    }
    return false;
  };
  std::function<bool(VId)> nonneg = [&](VId v) -> bool {
    uint64_t k;
    if (isConst(v, k)) return int64_t(k) >= 0;
    const Val &x = f.vals[v];
    if (x.cls != Cls::Int) return false;
    if (x.width < 64) return true;
    if (x.def != ~0u) return false;
    if (paramBlock.empty()) {
      paramBlock.assign(f.vals.size(), ~0u);
      for (uint32_t b = 0; b < nb; b++)
        for (VId p : f.blocks[b].params) paramBlock[p] = b;
    }
    uint32_t h = paramBlock[v];
    if (h == ~0u || h == 0 || order[h] == ~0u) return false;
    auto [it, fresh] = nn.try_emplace(v, 2);
    if (!fresh) return it->second != 0;
    size_t mark0 = trail.size();
    trail.push_back(v);
    uint32_t idx = 0;
    while (f.blocks[h].params[idx] != v) idx++;
    bool ok = true;
    auto arg = [&](uint32_t e) {
      const Edge &ed = f.edges[e];
      if (ed.block != h || !ok) return;
      VId a = f.edgeArgs[ed.argBegin + idx];
      if (a == v) return;
      if (const Inst *d = defOf(a); d && d->op == Op::Add && d->w == 64 && d->a == v && isConst(d->b, k) && k == 1) {
        ok = guardedBelow(v, blockOfInst(f.vals[a].def));
        return;
      }
      ok = nonneg(a);
    };
    for (uint32_t p : f.blocks[h].preds) {
      if (order[p] == ~0u) continue;
      const Inst &t = f.insts[f.blocks[p].end - 1];
      if (t.op == Op::Br) arg(t.extra);
      else if (t.op == Op::CondBr) {
        arg(t.extra);
        arg(t.aux2);
      } else if (t.op == Op::Switch) {
        for (uint32_t e : f.switches[t.extra].edges) arg(e);
      } else ok = false;
    }
    nn[v] = ok;
    // A no: answers given while v was assumed yes may rest on it.
    if (!ok)
      for (size_t i = mark0; i < trail.size(); i++) nn[trail[i]] = 0;
    return ok;
  };
  // A signed order between a nonnegative smaller side and anything is the
  // same unsigned order (the larger side is then nonnegative too).
  auto addWithUnsigned = [&](const Key &k) {
    add(k);
    if (k.w != 64) return;
    P p = P(k.pred), u;
    bool smallA;
    switch (p) {
    case P::slt: u = P::ult; smallA = true; break;
    case P::sle: u = P::ule; smallA = true; break;
    case P::sgt: u = P::ugt; smallA = false; break;
    case P::sge: u = P::uge; smallA = false; break;
    default: return;
    }
    uint8_t ks = smallA ? k.ka : k.kb;
    uint64_t vs = smallA ? k.a : k.b;
    if (ks ? int64_t(vs) >= 0 : nonneg(VId(vs))) {
      Key q = k;
      q.pred = uint8_t(u);
      add(q);
    }
  };
  auto edgeFact = [&](uint32_t b) {
    Key k;
    if (edgeKey(b, k)) addWithUnsigned(k);
  };
  // Dominator tree children, then a preorder walk with an undo log.
  std::vector<std::vector<uint32_t>> kids(rpo.size());
  for (uint32_t i = 1; i < rpo.size(); i++)
    if (idom[i] != ~0u) kids[idom[i]].push_back(i);
  bool any = false;
  std::vector<std::pair<uint32_t, size_t>> walk{{0, 0}};
  std::vector<size_t> mark;
  auto enter = [&](uint32_t i) {
    mark.push_back(undo.size());
    uint32_t b = rpo[i];
    edgeFact(b);
    const Inst *c = condOf(b);
    if (!c) return;
    P p = P(c->aux);
    int decided = -1;
    if (facts.count(key(p, *c, false)) || facts.count(key(p, *c, true))) decided = 1;
    else if (facts.count(key(inv(p), *c, false)) || facts.count(key(inv(p), *c, true))) decided = 0;
    if (decided < 0) return;
    Inst &t = f.insts[f.blocks[b].end - 1];
    uint32_t keep = decided ? t.extra : t.aux2, drop = decided ? t.aux2 : t.extra;
    f.vals[t.a].uses--;
    const Edge &de = f.edges[drop];
    for (uint32_t j = de.argBegin; j < de.argEnd; j++) f.vals[f.edgeArgs[j]].uses--;
    t.op = Op::Br;
    t.a = None;
    t.extra = keep;
    t.aux2 = 0;
    any = true;
  };
  enter(0);
  while (!walk.empty()) {
    auto &[i, k] = walk.back();
    if (k < kids[i].size()) {
      uint32_t c = kids[i][k++];
      enter(c);
      walk.push_back({c, 0});
      continue;
    }
    for (size_t n = mark.back(); undo.size() > n; undo.pop_back()) facts.erase(undo.back());
    mark.pop_back();
    walk.pop_back();
  }
  return any;
}

// Blocks reached by no live edge are dead: their instructions removed and
// their edges forgotten by their successors.
void Builder::removeDead() {
  const uint32_t nb = uint32_t(f.blocks.size());
  // Reachability from the entry over the terminators.
  std::vector<char> live(nb, 0);
  std::vector<uint32_t> work{0};
  live[0] = 1;
  while (!work.empty()) {
    uint32_t b = work.back();
    work.pop_back();
    const BB &bl = f.blocks[b];
    if (bl.end == bl.begin) continue;
    const Inst &t = f.insts[bl.end - 1];
    auto go = [&](uint32_t e) {
      uint32_t s = f.edges[e].block;
      if (!live[s]) {
        live[s] = 1;
        work.push_back(s);
      }
    };
    if (t.op == Op::Br) go(t.extra);
    if (t.op == Op::CondBr) {
      go(t.extra);
      go(t.aux2);
    }
    if (t.op == Op::Switch)
      for (uint32_t e : f.switches[t.extra].edges) go(e);
  }
  auto unuse = [&](VId v) {
    if (v != None) f.vals[v].uses--;
  };
  auto unuseEdge = [&](uint32_t e) {
    const Edge &ed = f.edges[e];
    for (uint32_t i = ed.argBegin; i < ed.argEnd; i++) unuse(f.edgeArgs[i]);
  };
  for (uint32_t b = 0; b < nb; b++) {
    if (live[b]) continue;
    BB &bl = f.blocks[b];
    bl.dead = true;
    for (uint32_t i = bl.begin; i < bl.end; i++) {
      Inst &in = f.insts[i];
      if (in.dead) continue;
      in.dead = true;
      unuse(in.a);
      unuse(in.b);
      unuse(in.c);
      switch (in.op) {
      case Op::Call:
        for (VId v : f.calls[in.extra].args) unuse(v);
        unuse(f.calls[in.extra].callee);
        break;
      case Op::Ret:
        for (VId v : f.rets[in.extra].pieces) unuse(v);
        break;
      case Op::Br: unuseEdge(in.extra); break;
      case Op::CondBr:
        unuseEdge(in.extra);
        unuseEdge(in.aux2);
        break;
      case Op::Switch:
        for (uint32_t e : f.switches[in.extra].edges) unuseEdge(e);
        break;
      default: break;
      }
    }
  }
  // Live blocks forget their dead predecessors.
  for (uint32_t b = 0; b < nb; b++) {
    auto &ps = f.blocks[b].preds;
    ps.erase(std::remove_if(ps.begin(), ps.end(), [&](uint32_t p) { return !live[p]; }), ps.end());
  }
}

// A load of an aggregate whose only use is a store of it in the same block,
// with nothing that may write memory between: one memory copy at the store
// (no pieces). Not for atomic or volatile accesses.
bool Builder::fuseLoadStore(LL::LoadOp l) {
  if (l.getVolatile_() || l.getOrdering() != LL::AtomicOrdering::not_atomic || !l->hasOneUse()) return false;
  auto st = dyn_cast<LL::StoreOp>(*l->getUsers().begin());
  if (!st || st.getValue() != l.getResult() || st->getBlock() != l->getBlock() || st.getVolatile_() ||
      st.getOrdering() != LL::AtomicOrdering::not_atomic)
    return false;
  for (Operation *o = l->getNextNode(); o != st.getOperation(); o = o->getNextNode()) {
    auto mi = dyn_cast<MemoryEffectOpInterface>(o);
    if (!mi || mi.hasEffect<MemoryEffects::Write>()) return false;
  }
  return true;
}

void Builder::op(Operation &o) {
  // i128 arithmetic (two words). Containers of i128 values (select,
  // extractvalue, insertvalue, stores, branches, returns) take the general
  // path, which handles pieces; anything else with an i128 is unsupported.
  if (isa<LL::ConstantOp, LL::ZeroOp, LL::UndefOp, LL::PoisonOp, LL::ZExtOp, LL::SExtOp, LL::TruncOp, LL::AddOp,
          LL::SubOp, LL::AndOp, LL::OrOp, LL::XOrOp, LL::MulOp, LL::ShlOp, LL::LShrOp, LL::AShrOp, LL::LoadOp>(o)) {
    bool w = false;
    for (Type t : o.getResultTypes()) w |= isWide(t);
    for (Type t : o.getOperandTypes()) w |= isWide(t);
    if (w) return wide(o);
  } else if (!isa<LL::SelectOp, LL::ExtractValueOp, LL::InsertValueOp, LL::StoreOp, LL::BrOp, LL::CondBrOp,
                  LL::ReturnOp>(o)) {
    for (Type t : o.getOperandTypes())
      if (isWide(t)) unsupported("i128 " + o.getName().getStringRef().str());
    for (Type t : o.getResultTypes())
      if (isWide(t)) unsupported("i128 " + o.getName().getStringRef().str());
  }
  auto binop = [&](Op k) {
    Value r = o.getResult(0);
    Cls c;
    unsigned w;
    bool ptr;
    clsOf(r.getType(), c, w, ptr);
    set(r, bin(k, w, val(o.getOperand(0)), val(o.getOperand(1)), ptr));
  };
  auto fbin = [&](Op k) {
    Value r = o.getResult(0);
    Inst in{k};
    in.a = val(o.getOperand(0));
    in.b = val(o.getOperand(1));
    Cls c = r.getType().isF64() ? Cls::F64 : Cls::F32;
    set(r, emit(in, c, c == Cls::F64 ? 64 : 32));
  };
  auto fcls = [](Type t) { return t.isF64() ? Cls::F64 : Cls::F32; };
  auto fw = [](Type t) { return t.isF64() ? 64u : 32u; };
  llvm::TypeSwitch<Operation *>(&o)
      .Case<LL::ConstantOp>([&](auto c) {
        Type t = c.getType();
        if (isAggregate(t)) unsupported("aggregate constant");
        if (auto i = dyn_cast<IntegerAttr>(c.getValue())) {
          set(c.getResult(), konst(i.getValue().getZExtValue(), intWidth(t)));
        } else if (auto fa = dyn_cast<FloatAttr>(c.getValue())) {
          uint64_t bits = fa.getValue().bitcastToAPInt().getZExtValue();
          set(c.getResult(), konst(bits, fw(t), fcls(t)));
        } else {
          unsupported("constant kind");
        }
      })
      .Case<LL::ZeroOp, LL::UndefOp, LL::PoisonOp>([&](auto z) {
        Type t = z.getType();
        if (!isAggOrWide(t)) {
          Cls c;
          unsigned w;
          bool ptr;
          clsOf(t, c, w, ptr);
          set(z.getResult(), konst(0, w, c));
          return;
        }
        Pieces ps;
        for (auto &[off, pt] : piecesOf(t)) {
          Cls c;
          unsigned w;
          bool ptr;
          clsOf(pt, c, w, ptr);
          ps.push_back(konst(0, w, c));
        }
        setPieces(z.getResult(), ps);
      })
      .Case<LL::AddressOfOp>([&](auto a) {
        Inst in{Op::Addr};
        in.sym = f.sym(a.getGlobalName());
        set(a.getResult(), emit(in, Cls::Int, 64, true));
      })
      .Case<LL::AddOp>([&](auto) { binop(Op::Add); })
      .Case<LL::SubOp>([&](auto) { binop(Op::Sub); })
      .Case<LL::MulOp>([&](auto) { binop(Op::Mul); })
      .Case<LL::AndOp>([&](auto) { binop(Op::And); })
      .Case<LL::OrOp>([&](auto) { binop(Op::Or); })
      .Case<LL::XOrOp>([&](auto) { binop(Op::Xor); })
      .Case<LL::ShlOp>([&](auto) { binop(Op::Shl); })
      .Case<LL::LShrOp>([&](auto) { binop(Op::LShr); })
      .Case<LL::AShrOp>([&](auto) { binop(Op::AShr); })
      .Case<LL::UDivOp>([&](auto) { binop(Op::UDiv); })
      .Case<LL::SDivOp>([&](auto) { binop(Op::SDiv); })
      .Case<LL::URemOp>([&](auto) { binop(Op::URem); })
      .Case<LL::SRemOp>([&](auto) { binop(Op::SRem); })
      .Case<LL::ICmpOp>([&](auto c) {
        set(c.getResult(), icmp(c.getPredicate(), intWidth(c.getLhs().getType()), val(c.getLhs()), val(c.getRhs())));
      })
      .Case<LL::SelectOp>([&](auto s) {
        VId c = val(s.getCondition());
        if (isAggOrWide(s.getType())) {
          const Pieces &a = pieces(s.getTrueValue()), &b = pieces(s.getFalseValue());
          Pieces r;
          for (size_t i = 0; i < a.size(); i++) r.push_back(select(c, a[i], b[i]));
          setPieces(s.getResult(), r);
          return;
        }
        set(s.getResult(), select(c, val(s.getTrueValue()), val(s.getFalseValue())));
      })
      .Case<LL::ZExtOp>([&](auto c) { set(c.getResult(), val(c.getArg())); })  // already canonical
      .Case<LL::PtrToIntOp, LL::IntToPtrOp, LL::AddrSpaceCastOp>([&](auto c) {
        unsigned w = intWidth(c.getResult().getType());
        set(c.getResult(), w < intWidth(c.getArg().getType()) ? trunc(val(c.getArg()), w) : val(c.getArg()));
      })
      .Case<LL::TruncOp>([&](auto c) { set(c.getResult(), trunc(val(c.getArg()), intWidth(c.getResult().getType()))); })
      .Case<LL::SExtOp>([&](auto c) {
        set(c.getResult(),
            sextTo(val(c.getArg()), intWidth(c.getArg().getType()), intWidth(c.getResult().getType())));
      })
      .Case<LL::BitcastOp>([&](auto c) {
        if (isAggregate(c.getType())) unsupported("aggregate bitcast");
        Type from = c.getArg().getType(), to = c.getType();
        if (isFloat(from) == isFloat(to)) {
          set(c.getResult(), val(c.getArg()));
          return;
        }
        Cls k;
        unsigned w;
        bool ptr;
        clsOf(to, k, w, ptr);
        set(c.getResult(), unary(Op::Bits, val(c.getArg()), k, w));
      })
      .Case<LL::FAddOp>([&](auto) { fbin(Op::FAdd); })
      .Case<LL::FSubOp>([&](auto) { fbin(Op::FSub); })
      .Case<LL::FMulOp>([&](auto) { fbin(Op::FMul); })
      .Case<LL::FDivOp>([&](auto) { fbin(Op::FDiv); })
      .Case<LL::MaximumOp>([&](auto) { fbin(Op::FMax); })
      .Case<LL::MinimumOp>([&](auto) { fbin(Op::FMin); })
      .Case<LL::MaxNumOp>([&](auto) { fbin(Op::FMaxNum); })
      .Case<LL::MinNumOp>([&](auto) { fbin(Op::FMinNum); })
      .Case<LL::FNegOp>([&](auto n) {
        set(n.getResult(), unary(Op::FNeg, val(n.getOperand()), fcls(n.getType()), fw(n.getType())));
      })
      .Case<LL::FCmpOp>([&](auto c) {
        Inst in{Op::FCmp};
        in.aux = uint16_t(c.getPredicate());
        in.a = val(c.getLhs());
        in.b = val(c.getRhs());
        in.w = uint8_t(fw(c.getLhs().getType()));
        set(c.getResult(), emit(in, Cls::Int, 1));
      })
      .Case<LL::SIToFPOp, LL::UIToFPOp>([&](auto c) {
        Op k = isa<LL::SIToFPOp>(c.getOperation()) ? Op::SIToFP : Op::UIToFP;
        set(c.getResult(), unary(k, val(c.getArg()), fcls(c.getType()), fw(c.getType()), intWidth(c.getArg().getType())));
      })
      .Case<LL::FPToSIOp, LL::FPToUIOp>([&](auto c) {
        Op k = isa<LL::FPToSIOp>(c.getOperation()) ? Op::FPToSI : Op::FPToUI;
        unsigned w = intWidth(c.getType());
        set(c.getResult(), unary(k, val(c.getArg()), Cls::Int, w, fw(c.getArg().getType())));
      })
      .Case<LL::FPExtOp>([&](auto c) { set(c.getResult(), unary(Op::FPExt, val(c.getArg()), Cls::F64, 64)); })
      .Case<LL::FPTruncOp>([&](auto c) { set(c.getResult(), unary(Op::FPTrunc, val(c.getArg()), Cls::F32, 32)); })
      .Case<LL::FMAOp, LL::FMulAddOp>([&](auto m) {
        Inst in{isa<LL::FMAOp>(m.getOperation()) ? Op::FMA : Op::FMulAdd};
        in.a = val(m.getA());
        in.b = val(m.getB());
        in.c = val(m.getC());
        set(m.getResult(), emit(in, fcls(m.getType()), fw(m.getType())));
      })
      .Case<LL::LoadOp>([&](auto l) {
        Type t = l.getType();
        bool atomic = l.getOrdering() != LL::AtomicOrdering::not_atomic;
        if (!isAggOrWide(t)) {
          set(l.getResult(), load(val(l.getAddr()), 0, t, atomic));
          return;
        }
        if (atomic) unsupported("atomic aggregate load");
        if (fuseLoadStore(l)) {
          skip.insert(l);
          return;
        }
        VId p = val(l.getAddr());
        Pieces ps;
        for (auto &[off, pt] : piecesOf(t)) ps.push_back(load(p, int64_t(off), pt, false));
        setPieces(l.getResult(), ps);
      })
      .Case<LL::StoreOp>([&](auto s) {
        Type t = s.getValue().getType();
        bool atomic = s.getOrdering() != LL::AtomicOrdering::not_atomic;
        VId p = val(s.getAddr());
        if (!isAggOrWide(t)) {
          store(p, 0, val(s.getValue()), sizeOf(t), atomic);
          return;
        }
        if (atomic) unsupported("atomic aggregate store");
        if (auto l = s.getValue().template getDefiningOp<LL::LoadOp>(); l && skip.count(l)) {
          Inst in{Op::Copy8};
          auto [db, doff] = addr(p);
          auto [sb, soff] = addr(val(l.getAddr()));
          in.a = db;
          in.imm = doff;
          in.b = sb;
          in.imm2 = soff;
          in.aux2 = uint32_t(sizeOf(t));
          emit(in);
          return;
        }
        if (auto z = s.getValue().template getDefiningOp<LL::ZeroOp>(); z && sizeOf(t) > 16) {
          Inst in{Op::Zero};
          auto [db, doff] = addr(p);
          in.a = db;
          in.imm = doff;
          in.aux2 = uint32_t(sizeOf(t));
          emit(in);
          return;
        }
        const Pieces &vs = pieces(s.getValue());
        auto pt = piecesOf(t);
        for (size_t i = 0; i < vs.size(); i++) store(p, int64_t(pt[i].first), vs[i], sizeOf(pt[i].second), false);
      })
      .Case<LL::AllocaOp>([&](auto a) {
        auto c = a.getArraySize().template getDefiningOp<LL::ConstantOp>();
        if (!c) unsupported("dynamic alloca");
        if (a->getBlock() != &fn.getBody().front()) unsupported("alloca outside the entry block");
        uint64_t n = cast<IntegerAttr>(c.getValue()).getValue().getZExtValue();
        uint64_t al = std::max<uint64_t>(a.getAlignment().value_or(0), alignOf(a.getElemType()));
        f.objs.push_back({std::max<uint64_t>(n * sizeOf(a.getElemType()), 1), std::max<uint64_t>(al, 8)});
        Inst in{Op::Frame};
        in.aux = uint16_t(f.objs.size() - 1);
        if (f.objs.size() > 65535) unsupported("mir: too many allocas");
        set(a.getResult(), emit(in, Cls::Int, 64, true));
      })
      .Case<LL::GEPOp>([&](auto g) { gep(g); })
      .Case<LL::ExtractValueOp>([&](auto x) {
        Type ct = x.getContainer().getType();
        auto [off, t] = elementAt(ct, x.getPosition());
        const Pieces &src = pieces(x.getContainer());
        auto pt = piecesOf(ct);
        uint64_t end = off + sizeOf(t);
        Pieces r;
        for (size_t i = 0; i < pt.size(); i++)
          if (pt[i].first >= off && pt[i].first < end) r.push_back(src[i]);
        if (isAggOrWide(t))
          setPieces(x.getResult(), r);
        else
          { if (r.empty()) unsupported("mir: empty extract"); set(x.getResult(), r[0]); }
      })
      .Case<LL::InsertValueOp>([&](auto ins) {
        Type ct = ins.getType();
        auto [off, t] = elementAt(ct, ins.getPosition());
        Pieces r = pieces(ins.getContainer());
        auto pt = piecesOf(ct);
        Pieces v = any(ins.getValue());
        uint64_t end = off + sizeOf(t);
        size_t k = 0;
        for (size_t i = 0; i < pt.size(); i++)
          if (pt[i].first >= off && pt[i].first < end) r[i] = v[k++];
        setPieces(ins.getResult(), r);
      })
      .Case<LL::CallOp>([&](auto c) { call(c); })
      .Case<LL::CallIntrinsicOp>([&](auto c) { intrinsic(c); })
      .Case<LL::MemcpyOp, LL::MemmoveOp>([&](auto m) {
        Inst in{Op::MemCall};
        in.aux = 0;
        in.a = val(m.getDst());
        in.b = val(m.getSrc());
        in.c = val(m.getLen());
        emit(in);
      })
      .Case<LL::MemsetOp>([&](auto m) {
        Inst in{Op::MemCall};
        in.aux = 1;
        in.a = val(m.getDst());
        in.b = val(m.getVal());
        in.c = val(m.getLen());
        emit(in);
      })
      .Case<LL::AtomicRMWOp>([&](auto a) {
        Inst in{Op::AtomicRMW};
        in.aux = uint16_t(a.getBinOp());
        Type t = a.getVal().getType();
        in.w = uint8_t(intWidth(t));
        in.a = val(a.getPtr());
        in.b = val(a.getVal());
        Cls c;
        unsigned w;
        bool ptr;
        clsOf(t, c, w, ptr);
        if (c != Cls::Int) unsupported("atomicrmw on a float");
        set(a.getResult(), emit(in, c, w, ptr));
      })
      .Case<LL::AtomicCmpXchgOp>([&](auto x) {
        Inst in{Op::CmpXchg};
        Type t = x.getVal().getType();
        in.w = uint8_t(intWidth(t));
        in.a = val(x.getPtr());
        in.b = val(x.getCmp());
        in.c = val(x.getVal());
        VId ok = newVal(Cls::Int, 1, false);
        in.extra = ok;
        VId old = emit(in, Cls::Int, intWidth(t), isa<LL::LLVMPointerType>(t));
        f.vals[ok].def = f.vals[old].def;
        setPieces(x.getResult(), Pieces{old, ok});
      })
      .Case<LL::FenceOp>([&](auto) { emit(Inst{Op::Fence}); })
      .Case<LL::UnreachableOp>([&](auto) { emit(Inst{Op::Unreachable}); })
      .Case<LL::ReturnOp>([&](auto r) {
        RetRec rr;
        if (r.getNumOperands() == 1)
          for (VId p : any(r.getOperand(0))) {
            use(p);
            rr.pieces.push_back(p);
          }
        Inst in{Op::Ret};
        in.extra = uint32_t(f.rets.size());
        f.rets.push_back(std::move(rr));
        emit(in);
      })
      .Case<LL::BrOp>([&](auto b) {
        Inst in{Op::Br};
        edge(b.getDest(), b.getDestOperands(), in.extra);
        emit(in);
      })
      .Case<LL::CondBrOp>([&](auto c) {
        VId cond = val(c.getCondition());
        uint64_t k;
        if (isConst(cond, k)) {  // a branch decided at compile time
          Inst in{Op::Br};
          if (k & 1)
            edge(c.getTrueDest(), c.getTrueDestOperands(), in.extra);
          else
            edge(c.getFalseDest(), c.getFalseDestOperands(), in.extra);
          emit(in);
          return;
        }
        Inst in{Op::CondBr};
        in.a = cond;
        uint32_t te, fe;
        edge(c.getTrueDest(), c.getTrueDestOperands(), te);
        edge(c.getFalseDest(), c.getFalseDestOperands(), fe);
        in.extra = te;
        in.aux2 = fe;
        emit(in);
      })
      .Case<LL::SwitchOp>([&](auto s) {
        SwitchRec sr;
        unsigned w = intWidth(s.getValue().getType());
        if (auto cases = s.getCaseValues()) {
          unsigned i = 0;
          for (const APInt &v : cases->template getValues<APInt>()) {
            sr.values.push_back(v.getZExtValue() & mask(w));
            uint32_t e;
            edge(s.getCaseDestinations()[i], s.getCaseOperands(i), e);
            sr.edges.push_back(e);
            i++;
          }
        }
        uint32_t d;
        edge(s.getDefaultDestination(), s.getDefaultOperands(), d);
        sr.edges.push_back(d);
        Inst in{Op::Switch};
        in.w = uint8_t(w);
        in.a = val(s.getValue());
        in.extra = uint32_t(f.switches.size());
        f.switches.push_back(std::move(sr));
        emit(in);
      })
      .Default([&](Operation *x) { unsupported("op " + x->getName().getStringRef().str()); });
}

void Builder::gep(LL::GEPOp g) {
  VId acc = val(g.getBase());
  Type et = g.getElemType();
  bool first = true;
  int64_t constOff = 0;
  for (auto idx : g.getIndices()) {
    uint64_t scale;
    if (first) {
      scale = sizeOf(et);
    } else if (auto s = dyn_cast<LL::LLVMStructType>(et)) {
      auto ia = llvm::dyn_cast_if_present<IntegerAttr>(idx);
      if (!ia) unsupported("dynamic struct index");
      constOff += int64_t(fieldOffset(s, unsigned(ia.getInt())));
      et = s.getBody()[ia.getInt()];
      continue;
    } else if (auto at = dyn_cast<LL::LLVMArrayType>(et)) {
      scale = sizeOf(at.getElementType());
      et = at.getElementType();
    } else {
      unsupported("gep into a scalar");
    }
    first = false;
    if (auto ia = llvm::dyn_cast_if_present<IntegerAttr>(idx)) {
      constOff += ia.getInt() * int64_t(scale);
      continue;
    }
    Value iv = llvm::cast<Value>(idx);
    unsigned w = intWidth(iv.getType());
    VId i = sextTo(val(iv), w, 64);  // GEP indices are signed
    if (scale == 0) continue;
    VId off = llvm::isPowerOf2_64(scale) ? bin(Op::Shl, 64, i, konst(llvm::Log2_64(scale)))
                                          : bin(Op::Mul, 64, i, konst(scale));
    acc = bin(Op::Add, 64, acc, off, true);
  }
  if (constOff) {
    // base + k1 + k2: one Add of the sum.
    if (const Inst *d = defOf(acc); d && d->op == Op::Add) {
      uint64_t k;
      if (isConst(d->b, k)) {
        acc = bin(Op::Add, 64, d->a, konst(k + uint64_t(constOff)), true);
        set(g.getResult(), acc);
        return;
      }
    }
    acc = bin(Op::Add, 64, acc, konst(uint64_t(constOff)), true);
  }
  set(g.getResult(), acc);
}

void Builder::call(LL::CallOp c) {
  if (c.getVarCalleeType()) unsupported("variadic call");
  CallRec cr;
  cr.op = c;
  if (auto callee = c.getCallee()) {
    if (*callee == "marmot_gpu_for") {  // CPU path: the call returns false
      if (c.getNumResults()) set(c.getResult(), konst(0, intWidth(c.getResult().getType())));
      return;
    }
    cr.sym = f.sym(*callee);
    if (mg.returnsTwice.count(c.getCalleeAttr().getAttr())) f.hasSetjmp = true;
  } else {
    cr.indirect = true;
    cr.callee = val(c.getCalleeOperands().front());
    use(cr.callee);
  }
  for (Value a : c.getArgOperands()) {
    if (isWide(a.getType())) unsupported("i128 in a call");
    for (VId p : any(a)) {
      use(p);
      cr.args.push_back(p);
    }
  }
  Inst in{Op::Call};
  in.extra = uint32_t(f.calls.size());
  if (c.getNumResults() == 1) {
    Type rt = c.getResult().getType();
    if (isWide(rt)) unsupported("i128 in a call");
    bool mem = x64 ? returnsInMemoryX64(rt) : returnsInMemory(rt);
    if (mem) {
      f.objs.push_back({sizeOf(rt), std::max<uint64_t>(alignOf(rt), 8)});
      cr.sretObj = int(f.objs.size() - 1);
    } else {
      for (auto &[off, t] : piecesOf(rt)) {
        Cls k;
        unsigned w;
        bool ptr;
        clsOf(t, k, w, ptr);
        VId v = newVal(k, w, ptr);
        f.vals[v].def = uint32_t(f.insts.size());
        cr.results.push_back(v);
      }
    }
  }
  f.calls.push_back(std::move(cr));
  emit(in);
  CallRec &r = f.calls.back();
  Value res = c.getNumResults() ? c.getResult() : Value();
  if (!res) return;
  if (r.sretObj >= 0) {
    Inst fr{Op::Frame};
    fr.aux = uint16_t(r.sretObj);
    VId base = emit(fr, Cls::Int, 64, true);
    Pieces ps;
    for (auto &[off, t] : piecesOf(res.getType())) ps.push_back(load(base, int64_t(off), t, false));
    if (isAggregate(res.getType()))
      setPieces(res, ps);
    else
      set(res, ps[0]);
    return;
  }
  if (isAggregate(res.getType()))
    setPieces(res, Pieces(r.results.begin(), r.results.end()));
  else
    set(res, r.results[0]);
}

void Builder::intrinsic(LL::CallIntrinsicOp c) {
  StringRef name = c.getIntrin();
  auto args = c.getArgs();
  if (c.getNumResults() != 1) unsupported("intrinsic " + name.str());
  Value res = c.getResult(0);
  Type rt = res.getType();
  if (isWide(rt)) unsupported("intrinsic " + name.str());
  auto un = [&](Op k) { set(res, unary(k, val(args[0]), Cls::Int, intWidth(rt), intWidth(rt))); };
  if (name.starts_with("llvm.ctlz.")) {
    un(Op::Ctlz);
  } else if (name.starts_with("llvm.cttz.")) {
    un(Op::Cttz);
  } else if (name.starts_with("llvm.ctpop.")) {
    un(Op::Ctpop);
  } else if (name.starts_with("llvm.bswap.")) {
    un(Op::Bswap);
  } else if (name.starts_with("llvm.bitreverse.")) {
    un(Op::Bitrev);
  } else if (name.starts_with("llvm.fshl.") || name.starts_with("llvm.fshr.")) {
    unsigned n = intWidth(rt);
    if (n != 64 && n > 32) unsupported("intrinsic " + name.str());
    Inst in{name.starts_with("llvm.fshl.") ? Op::Fshl : Op::Fshr};
    in.w = uint8_t(n);
    in.a = val(args[0]);
    in.b = val(args[1]);
    in.c = val(args[2]);
    set(res, emit(in, Cls::Int, n));
  } else if (name == "llvm.fptosi.sat.i64.f64" || name == "llvm.fptoui.sat.i64.f64" ||
             name == "llvm.fptosi.sat.i64.f32" || name == "llvm.fptoui.sat.i64.f32") {
    Op k = name.starts_with("llvm.fptosi") ? Op::FPToSISat : Op::FPToUISat;
    set(res, unary(k, val(args[0]), Cls::Int, 64, args[0].getType().isF64() ? 64 : 32));
  } else if (name == "llvm.frameaddress.p0") {
    auto k = args[0].getDefiningOp<LL::ConstantOp>();
    auto ia = k ? dyn_cast<IntegerAttr>(k.getValue()) : IntegerAttr();
    if (!ia || ia.getInt() != 0) unsupported("frameaddress above level 0");
    Inst in{Op::FrameAddr};
    set(res, emit(in, Cls::Int, 64, true));
  } else {
    unsupported("intrinsic " + name.str());
  }
}

// i128 as (low, high) words: constants, extensions, truncation, add, sub,
// mul, bitwise operations and shifts by an amount below 128 (math/bits).
void Builder::wide(Operation &o) {
  auto w2 = [&](Value v) -> std::pair<VId, VId> {
    if (isWide(v.getType())) {
      const Pieces &p = pieces(v);
      return {p[0], p[1]};
    }
    return {val(v), konst(0)};
  };
  auto out = [&](VId lo, VId hi) { setPieces(o.getResult(0), Pieces{lo, hi}); };
  auto pairOp = [&](Op k) {
    auto [al, ah] = w2(o.getOperand(0));
    auto [bl, bh] = w2(o.getOperand(1));
    out(bin(k, 64, al, bl), bin(k, 64, ah, bh));
  };
  llvm::TypeSwitch<Operation *>(&o)
      .Case<LL::ConstantOp>([&](auto c) {
        auto ia = dyn_cast<IntegerAttr>(c.getValue());
        if (!ia) unsupported("i128 constant");
        APInt v = ia.getValue().zextOrTrunc(128);
        out(konst(v.extractBitsAsZExtValue(64, 0)), konst(v.extractBitsAsZExtValue(64, 64)));
      })
      .Case<LL::ZeroOp, LL::UndefOp, LL::PoisonOp>([&](auto) { out(konst(0), konst(0)); })
      .Case<LL::ZExtOp>([&](auto c) { out(val(c.getArg()), konst(0)); })
      .Case<LL::SExtOp>([&](auto c) {
        VId lo = sextTo(val(c.getArg()), intWidth(c.getArg().getType()), 64);
        out(lo, bin(Op::AShr, 64, lo, konst(63)));
      })
      .Case<LL::TruncOp>([&](auto c) {
        if (isWide(c.getResult().getType())) unsupported("i128 operation");
        set(c.getResult(), trunc(w2(c.getArg()).first, intWidth(c.getResult().getType())));
      })
      .Case<LL::AddOp>([&](auto) {
        auto [al, ah] = w2(o.getOperand(0));
        auto [bl, bh] = w2(o.getOperand(1));
        VId lo = bin(Op::Add, 64, al, bl);
        VId carry = icmp(LL::ICmpPredicate::ult, 64, lo, al);
        out(lo, bin(Op::Add, 64, bin(Op::Add, 64, ah, bh), carry));
      })
      .Case<LL::SubOp>([&](auto) {
        auto [al, ah] = w2(o.getOperand(0));
        auto [bl, bh] = w2(o.getOperand(1));
        VId borrow = icmp(LL::ICmpPredicate::ult, 64, al, bl);
        out(bin(Op::Sub, 64, al, bl), bin(Op::Sub, 64, bin(Op::Sub, 64, ah, bh), borrow));
      })
      .Case<LL::AndOp>([&](auto) { pairOp(Op::And); })
      .Case<LL::OrOp>([&](auto) { pairOp(Op::Or); })
      .Case<LL::XOrOp>([&](auto) { pairOp(Op::Xor); })
      .Case<LL::MulOp>([&](auto) {
        auto [al, ah] = w2(o.getOperand(0));
        auto [bl, bh] = w2(o.getOperand(1));
        Inst h{Op::UMulH};
        h.a = al;
        h.b = bl;
        VId hi = emit(h);
        hi = bin(Op::Add, 64, hi, bin(Op::Mul, 64, al, bh));
        hi = bin(Op::Add, 64, hi, bin(Op::Mul, 64, ah, bl));
        out(bin(Op::Mul, 64, al, bl), hi);
      })
      .Case<LL::ShlOp, LL::LShrOp, LL::AShrOp>([&](auto) {
        auto [l, h] = w2(o.getOperand(0));
        VId s = w2(o.getOperand(1)).first;  // below 128
        bool left = isa<LL::ShlOp>(o), arith = isa<LL::AShrOp>(o);
        VId inv = bin(Op::Xor, 64, s, konst(63));  // 63 - s for s < 64
        VId big = icmp(LL::ICmpPredicate::uge, 64, s, konst(64));
        if (left) {
          // small: hi = hi << s | (lo >> 1) >> (63 - s), lo = lo << s; big: hi = lo << (s - 64), lo = 0
          VId sh = bin(Op::Or, 64, bin(Op::Shl, 64, h, s), bin(Op::LShr, 64, bin(Op::LShr, 64, l, konst(1)), inv));
          VId sl = bin(Op::Shl, 64, l, s);  // also the big hi (amount mod 64)
          out(select(big, konst(0), sl), select(big, sl, sh));
        } else {
          Op k = arith ? Op::AShr : Op::LShr;
          VId sl = bin(Op::Or, 64, bin(Op::LShr, 64, l, s), bin(Op::Shl, 64, bin(Op::Shl, 64, h, konst(1)), inv));
          VId sh = bin(k, 64, h, s);  // also the big lo
          VId bh = arith ? bin(Op::AShr, 64, h, konst(63)) : konst(0);
          out(select(big, sh, sl), select(big, bh, sh));
        }
      })
      .Case<LL::LoadOp>([&](auto l) {
        if (l.getOrdering() != LL::AtomicOrdering::not_atomic) unsupported("atomic i128 load");
        VId p = val(l.getAddr());
        Type i64 = IntegerType::get(ctx, 64);
        out(load(p, 0, i64, false), load(p, 8, i64, false));
      })
      .Default([&](Operation *x) { unsupported("i128 " + x->getName().getStringRef().str()); });
}

// Removes pure instructions whose results nobody uses, last first, so a
// chain goes in one walk (operands are defined before their users).
void Builder::dce() {
  for (size_t i = f.insts.size(); i-- > 0;) {
    Inst &in = f.insts[i];
    if (in.dead || !isPure(in.op) || in.dst == None || f.vals[in.dst].uses) continue;
    in.dead = true;
    for (VId v : {in.a, in.b, in.c})
      if (v != None) f.vals[v].uses--;
  }
}

} // namespace

void build(ModuleGen &mg, LL::LLVMFuncOp fn, Func &f, bool x64) {
  f.fn = fn;
  Builder(mg, fn, f, x64).run();
}

static const char *opName(Op o) {
  static const char *names[] = {
      "const", "addr", "frame", "copy", "add", "sub", "mul", "udiv", "sdiv", "urem", "srem", "and", "or", "xor",
      "shl", "lshr", "ashr", "umulh", "icmp", "select", "sext", "trunc", "fadd", "fsub", "fmul", "fdiv", "fmin",
      "fmax", "fminnum", "fmaxnum", "fneg", "fma", "fmuladd", "fcmp", "sitofp", "uitofp", "fptosi", "fptoui",
      "fptosi.sat", "fptoui.sat", "fpext", "fptrunc", "bits", "ctlz", "cttz", "ctpop", "bswap", "bitrev", "fshl",
      "fshr", "frameaddr", "load", "store", "zero", "copy8", "memcall", "call", "atomicrmw", "cmpxchg", "fence",
      "br", "condbr", "switch", "ret", "unreachable"};
  static_assert(sizeof(names) / sizeof(names[0]) == size_t(Op::Unreachable) + 1, "op names");
  return names[size_t(o)];
}

std::string print(const Func &f) {
  std::string s;
  llvm::raw_string_ostream os(s);
  auto v = [&](VId x) -> std::string { return x == None ? "_" : "%" + std::to_string(x); };
  auto edge = [&](uint32_t e) {
    const Edge &ed = f.edges[e];
    os << "^" << ed.block << "(";
    for (uint32_t i = ed.argBegin; i < ed.argEnd; i++) os << (i > ed.argBegin ? ", " : "") << v(f.edgeArgs[i]);
    os << ")";
  };
  os << "func @" << LL::LLVMFuncOp(f.fn).getSymName() << "\n";
  for (size_t b = 0; b < f.blocks.size(); b++) {
    const BB &bl = f.blocks[b];
    os << "^" << b << "(";
    for (size_t i = 0; i < bl.params.size(); i++) os << (i ? ", " : "") << v(bl.params[i]);
    os << "):" << (bl.dead ? " dead" : "") << "\n";
    for (uint32_t i = bl.begin; i < bl.end; i++) {
      const Inst &in = f.insts[i];
      if (in.dead) continue;
      os << "  ";
      if (in.dst != None) os << v(in.dst) << " = ";
      os << opName(in.op) << "." << unsigned(in.w);
      if (in.aux) os << " aux=" << in.aux;
      for (VId x : {in.a, in.b, in.c})
        if (x != None) os << " " << v(x);
      if (in.imm) os << " imm=" << in.imm;
      if (in.op == Op::Addr) os << " @" << f.syms[in.sym];
      if (in.op == Op::Br) {
        os << " ";
        edge(in.extra);
      } else if (in.op == Op::CondBr) {
        os << " ";
        edge(in.extra);
        os << " ";
        edge(in.aux2);
      } else if (in.op == Op::Call) {
        const CallRec &c = f.calls[in.extra];
        os << (c.indirect ? " *" + v(c.callee) : " @" + f.syms[c.sym]) << "(";
        for (size_t k = 0; k < c.args.size(); k++) os << (k ? ", " : "") << v(c.args[k]);
        os << ") -> (";
        for (size_t k = 0; k < c.results.size(); k++) os << (k ? ", " : "") << v(c.results[k]);
        os << ")";
      } else if (in.op == Op::Ret) {
        for (VId x : f.rets[in.extra].pieces) os << " " << v(x);
      }
      os << "\n";
    }
  }
  return os.str();
}

} // namespace mir
} // namespace tier0
} // namespace marmot
