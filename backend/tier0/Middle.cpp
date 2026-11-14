// Middle.cpp: see Middle.h.
#include "Middle.h"

#include "mlir/Interfaces/SideEffectInterfaces.h"
#include "llvm/ADT/BitVector.h"
#include "llvm/ADT/StringMap.h"

#include <algorithm>
#include <climits>

namespace marmot {
namespace tier0 {

// Linear scan register allocation: scalar values (integers, pointers and
// floats as bits, not constants) get one callee saved register for their
// whole live interval, computed from block liveness over the emission order;
// the rest keep their frame slots. No splitting, no coalescing. Functions
// that call a returns_twice function (rt_setjmp for defer) keep every value
// in memory, as a longjmp restores the callee saved registers.
bool callsReturnsTwice(LL::LLVMFuncOp fn, const llvm::DenseSet<StringAttr> &twice) {
  bool found = false;
  fn.walk([&](LL::CallOp c) {
    if (auto callee = c.getCalleeAttr(); callee && twice.count(callee.getAttr())) found = true;
  });
  return found;
}

static bool raOff() {
  static const bool off = [] {
    const char *v = getenv("MARMOT_TIER0_NORA");
    return v && *v && *v != '0';
  }();
  return off;
}

// Values to registers (from regs, all below 32); values of the operations in
// skip are never materialized. A value used only in its own block gets the
// interval from its definition to its last use; only values used in another
// block go through the liveness dataflow.
llvm::DenseMap<Value, unsigned> linearScan(LL::LLVMFuncOp fn, llvm::ArrayRef<unsigned> regs,
                                           const llvm::DenseSet<Operation *> &skip,
                                           const llvm::DenseSet<StringAttr> &twice,
                                           llvm::ArrayRef<unsigned> low) {
  llvm::DenseMap<Value, unsigned> res;
  if (raOff() || callsReturnsTwice(fn, twice)) return res;
  llvm::DenseMap<Value, unsigned> idx;
  std::vector<Value> vals;
  std::vector<unsigned> defBlk;
  std::vector<std::pair<unsigned, unsigned>> iv;  // first, last position
  // Positions in emission order: a block's start, then each operation.
  std::vector<Block *> blocks;
  std::vector<std::pair<unsigned, unsigned>> span;
  llvm::DenseMap<Block *, unsigned> bi;
  auto cand = [&](Value v, unsigned blk, unsigned at) {
    Type t = v.getType();
    uint64_t bits;
    if (isa<LL::LLVMVoidType>(t) || isAggregate(t) || isWide(t) || sizeOf(t) > 8 || scalarConst(v, bits)) return;
    if (Operation *d = v.getDefiningOp(); d && skip.count(d)) return;
    idx[v] = unsigned(vals.size());
    vals.push_back(v);
    defBlk.push_back(blk);
    iv.push_back({at, at});
  };
  // Uses of values defined in another block: (value, block).
  std::vector<std::pair<unsigned, unsigned>> far;
  std::vector<unsigned> barrier;  // positions of operations that use the low registers
  unsigned p = 0;
  for (Block &b : fn.getBody()) {
    unsigned i = unsigned(blocks.size());
    bi[&b] = i;
    blocks.push_back(&b);
    unsigned start = p++;
    for (BlockArgument a : b.getArguments()) cand(a, i, start);
    for (Operation &op : b) {
      unsigned at = p++;
      if (isa<LL::CallOp, LL::MemcpyOp, LL::MemmoveOp, LL::MemsetOp, LL::ReturnOp>(op)) barrier.push_back(at);
      llvm::SmallVector<Value, 4> used(op.getOperands());
      // A skipped operation (a fused compare, a lazy load) is emitted at its
      // users: its operands live there.
      for (size_t q = 0; q < used.size(); q++)
        if (Operation *d = used[q].getDefiningOp(); d && skip.count(d))
          used.append(d->getOperands().begin(), d->getOperands().end());
      for (Value v : used)
        if (auto it = idx.find(v); it != idx.end()) {
          unsigned k = it->second;
          iv[k].first = std::min(iv[k].first, at);
          iv[k].second = std::max(iv[k].second, at);
          if (defBlk[k] != i) far.push_back({k, i});
        }
      for (Value r : op.getResults()) cand(r, i, at);
    }
    span.push_back({start, p - 1});
  }
  if (vals.empty()) return res;
  auto ext = [&](unsigned v, unsigned at) {
    iv[v].first = std::min(iv[v].first, at);
    iv[v].second = std::max(iv[v].second, at);
  };
  // Block arguments are written by the edge moves at each predecessor's branch.
  for (unsigned i = 0; i < blocks.size(); i++)
    for (BlockArgument a : blocks[i]->getArguments())
      if (auto it = idx.find(a); it != idx.end())
        for (Block *pr : blocks[i]->getPredecessors()) ext(it->second, span[bi[pr]].second);
  if (!far.empty()) {
    std::vector<unsigned> gid(vals.size(), ~0u), gval;
    for (auto [k, b] : far)
      if (gid[k] == ~0u) {
        gid[k] = unsigned(gval.size());
        gval.push_back(k);
      }
    unsigned n = unsigned(gval.size());
    std::vector<llvm::BitVector> gen(blocks.size(), llvm::BitVector(n)), in(gen), out(gen);
    std::vector<std::vector<unsigned>> kill(blocks.size());
    for (unsigned g = 0; g < n; g++) kill[defBlk[gval[g]]].push_back(g);
    for (auto [k, b] : far) gen[b].set(gid[k]);
    in = gen;  // out starts empty
    std::vector<llvm::SmallVector<unsigned, 2>> succ(blocks.size());
    for (unsigned i = 0; i < blocks.size(); i++)
      for (Block *s : blocks[i]->getSuccessors()) succ[i].push_back(bi[s]);
    llvm::BitVector o(n);
    for (bool changed = true; changed;) {
      changed = false;
      for (unsigned i = unsigned(blocks.size()); i-- > 0;) {
        o.reset();
        for (unsigned s : succ[i]) o |= in[s];
        if (o == out[i]) continue;
        out[i] = o;
        for (unsigned g : kill[i]) o.reset(g);
        o |= gen[i];
        in[i] = o;
        changed = true;
      }
    }
    for (unsigned i = 0; i < blocks.size(); i++) {
      for (int g = in[i].find_first(); g >= 0; g = in[i].find_next(g)) ext(gval[g], span[i].first);
      for (int g = out[i].find_first(); g >= 0; g = out[i].find_next(g)) ext(gval[g], span[i].second);
    }
  }
  unsigned n = unsigned(vals.size());
  std::vector<unsigned> order(n);
  for (unsigned i = 0; i < n; i++) order[i] = i;
  std::stable_sort(order.begin(), order.end(), [&](unsigned a, unsigned b) { return iv[a].first < iv[b].first; });
  std::vector<unsigned> reg(n, ~0u);
  // One linear scan of the values that want takes over regs.
  auto scan = [&](llvm::ArrayRef<unsigned> rs, auto want) {
    std::vector<unsigned> active;  // value indices holding a register
    uint32_t freeRegs = 0;
    for (unsigned r : rs) freeRegs |= 1u << r;
    for (unsigned v : order) {
      if (reg[v] != ~0u || !want(v)) continue;
      unsigned start = iv[v].first;
      for (size_t k = 0; k < active.size();) {
        unsigned a = active[k];
        if (iv[a].second < start) {
          freeRegs |= 1u << reg[a];
          active[k] = active.back();
          active.pop_back();
        } else {
          k++;
        }
      }
      if (freeRegs) {
        unsigned r = unsigned(llvm::countr_zero(freeRegs));
        freeRegs &= ~(1u << r);
        reg[v] = r;
        active.push_back(v);
        continue;
      }
      // Full: the interval that ends last stays in memory.
      size_t fk = 0;
      for (size_t k = 1; k < active.size(); k++)
        if (iv[active[k]].second > iv[active[fk]].second) fk = k;
      unsigned a = active[fk];
      if (iv[a].second > iv[v].second) {
        reg[v] = reg[a];
        reg[a] = ~0u;
        active[fk] = v;
      }
    }
  };
  // The low registers first, for values that no call, memory intrinsic or
  // return meets after their definition (a call's result may start there)
  // and that the prologue does not write (entry block arguments).
  if (!low.empty()) {
    Block *entry = &fn.getBody().front();
    scan(low, [&](unsigned v) {
      if (auto ba = dyn_cast<BlockArgument>(vals[v]); ba && ba.getOwner() == entry) return false;
      auto it = std::upper_bound(barrier.begin(), barrier.end(), iv[v].first);
      return it == barrier.end() || *it > iv[v].second;
    });
  }
  scan(regs, [](unsigned) { return true; });
  for (unsigned v = 0; v < n; v++)
    if (reg[v] != ~0u) res[vals[v]] = reg[v];
  return res;
}

void FuncPlan::analyze() {
  // An icmp right before the cond_br that is its only use sets the flags.
  fn.walk([&](LL::CondBrOp c) {
    auto ic = c.getCondition().getDefiningOp<LL::ICmpOp>();
    if (ic && ic->getBlock() == c->getBlock() && ic->hasOneUse() && !isWide(ic.getLhs().getType()))
      fusedCmp.insert(ic.getOperation());
    // A bool byte loaded from memory and truncated to i1 for the branch:
    // cbz/cbnz on the byte (a Go bool in memory is 0 or 1; gc tests the
    // byte too).
    auto tr = c.getCondition().getDefiningOp<LL::TruncOp>();
    if (tr && tr->getBlock() == c->getBlock() && tr->hasOneUse() && tr.getType().isInteger(1))
      if (auto ld = tr.getArg().getDefiningOp<LL::LoadOp>(); ld && ld.getType().isInteger(8))
        fusedCmp.insert(tr.getOperation());
  });
  findDecidedNilChecks();
  findLazyAggArgs();
  // A GEP with constant indices whose every use is the address of a plain
  // (not atomic) load or store folds into them as an offset.
  fn.walk([&](LL::GEPOp g) {
    int64_t off = 0;
    Type et = g.getElemType();
    bool first = true;
    for (auto idx : g.getIndices()) {
      auto ia = llvm::dyn_cast_if_present<IntegerAttr>(idx);
      if (!ia) return;
      if (first) {
        off += ia.getInt() * int64_t(sizeOf(et));
      } else if (auto st = dyn_cast<LL::LLVMStructType>(et)) {
        off += int64_t(fieldOffset(st, unsigned(ia.getInt())));
        et = st.getBody()[ia.getInt()];
      } else if (auto at = dyn_cast<LL::LLVMArrayType>(et)) {
        off += ia.getInt() * int64_t(sizeOf(at.getElementType()));
        et = at.getElementType();
      } else {
        return;
      }
      first = false;
    }
    if (off < 0 || off >= 4096 || g.getResult().use_empty()) return;
    for (OpOperand &u : g.getResult().getUses()) {
      Operation *o = u.getOwner();
      if (auto l = dyn_cast<LL::LoadOp>(o)) {
        if (l.getOrdering() != LL::AtomicOrdering::not_atomic) return;
      } else if (auto st = dyn_cast<LL::StoreOp>(o)) {
        if (u.getOperandNumber() != 1 || st.getOrdering() != LL::AtomicOrdering::not_atomic) return;
      } else {
        return;
      }
    }
    foldGep[g] = uint64_t(off);
    fusedCmp.insert(g);
  });
  // An aggregate load whose uses are all in its block, read its fields
  // (extractvalue), copy it (store), pass it (call) or return it, with
  // nothing that may write memory in between, is not copied to a slot:
  // each use reads the memory.
  fn.walk([&](LL::LoadOp l) {
    if (!isAggregate(l.getType()) || l.getVolatile_() || l.getOrdering() != LL::AtomicOrdering::not_atomic)
      return;
    Operation *last = nullptr;
    for (OpOperand &u : l.getResult().getUses()) {
      Operation *o = u.getOwner();
      if (o->getBlock() != l->getBlock()) return;
      bool ok = isa<LL::ExtractValueOp, LL::ReturnOp>(o) || (isa<LL::StoreOp>(o) && u.getOperandNumber() == 0) ||
                (isa<LL::CallOp>(o) && !cast<LL::CallOp>(o).getVarCalleeType());
      if (auto c = dyn_cast<LL::CallOp>(o); c && !c.getCallee() && u.getOperandNumber() == 0) ok = false;
      if (!ok) return;
      if (!last || last->isBeforeInBlock(o)) last = o;
    }
    if (!last) return;
    for (Operation *o = l->getNextNode(); o != last; o = o->getNextNode()) {
      auto mi = dyn_cast<MemoryEffectOpInterface>(o);
      if (!mi || mi.hasEffect<MemoryEffects::Write>()) return;
    }
    lazyLoad.insert(l);
    fusedCmp.insert(l);
  });
}

void FuncPlan::layout() {
  frameSize = 0;
  // Outgoing stack arguments at the bottom of the frame.
  uint64_t out = 0;
  fn.walk([&](LL::CallOp c) { out = std::max(out, outgoingArgBytes(c)); });
  alloc(out, 16);
  // The callee saved registers next, near sp, so ldp/stp reach them.
  saveArea = alloc(8 * uint64_t(llvm::popcount(calleeUsed)), 8);
  auto rt = fn.getFunctionType().getReturnType();
  if (!isa<LL::LLVMVoidType>(rt) && resultInMemory(rt)) {
    sret = true;
    sretSlot = alloc(8, 8);
  }
  // Values of at most 32 bytes first, near sp (ldp/stp reach 504 bytes);
  // larger values and alloca storage after them.
  std::vector<std::pair<Value, Operation *>> large;
  // Values that need a slot, in definition order; an insertvalue whose
  // aggregate has no other use updates it in place (one slot for the
  // chain: building a struct field by field copies nothing).
  std::vector<Value> vals;
  llvm::DenseMap<Value, unsigned> leader;  // value -> index of its slot's first value
  for (Block &b : fn.getBody()) {
    for (BlockArgument a : b.getArguments())
      if (!home.count(a)) {
        leader[a] = unsigned(vals.size());
        vals.push_back(a);
      }
    for (Operation &op : b) {
      for (Value r : op.getResults()) {
        uint64_t bits;
        if (home.count(r) || constBits(r, bits) || fusedCmp.count(&op)) continue;
        unsigned me = unsigned(vals.size());
        vals.push_back(r);
        leader[r] = me;
        if (auto ins = dyn_cast<LL::InsertValueOp>(op); ins && ins.getContainer().hasOneUse())
          if (auto it = leader.find(ins.getContainer()); it != leader.end()) leader[r] = it->second;
      }
      if (auto a = dyn_cast<LL::AllocaOp>(op)) {
        if (&b != &fn.getBody().front()) unsupported("alloca outside the entry block");
        auto c = a.getArraySize().getDefiningOp<LL::ConstantOp>();
        if (!c) unsupported("dynamic alloca");
        large.push_back({Value(), &op});
      }
    }
  }
  // Slots are shared by values whose lifetimes do not overlap (each value's
  // hull, from its first to its last live position, as one interval): the
  // frame shrinks, more slots are within ldp/stp reach, and a dead
  // pointer's slot is soon written by another value. Values of at most 32
  // bytes first, near sp; larger ones after.
  std::vector<std::pair<unsigned, unsigned>> hull = slotHulls(vals);
  struct Group {
    unsigned first, last;
    uint64_t size, align;
    std::vector<unsigned> members;
  };
  std::vector<Group> groups;
  llvm::DenseMap<unsigned, unsigned> groupOf;  // leader -> group
  for (unsigned v = 0; v < vals.size(); v++) {
    unsigned l = leader[vals[v]];
    auto [it, fresh] = groupOf.try_emplace(l, unsigned(groups.size()));
    if (fresh) {
      uint64_t sz = sizeOf(vals[v].getType());
      groups.push_back({hull[v].first, hull[v].second, sz < 8 ? 8 : (sz + 7) & ~uint64_t(7),
                        std::max<uint64_t>(8, alignOf(vals[v].getType())), {}});
    }
    Group &g = groups[it->second];
    g.first = std::min(g.first, hull[v].first);
    g.last = std::max(g.last, hull[v].second);
    g.members.push_back(v);
  }
  auto place = [&](bool small) {
    std::vector<unsigned> order;
    for (unsigned g = 0; g < groups.size(); g++)
      if ((groups[g].size <= 32) == small) order.push_back(g);
    llvm::stable_sort(order, [&](unsigned a, unsigned b) { return groups[a].first < groups[b].first; });
    // Slots by (size, align): offset and the last position of their value.
    std::map<std::pair<uint64_t, uint64_t>, std::vector<std::pair<uint64_t, unsigned>>> slots;
    for (unsigned g : order) {
      Group &gr = groups[g];
      auto &list = slots[{gr.size, gr.align}];
      uint64_t off = UINT64_MAX;
      for (auto &[o, last] : list)
        if (last < gr.first) {
          off = o;
          last = gr.last;
          break;
        }
      if (off == UINT64_MAX) {
        off = alloc(gr.size, gr.align);
        list.push_back({off, gr.last});
      }
      for (unsigned v : gr.members) slot[vals[v]] = off;
    }
  };
  place(true);
  place(false);
  for (auto [r, op] : large) {
    if (op) {
      auto a = cast<LL::AllocaOp>(op);
      uint64_t n = cast<IntegerAttr>(a.getArraySize().getDefiningOp<LL::ConstantOp>().getValue()).getValue().getZExtValue();
      uint64_t al = std::max<uint64_t>(a.getAlignment().value_or(0), alignOf(a.getElemType()));
      allocaStorage[op] = alloc(std::max<uint64_t>(n * sizeOf(a.getElemType()), 1), std::max<uint64_t>(al, 8));
    } else {
      slot[r] = alloc(sizeOf(r.getType()), std::max<uint64_t>(8, alignOf(r.getType())));
    }
  }
  // Temporary area for parallel block argument moves.
  for (Block &b : fn.getBody()) {
    uint64_t n = 0;
    for (BlockArgument a : b.getArguments()) n += (sizeOf(a.getType()) + 7) & ~uint64_t(7);
    tempSize = std::max(tempSize, n);
  }
  tempArea = alloc(tempSize, 8);
  frameSize = (frameSize + 15) & ~uint64_t(15);
}

std::vector<std::pair<unsigned, unsigned>> FuncPlan::slotHulls(const std::vector<Value> &vals) {
  const unsigned n = unsigned(vals.size());
  std::vector<std::pair<unsigned, unsigned>> hull(n, {UINT_MAX, 0});
  if (!n) return hull;
  llvm::DenseMap<Value, unsigned> idx;
  for (unsigned v = 0; v < n; v++) idx[vals[v]] = v;
  auto skipped = [&](Operation *d) { return d && (fusedCmp.count(d) || lazyChainOps.count(d)); };
  auto usesOf = [&](Operation &op, llvm::SmallVectorImpl<unsigned> &out) {
    llvm::SmallVector<Value, 8> work(op.getOperands());
    llvm::DenseSet<Operation *> seen;
    while (!work.empty()) {
      Value v = work.pop_back_val();
      if (auto it = idx.find(v); it != idx.end()) out.push_back(it->second);
      if (Operation *d = v.getDefiningOp(); skipped(d) && seen.insert(d).second)
        work.append(d->getOperands().begin(), d->getOperands().end());
    }
  };
  auto touch = [&](unsigned v, unsigned at) {
    hull[v].first = std::min(hull[v].first, at);
    hull[v].second = std::max(hull[v].second, at);
  };
  std::vector<Block *> blocks;
  llvm::DenseMap<Block *, unsigned> bi;
  std::vector<std::pair<unsigned, unsigned>> span;  // block start, terminator position
  unsigned p = 0;
  for (Block &b : fn.getBody()) {
    bi[&b] = unsigned(blocks.size());
    blocks.push_back(&b);
    unsigned start = p++;
    for (Operation &op : b) {
      (void)op;
      p++;
    }
    span.push_back({start, p - 1});
  }
  std::vector<llvm::BitVector> gen(blocks.size(), llvm::BitVector(n)), kill(blocks.size(), llvm::BitVector(n)),
      in(blocks.size(), llvm::BitVector(n)), out(blocks.size(), llvm::BitVector(n));
  for (unsigned k = 0; k < blocks.size(); k++) {
    Block *b = blocks[k];
    unsigned at = span[k].first;
    for (BlockArgument a : b->getArguments())
      if (auto it = idx.find(a); it != idx.end()) {
        touch(it->second, at);
        kill[k].set(it->second);
      }
    for (Operation &op : *b) {
      ++at;
      if (skipped(&op)) continue;
      llvm::SmallVector<unsigned, 8> u;
      usesOf(op, u);
      for (unsigned v : u) {
        touch(v, at);
        if (!kill[k].test(v)) gen[k].set(v);
      }
      for (Value r : op.getResults())
        if (auto it = idx.find(r); it != idx.end()) {
          touch(it->second, at);
          kill[k].set(it->second);
        }
      // A branch writes its successors' argument slots here.
      for (Block *s2 : op.getSuccessors())
        for (BlockArgument a : s2->getArguments())
          if (auto it = idx.find(a); it != idx.end()) touch(it->second, at);
    }
  }
  for (bool changed = true; changed;) {
    changed = false;
    for (unsigned k = unsigned(blocks.size()); k-- > 0;) {
      llvm::BitVector o(n);
      for (Block *s2 : blocks[k]->getSuccessors()) o |= in[bi[s2]];
      llvm::BitVector i2 = o;
      i2.reset(kill[k]);
      i2 |= gen[k];
      if (o != out[k] || i2 != in[k]) {
        out[k] = std::move(o);
        in[k] = std::move(i2);
        changed = true;
      }
    }
  }
  for (unsigned k = 0; k < blocks.size(); k++) {
    for (unsigned v : in[k].set_bits()) touch(v, span[k].first);
    for (unsigned v : out[k].set_bits()) touch(v, span[k].second);
  }
  for (auto &h : hull)
    if (h.first == UINT_MAX) h = {0, 0};
  return hull;
}

static bool holdsPointers(Type t) {
  if (isa<LLVM::LLVMPointerType>(t)) return true;
  if (auto st = dyn_cast<LLVM::LLVMStructType>(t)) return llvm::any_of(st.getBody(), holdsPointers);
  if (auto at = dyn_cast<LLVM::LLVMArrayType>(t)) return holdsPointers(at.getElementType());
  return false;
}

// Per call, the frame words the collector scans: every word except those
// that only hold values (their slots) that are dead after the call or
// hold no pointers. Words outside value slots (allocas, the callee saved
// registers, outgoing arguments, temporaries) are always scanned. Off
// unless MARMOT_STACKMAPS=1 (the runtime reads them from pc table v4).
void FuncPlan::computeStackMaps() {
  static const bool on = [] {
    const char *e = getenv("MARMOT_STACKMAPS");
    return e && *e == '1';
  }();
  if (!on || frameSize == 0 || frameSize % 8 || callsReturnsTwice(fn, mg.returnsTwice)) return;
  // Only frames that can stay on the stack across collections while their
  // dead slots go stale: functions with a loop (a branch back to an earlier
  // block). Other frames return soon; they stay conservative, and their
  // maps would cost more table than they save.
  {
    llvm::DenseMap<Block *, unsigned> order;
    for (Block &b : fn.getBody()) order[&b] = unsigned(order.size());
    bool loop = false;
    for (Block &b : fn.getBody())
      for (Block *s2 : b.getSuccessors())
        if (order[s2] <= order[&b]) loop = true;
    if (!loop) return;
  }
  // Slot values, by index.
  llvm::DenseMap<Value, unsigned> idx;
  std::vector<Value> vals;
  for (auto &[v, off] : slot) {
    idx[v] = unsigned(vals.size());
    vals.push_back(v);
  }
  if (vals.empty()) return;
  const unsigned n = unsigned(vals.size()), nwords = unsigned(frameSize / 8);
  auto skipped = [&](Operation *d) { return d && (fusedCmp.count(d) || lazyChainOps.count(d)); };
  // The values op uses where it is emitted (operands of the skipped
  // operations it makes at its place included).
  auto usesOf = [&](Operation &op, llvm::BitVector &out) {
    llvm::SmallVector<Value, 8> work(op.getOperands());
    llvm::DenseSet<Operation *> seen;
    while (!work.empty()) {
      Value v = work.pop_back_val();
      if (auto it = idx.find(v); it != idx.end()) out.set(it->second);
      if (Operation *d = v.getDefiningOp(); skipped(d) && seen.insert(d).second)
        work.append(d->getOperands().begin(), d->getOperands().end());
    }
  };
  // Liveness over blocks.
  std::vector<Block *> blocks;
  llvm::DenseMap<Block *, unsigned> bi;
  for (Block &b : fn.getBody()) {
    bi[&b] = unsigned(blocks.size());
    blocks.push_back(&b);
  }
  std::vector<llvm::BitVector> gen(blocks.size(), llvm::BitVector(n)), kill(blocks.size(), llvm::BitVector(n)),
      in(blocks.size(), llvm::BitVector(n)), out(blocks.size(), llvm::BitVector(n));
  for (unsigned k = 0; k < blocks.size(); k++) {
    Block *b = blocks[k];
    // Backward: gen = uses not preceded by a def in the block.
    for (Operation &op : llvm::reverse(*b)) {
      if (skipped(&op)) continue;
      for (Value r : op.getResults())
        if (auto it = idx.find(r); it != idx.end()) {
          kill[k].set(it->second);
          gen[k].reset(it->second);
        }
      llvm::BitVector u(n);
      usesOf(op, u);
      gen[k] |= u;
    }
    for (BlockArgument a : b->getArguments())
      if (auto it = idx.find(a); it != idx.end()) {
        kill[k].set(it->second);
        gen[k].reset(it->second);
      }
  }
  for (bool changed = true; changed;) {
    changed = false;
    for (unsigned k = unsigned(blocks.size()); k-- > 0;) {
      llvm::BitVector o(n);
      for (Block *s2 : blocks[k]->getSuccessors()) o |= in[bi[s2]];
      llvm::BitVector i2 = o;
      i2.reset(kill[k]);
      i2 |= gen[k];
      if (o != out[k] || i2 != in[k]) {
        out[k] = std::move(o);
        in[k] = std::move(i2);
        changed = true;
      }
    }
  }
  // Words: covered by some value slot; the pointer values covering each.
  std::vector<char> covered(nwords, 0);
  std::vector<std::vector<unsigned>> ptrVals(nwords);
  auto wordOf = [&](uint64_t off) { return int64_t(frameSize - off) / 8 - 1; };  // sp + off = fp - 8*(i+1)
  for (unsigned v = 0; v < n; v++) {
    uint64_t off = slot[vals[v]], sz = std::max<uint64_t>((sizeOf(vals[v].getType()) + 7) & ~uint64_t(7), 8);
    bool ptr = holdsPointers(vals[v].getType());
    for (uint64_t o = off; o < off + sz; o += 8) {
      int64_t i = wordOf(o);
      if (i < 0 || i >= int64_t(nwords)) continue;
      covered[i] = 1;
      if (ptr) ptrVals[i].push_back(v);
    }
  }
  llvm::StringMap<uint32_t> mapId;
  for (unsigned k = 0; k < blocks.size(); k++) {
    llvm::BitVector live = out[k];
    for (Operation &op : llvm::reverse(*blocks[k])) {
      if (skipped(&op)) continue;
      if (auto call = dyn_cast<LL::CallOp>(op)) {
        // Live across the call: live after it; a result the callee writes
        // into the frame (returned in memory) too.
        llvm::BitVector at = live;
        for (Value r : op.getResults())
          if (auto it = idx.find(r); it != idx.end()) {
            if (resultInMemory(r.getType())) at.set(it->second);
            else at.reset(it->second);
          }
        std::string bits((nwords + 7) / 8, '\0');
        for (unsigned i = 0; i < nwords; i++) {
          bool scan = !covered[i] || llvm::any_of(ptrVals[i], [&](unsigned v) { return at.test(v); });
          if (scan) bits[i / 8] = char(bits[i / 8] | (1 << (i % 8)));
        }
        auto [it, fresh] = mapId.try_emplace(bits, uint32_t(smaps.size()));
        if (fresh) smaps.push_back({nwords, bits});
        callMap[call.getOperation()] = it->second;
      }
      for (Value r : op.getResults())
        if (auto it = idx.find(r); it != idx.end()) live.reset(it->second);
      usesOf(op, live);
    }
  }
}

void FuncPlan::findLazyAggArgs() {
  fn.walk([&](LL::CallOp call) {
    if (call.getVarCalleeType()) return;
    for (Value v : call.getArgOperands()) {
      if (!isAggregate(v.getType()) || !v.hasOneUse() || !v.getDefiningOp<LL::InsertValueOp>()) continue;
      std::vector<std::pair<uint64_t, Type>> parts;
      flatten(v.getType(), 0, parts);
      if (!llvm::all_of(parts, [&](auto &pt) { return !isFloat(pt.second) && sizeOf(pt.second) == 8 && pt.first % 8 == 0; }))
        continue;
      std::map<uint64_t, Value> fields;
      std::vector<Operation *> ops;
      Value cur = v;
      bool ok = true;
      while (auto ins = cur.getDefiningOp<LL::InsertValueOp>()) {
        if (!ins->hasOneUse() || ins.getPosition().size() != 1) { ok = false; break; }
        auto [off, t] = elementAt(ins.getType(), ins.getPosition());
        if (isAggregate(t) || isFloat(t) || sizeOf(t) != 8) { ok = false; break; }
        // Only values the call can make again: constants, and addresses of
        // globals used only here (another value's register or slot may be
        // reused between the insertvalue, its last use, and the call).
        Value fv = ins.getValue();
        uint64_t bits;
        if (auto ao = fv.getDefiningOp<LL::AddressOfOp>(); ao && fv.hasOneUse()) {
          ops.push_back(ao);
        } else if (!constBits(fv, bits)) {
          ok = false;
          break;
        }
        fields.try_emplace(off, fv);  // the last insert of a field wins
        ops.push_back(ins);
        cur = ins.getContainer();
      }
      Operation *base = cur.getDefiningOp();
      if (!ok || !base || !isa<LL::UndefOp, LL::ZeroOp, LL::PoisonOp>(base) || !base->hasOneUse()) continue;
      ops.push_back(base);
      lazyAgg[v] = std::move(fields);
      lazyChainOps.insert(ops.begin(), ops.end());
    }
  });
}

void FuncPlan::findDecidedNilChecks() {
  // The pointers known non-nil on entry to a block, through chains of
  // single-predecessor blocks from the non-nil side of a nil check.
  llvm::DenseMap<Block *, llvm::SmallVector<Value, 4>> nonNull;
  auto nilCheck = [&](Operation *t, Value &p, bool &eq) {
    auto cb = dyn_cast_or_null<LL::CondBrOp>(t);
    if (!cb || !cb.getTrueDestOperands().empty() || !cb.getFalseDestOperands().empty()) return false;
    auto ic = cb.getCondition().getDefiningOp<LL::ICmpOp>();
    uint64_t bits;
    if (!ic || !fusedCmp.count(ic) || !isa<LLVM::LLVMPointerType>(ic.getLhs().getType()) ||
        !constBits(ic.getRhs(), bits) || bits != 0)
      return false;
    auto pr = ic.getPredicate();
    if (pr != LL::ICmpPredicate::eq && pr != LL::ICmpPredicate::ne) return false;
    p = ic.getLhs();
    eq = pr == LL::ICmpPredicate::eq;
    return true;
  };
  for (Block &b : fn.getBody()) {
    Block *pb = b.getSinglePredecessor();
    if (!pb) continue;
    llvm::SmallVector<Value, 4> facts;
    if (auto it = nonNull.find(pb); it != nonNull.end()) facts = it->second;
    Value p;
    bool eq;
    if (nilCheck(pb->getTerminator(), p, eq)) {
      auto cb = cast<LL::CondBrOp>(pb->getTerminator());
      Block *nonNil = eq ? cb.getFalseDest() : cb.getTrueDest();
      if (nonNil == &b && cb.getTrueDest() != cb.getFalseDest() && !llvm::is_contained(facts, p)) facts.push_back(p);
    }
    if (!facts.empty()) nonNull[&b] = std::move(facts);
  }
  for (Block &b : fn.getBody()) {
    Value p;
    bool eq;
    if (!nilCheck(b.getTerminator(), p, eq)) continue;
    auto it = nonNull.find(&b);
    if (it == nonNull.end() || !llvm::is_contained(it->second, p)) continue;
    auto cb = cast<LL::CondBrOp>(b.getTerminator());
    decidedBr[cb.getOperation()] = eq ? cb.getFalseDest() : cb.getTrueDest();
  }
  if (decidedBr.empty()) return;
  // Blocks that only decided branches (or dead blocks) reach.
  for (bool changed = true; changed;) {
    changed = false;
    for (Block &b : fn.getBody()) {
      if (b.isEntryBlock() || deadBlock.count(&b) || b.hasNoPredecessors()) continue;
      bool live = false;
      for (Block *pb : b.getPredecessors()) {
        if (deadBlock.count(pb)) continue;
        auto d = decidedBr.find(pb->getTerminator());
        if (d == decidedBr.end() || d->second == &b) live = true;
      }
      if (!live) {
        deadBlock.insert(&b);
        changed = true;
      }
    }
  }
}

} // namespace tier0
} // namespace marmot
