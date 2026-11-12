// Codegen.cpp: tier0 code generation for arm64 Mach-O (see DESIGN.md).
//
// First version: every SSA value has a stack slot (aggregates a slot of their
// size); each operation loads its operands into scratch registers, computes,
// and stores the result. Integers narrower than 64 bits are kept zero
// extended in their slots, so width sensitive operations only need to sign
// extend. Functions using anything not handled here are reported for LLVM.

#include "Tier0.h"

#include "Arm64.h"
#include "MachO.h"

#include "mlir/Dialect/LLVMIR/LLVMDialect.h"
#include "mlir/Dialect/LLVMIR/LLVMTypes.h"
#include "mlir/IR/BuiltinOps.h"
#include "mlir/IR/BuiltinTypes.h"
#include "llvm/ADT/DenseMap.h"
#include "llvm/ADT/TypeSwitch.h"

#include <cstring>
#include <optional>

using namespace mlir;
namespace LL = mlir::LLVM;

namespace marmot {
namespace tier0 {
namespace {

using namespace a64;

// Scratch registers. x0-x7 and d0-d7 carry arguments; x9-x15 are free
// temporaries; x16/x17 are used for addresses of large offsets.
constexpr unsigned R1 = 9, R2 = 10, R3 = 11, R4 = 12, RA = 13, RT = 16;
constexpr unsigned F1 = 16, F2 = 17, F3 = 18;  // v16-v18: caller saved, not arguments

struct Unsupported {
  std::string why;
};

[[noreturn]] void unsupported(const std::string &why) { throw Unsupported{why}; }

// ---- types and layout (the default data layout of arm64 and x86-64) ----

uint64_t alignOf(Type t);

uint64_t sizeOf(Type t) {
  if (auto i = dyn_cast<IntegerType>(t)) {
    unsigned w = i.getWidth();
    if (w > 64) unsupported("integer wider than 64 bits");
    return w <= 8 ? 1 : w <= 16 ? 2 : w <= 32 ? 4 : 8;
  }
  if (isa<LL::LLVMPointerType>(t)) return 8;
  if (t.isF32()) return 4;
  if (t.isF64()) return 8;
  if (auto s = dyn_cast<LL::LLVMStructType>(t)) {
    uint64_t off = 0, maxAlign = 1;
    for (Type f : s.getBody()) {
      uint64_t a = s.isPacked() ? 1 : alignOf(f);
      off = (off + a - 1) & ~(a - 1);
      off += sizeOf(f);
      maxAlign = std::max(maxAlign, a);
    }
    return (off + maxAlign - 1) & ~(maxAlign - 1);
  }
  if (auto a = dyn_cast<LL::LLVMArrayType>(t)) return sizeOf(a.getElementType()) * a.getNumElements();
  unsupported("type");
}

uint64_t alignOf(Type t) {
  if (auto s = dyn_cast<LL::LLVMStructType>(t)) {
    if (s.isPacked()) return 1;
    uint64_t a = 1;
    for (Type f : s.getBody()) a = std::max(a, alignOf(f));
    return a;
  }
  if (auto a = dyn_cast<LL::LLVMArrayType>(t)) return alignOf(a.getElementType());
  return sizeOf(t);
}

uint64_t fieldOffset(LL::LLVMStructType s, unsigned idx) {
  uint64_t off = 0;
  auto body = s.getBody();
  for (unsigned i = 0; i < body.size(); i++) {
    uint64_t a = s.isPacked() ? 1 : alignOf(body[i]);
    off = (off + a - 1) & ~(a - 1);
    if (i == idx) return off;
    off += sizeOf(body[i]);
  }
  return off;
}

bool isAggregate(Type t) { return isa<LL::LLVMStructType, LL::LLVMArrayType>(t); }
bool isFloat(Type t) { return t.isF32() || t.isF64(); }
unsigned intWidth(Type t) {
  if (auto i = dyn_cast<IntegerType>(t)) return i.getWidth();
  return 64;  // pointers
}

// Offset and type of the element at a position path inside an aggregate.
std::pair<uint64_t, Type> elementAt(Type t, ArrayRef<int64_t> pos) {
  uint64_t off = 0;
  for (int64_t p : pos) {
    if (auto s = dyn_cast<LL::LLVMStructType>(t)) {
      off += fieldOffset(s, unsigned(p));
      t = s.getBody()[p];
    } else if (auto a = dyn_cast<LL::LLVMArrayType>(t)) {
      off += uint64_t(p) * sizeOf(a.getElementType());
      t = a.getElementType();
    } else {
      unsupported("aggregate position");
    }
  }
  return {off, t};
}

// Scalar pieces of a value in memory order, as LLVM splits aggregates for
// calls and returns: offset and type of each.
void flatten(Type t, uint64_t base, std::vector<std::pair<uint64_t, Type>> &out) {
  if (auto s = dyn_cast<LL::LLVMStructType>(t)) {
    auto body = s.getBody();
    for (unsigned i = 0; i < body.size(); i++) flatten(body[i], base + fieldOffset(s, i), out);
    return;
  }
  if (auto a = dyn_cast<LL::LLVMArrayType>(t)) {
    uint64_t es = sizeOf(a.getElementType());
    for (unsigned i = 0; i < a.getNumElements(); i++) flatten(a.getElementType(), base + i * es, out);
    return;
  }
  sizeOf(t);  // rejects unsupported scalars
  out.push_back({base, t});
}

unsigned log2Size(uint64_t n) { return n == 1 ? 0 : n == 2 ? 1 : n == 4 ? 2 : 3; }

std::string machoName(StringRef sym) { return "_" + sym.str(); }

// ---- module level ----

class ModuleGen {
public:
  explicit ModuleGen(ModuleOp m) : module(m), ctx(m.getContext()) {}
  bool run(std::string &fragment, std::vector<std::string> &fallback, std::vector<std::string> &reasons,
           Stats &stats, std::string &err);
  MachOWriter w;

  // Symbols defined in this module (functions with bodies and globals with
  // initializers): reached directly; everything else through the GOT.
  llvm::DenseMap<StringAttr, bool> definedData;
  bool isDefinedData(StringRef name) const;

private:
  ModuleOp module;
  MLIRContext *ctx;
  void emitGlobal(LL::GlobalOp g);
};

// ---- constant evaluation for global initializers ----

struct ConstVal {
  std::vector<uint8_t> bytes;
  struct Rel {
    uint64_t off;
    std::string sym;
    int64_t addend;
  };
  std::vector<Rel> rels;
};

void putInt(std::vector<uint8_t> &b, uint64_t off, uint64_t v, uint64_t n) {
  for (uint64_t i = 0; i < n; i++) b[off + i] = uint8_t(v >> (8 * i));
}

void attrBytes(Attribute a, Type t, std::vector<uint8_t> &b, uint64_t off) {
  if (auto i = dyn_cast<IntegerAttr>(a)) {
    putInt(b, off, i.getValue().getZExtValue(), sizeOf(t));
    return;
  }
  if (auto f = dyn_cast<FloatAttr>(a)) {
    uint64_t bits = f.getValue().bitcastToAPInt().getZExtValue();
    putInt(b, off, bits, sizeOf(t));
    return;
  }
  if (auto s = dyn_cast<StringAttr>(a)) {
    for (size_t i = 0; i < s.getValue().size() && off + i < b.size(); i++) b[off + i] = uint8_t(s.getValue()[i]);
    return;
  }
  if (auto d = dyn_cast<DenseElementsAttr>(a)) {
    auto at = dyn_cast<LL::LLVMArrayType>(t);
    if (!at) unsupported("dense constant of non-array type");
    Type et = at.getElementType();
    uint64_t es = sizeOf(et), i = 0;
    if (isa<IntegerType>(et)) {
      for (const APInt &v : d.getValues<APInt>()) putInt(b, off + es * i++, v.getZExtValue(), es);
    } else if (isFloat(et)) {
      for (const APFloat &v : d.getValues<APFloat>())
        putInt(b, off + es * i++, v.bitcastToAPInt().getZExtValue(), es);
    } else {
      unsupported("dense constant element type");
    }
    return;
  }
  if (auto arr = dyn_cast<ArrayAttr>(a)) {
    auto at = dyn_cast<LL::LLVMArrayType>(t);
    if (!at) unsupported("array attribute for non-array");
    uint64_t es = sizeOf(at.getElementType());
    for (size_t i = 0; i < arr.size(); i++) attrBytes(arr[i], at.getElementType(), b, off + es * i);
    return;
  }
  if (isa<LL::ZeroAttr>(a) || isa<LL::UndefAttr>(a)) return;
  unsupported("constant attribute");
}

ConstVal evalInitializer(LL::GlobalOp g) {
  Type t = g.getGlobalType();
  ConstVal out;
  out.bytes.assign(sizeOf(t), 0);
  if (auto v = g.getValueOrNull()) {
    attrBytes(v, t, out.bytes, 0);
    return out;
  }
  Region &r = g.getInitializerRegion();
  if (r.empty()) return out;
  llvm::DenseMap<Value, ConstVal> vals;
  for (Operation &op : r.front()) {
    llvm::TypeSwitch<Operation *>(&op)
        .Case<LL::ConstantOp>([&](auto c) {
          ConstVal cv;
          cv.bytes.assign(sizeOf(c.getType()), 0);
          attrBytes(c.getValue(), c.getType(), cv.bytes, 0);
          vals[c.getResult()] = cv;
        })
        .Case<LL::ZeroOp, LL::UndefOp, LL::PoisonOp>([&](auto z) {
          ConstVal cv;
          cv.bytes.assign(sizeOf(z.getType()), 0);
          vals[z.getResult()] = cv;
        })
        .Case<LL::AddressOfOp>([&](auto a) {
          ConstVal cv;
          cv.bytes.assign(8, 0);
          cv.rels.push_back({0, machoName(a.getGlobalName()), 0});
          vals[a.getResult()] = cv;
        })
        .Case<LL::GEPOp>([&](auto gep) {
          auto base = vals.find(gep.getBase());
          if (base == vals.end() || base->second.rels.size() != 1) unsupported("constant gep base");
          int64_t off = 0;
          Type et = gep.getElemType();
          bool first = true;
          for (auto idx : gep.getIndices()) {
            auto ia = llvm::dyn_cast_if_present<IntegerAttr>(idx);
            if (!ia) unsupported("constant gep with a dynamic index");
            int64_t k = ia.getInt();
            if (first) {
              off += k * int64_t(sizeOf(et));
              first = false;
            } else if (auto s = dyn_cast<LL::LLVMStructType>(et)) {
              off += int64_t(fieldOffset(s, unsigned(k)));
              et = s.getBody()[k];
            } else if (auto at = dyn_cast<LL::LLVMArrayType>(et)) {
              off += k * int64_t(sizeOf(at.getElementType()));
              et = at.getElementType();
            }
          }
          ConstVal cv = base->second;
          cv.rels[0].addend += off;
          vals[gep.getResult()] = cv;
        })
        .Case<LL::InsertValueOp>([&](auto ins) {
          ConstVal cv = vals.lookup(ins.getContainer());
          if (cv.bytes.empty()) cv.bytes.assign(sizeOf(ins.getType()), 0);
          auto [off, ft] = elementAt(ins.getType(), ins.getPosition());
          const ConstVal &v = vals[ins.getValue()];
          for (size_t i = 0; i < v.bytes.size(); i++) cv.bytes[off + i] = v.bytes[i];
          for (auto rel : v.rels) cv.rels.push_back({off + rel.off, rel.sym, rel.addend});
          (void)ft;
          vals[ins.getResult()] = cv;
        })
        .Case<LL::ReturnOp>([&](auto ret) { out = vals[ret.getOperand(0)]; })
        .Default([&](Operation *o) { unsupported("initializer op " + o->getName().getStringRef().str()); });
  }
  return out;
}

bool isLocalLinkage(LL::Linkage l) { return l == LL::Linkage::Internal || l == LL::Linkage::Private; }
bool isWeakLinkage(LL::Linkage l) {
  return l == LL::Linkage::Linkonce || l == LL::Linkage::LinkonceODR || l == LL::Linkage::Weak ||
         l == LL::Linkage::WeakODR;
}

void ModuleGen::emitGlobal(LL::GlobalOp g) {
  if (g.getThreadLocal_()) unsupported("thread local global");
  ConstVal cv = evalInitializer(g);
  std::string name = machoName(g.getSymName());
  uint64_t align = std::max<uint64_t>(g.getAlignment().value_or(0), alignOf(g.getGlobalType()));
  bool zero = cv.rels.empty() && std::all_of(cv.bytes.begin(), cv.bytes.end(), [](uint8_t b) { return b == 0; });
  bool local = isLocalLinkage(g.getLinkage()), weak = isWeakLinkage(g.getLinkage());
  if (zero && !g.getConstant() && !weak) {
    w.reserveBss(0, unsigned(align));
    w.define(name, Sect::Bss, w.size(Sect::Bss), !local);
    w.reserveBss(std::max<uint64_t>(cv.bytes.size(), 1), 1);
    return;
  }
  // Read-only data without pointers can live in __TEXT,__const; anything
  // with relocations goes to __DATA.
  Sect s = g.getConstant() && cv.rels.empty() ? Sect::Const : Sect::Data;
  w.align(s, unsigned(align));
  uint64_t at = w.size(s);
  w.define(name, s, at, !local, weak);
  w.emitData(s, cv.bytes.data(), cv.bytes.size());
  if (cv.bytes.empty()) {
    uint8_t z = 0;
    w.emitData(s, &z, 1);
  }
  for (auto &r : cv.rels) w.reloc(s, at + r.off, Reloc::Abs64, w.symbol(r.sym), r.addend);
}

bool ModuleGen::isDefinedData(StringRef name) const {
  return definedData.count(StringAttr::get(ctx, name)) != 0;
}

// ---- function level ----

class FuncGen {
public:
  FuncGen(ModuleGen &mg, LL::LLVMFuncOp f) : mg(mg), w(mg.w), fn(f) {}
  void run();

private:
  ModuleGen &mg;
  MachOWriter &w;
  LL::LLVMFuncOp fn;

  llvm::DenseMap<Value, uint64_t> slot;        // value -> frame offset from sp
  llvm::DenseMap<Operation *, uint64_t> allocaStorage;
  llvm::DenseMap<Block *, uint64_t> blockStart;
  struct Fixup {
    uint64_t at;
    Block *dest;
    enum { B, BCond, Cbnz } kind;
    Cond cond;
    unsigned reg;
  };
  std::vector<Fixup> fixups;
  uint64_t frameSize = 0, tempArea = 0, tempSize = 0;

  // Frame layout.
  uint64_t alloc(uint64_t size, uint64_t align) {
    frameSize = (frameSize + align - 1) & ~(align - 1);
    uint64_t at = frameSize;
    frameSize += size;
    return at;
  }
  void layout();

  // Instructions.
  void e(uint32_t w32) { w.emit32(w32); }
  uint64_t here() const { return w.size(Sect::Text); }
  void addrOf(unsigned rd, uint64_t off);  // rd = sp + off
  void ldSlot(unsigned r, uint64_t off, uint64_t size);
  void stSlot(unsigned r, uint64_t off, uint64_t size);
  void movImm(unsigned rd, uint64_t v);
  void copyBytes(unsigned dstBase, uint64_t dstOff, unsigned srcBase, uint64_t srcOff, uint64_t n);
  void memLoad(unsigned rt, unsigned base, uint64_t size);
  void memStore(unsigned rt, unsigned base, uint64_t size);

  // Values.
  uint64_t of(Value v);
  void get(unsigned r, Value v);            // scalar into a general register, canonical
  void getSigned(unsigned r, Value v);      // sign extended to 64 bits
  void put(unsigned r, Value v);            // stores a scalar, normalizing the width
  void getF(unsigned vr, Value v);          // float into an FP register
  void putF(unsigned vr, Value v);
  void normalize(unsigned r, Type t);

  void emitOp(Operation &op);
  void emitCall(LL::CallOp call);
  void emitReturn(LL::ReturnOp ret);
  void edgeMoves(Block *dest, OperandRange args);
  void branchTo(Block *dest);
  void prologue();
  void epilogue();
};

void FuncGen::layout() {
  frameSize = 0;
  // Function arguments are the entry block's arguments.
  for (Block &b : fn.getBody()) {
    for (BlockArgument a : b.getArguments()) slot[a] = alloc(sizeOf(a.getType()) < 8 ? 8 : sizeOf(a.getType()), 8);
    for (Operation &op : b) {
      for (Value r : op.getResults()) {
        uint64_t sz = sizeOf(r.getType());
        slot[r] = alloc(sz < 8 ? 8 : sz, std::max<uint64_t>(8, alignOf(r.getType())));
      }
      if (auto a = dyn_cast<LL::AllocaOp>(op)) {
        if (&b != &fn.getBody().front()) unsupported("alloca outside the entry block");
        auto c = a.getArraySize().getDefiningOp<LL::ConstantOp>();
        if (!c) unsupported("dynamic alloca");
        uint64_t n = cast<IntegerAttr>(c.getValue()).getValue().getZExtValue();
        uint64_t al = std::max<uint64_t>(a.getAlignment().value_or(0), alignOf(a.getElemType()));
        allocaStorage[&op] = alloc(std::max<uint64_t>(n * sizeOf(a.getElemType()), 1), std::max<uint64_t>(al, 8));
      }
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

void FuncGen::addrOf(unsigned rd, uint64_t off) {
  if (off < 4096) {
    e(addImm(rd, SP, uint32_t(off)));
  } else {
    if (off >= (uint64_t(1) << 24)) unsupported("frame larger than 16 MiB");
    e(addImm(rd, SP, uint32_t(off >> 12), true, true));
    e(addImm(rd, rd, uint32_t(off & 0xFFF)));
  }
}

void FuncGen::ldSlot(unsigned r, uint64_t off, uint64_t size) {
  unsigned ls = log2Size(size);
  if (off % size == 0 && (off >> ls) < 4096) {
    e(ldrImm(r, SP, uint32_t(off), ls));
    return;
  }
  addrOf(RT, off);
  e(ldrImm(r, RT, 0, ls));
}

void FuncGen::stSlot(unsigned r, uint64_t off, uint64_t size) {
  unsigned ls = log2Size(size);
  if (off % size == 0 && (off >> ls) < 4096) {
    e(strImm(r, SP, uint32_t(off), ls));
    return;
  }
  addrOf(RT, off);
  e(strImm(r, RT, 0, ls));
}

void FuncGen::movImm(unsigned rd, uint64_t v) {
  if (~v < 0x10000) {
    e(movn(rd, uint32_t(~v & 0xFFFF), 0));
    return;
  }
  bool first = true;
  for (unsigned hw = 0; hw < 4; hw++) {
    uint32_t chunk = uint32_t(v >> (16 * hw)) & 0xFFFF;
    if (chunk == 0 && !(hw == 3 && first)) continue;
    e(first ? movz(rd, chunk, hw) : movk(rd, chunk, hw));
    first = false;
  }
  if (first) e(movz(rd, 0, 0));
}

// Copies n bytes from [srcBase + srcOff] to [dstBase + dstOff] (bases are
// registers, sp allowed) through x14, with both addresses in x12 and x13
// first so every access uses a small aligned offset.
void FuncGen::copyBytes(unsigned dstBase, uint64_t dstOff, unsigned srcBase, uint64_t srcOff, uint64_t n) {
  auto base = [&](unsigned out, unsigned reg, uint64_t off) {
    if (reg == SP) {
      addrOf(out, off);
      return;
    }
    if (off == 0) {
      e(movReg(out, reg));
    } else if (off < 4096) {
      e(addImm(out, reg, uint32_t(off)));
    } else {
      movImm(out, off);
      e(addReg(out, out, reg));
    }
  };
  base(RA, srcBase, srcOff);  // x13 first: dstBase may be x13 only if it is RA, never the case
  base(R4, dstBase, dstOff);
  uint64_t i = 0;
  for (; i + 8 <= n; i += 8) {
    e(ldrImm(14, RA, uint32_t(i), 3));
    e(strImm(14, R4, uint32_t(i), 3));
  }
  for (uint64_t sz = 4; sz >= 1; sz /= 2)
    for (; i + sz <= n; i += sz) {
      unsigned ls = log2Size(sz);
      e(ldrImm(14, RA, uint32_t(i), ls));
      e(strImm(14, R4, uint32_t(i), ls));
    }
}

void FuncGen::memLoad(unsigned rt, unsigned base, uint64_t size) { e(ldrImm(rt, base, 0, log2Size(size))); }
void FuncGen::memStore(unsigned rt, unsigned base, uint64_t size) { e(strImm(rt, base, 0, log2Size(size))); }

uint64_t FuncGen::of(Value v) {
  auto it = slot.find(v);
  if (it == slot.end()) unsupported("value without a slot");
  return it->second;
}

void FuncGen::normalize(unsigned r, Type t) {
  unsigned wdt = intWidth(t);
  if (wdt < 64) e(ubfx0(r, r, wdt));
}

void FuncGen::get(unsigned r, Value v) { ldSlot(r, of(v), 8); }

void FuncGen::getSigned(unsigned r, Value v) {
  get(r, v);
  unsigned wdt = intWidth(v.getType());
  if (wdt < 64) e(sbfx0(r, r, wdt));
}

void FuncGen::put(unsigned r, Value v) {
  if (isFloat(v.getType())) {
    stSlot(r, of(v), 8);
    return;
  }
  normalize(r, v.getType());
  stSlot(r, of(v), 8);
}

void FuncGen::getF(unsigned vr, Value v) {
  bool dbl = v.getType().isF64();
  get(R1, v);
  e(fmovFromGp(vr, R1, dbl));
}

void FuncGen::putF(unsigned vr, Value v) {
  bool dbl = v.getType().isF64();
  e(fmovToGp(R1, vr, dbl));
  if (!dbl) e(uxtw(R1, R1));
  stSlot(R1, of(v), 8);
}

void FuncGen::prologue() {
  e(stpPre(FP, LR, SP, -16));
  e(movSp(FP, SP));
  if (frameSize) {
    if (frameSize < 4096) {
      e(subImm(SP, SP, uint32_t(frameSize)));
    } else {
      e(subImm(SP, SP, uint32_t(frameSize >> 12), true, true));
      if (frameSize & 0xFFF) e(subImm(SP, SP, uint32_t(frameSize & 0xFFF)));
    }
  }
  // Incoming arguments, split as LLVM splits them, into the entry block's
  // argument slots.
  unsigned ngp = 0, nfp = 0;
  for (BlockArgument a : fn.getBody().front().getArguments()) {
    std::vector<std::pair<uint64_t, Type>> parts;
    flatten(a.getType(), 0, parts);
    for (auto [off, t] : parts) {
      if (isFloat(t)) {
        if (nfp >= 8) unsupported("arguments on the stack");
        e(fmovToGp(R1, nfp++, t.isF64()));
        if (t.isF32()) e(uxtw(R1, R1));
        if (isAggregate(a.getType())) {
          addrOf(R2, of(a) + off);
          memStore(R1, R2, sizeOf(t));
        } else {
          stSlot(R1, of(a), 8);
        }
      } else {
        if (ngp >= 8) unsupported("arguments on the stack");
        unsigned r = ngp++;
        if (isAggregate(a.getType())) {
          addrOf(R2, of(a) + off);
          memStore(r, R2, sizeOf(t));
        } else {
          e(movReg(R1, r));
          normalize(R1, t);
          stSlot(R1, of(a), 8);
        }
      }
    }
  }
}

void FuncGen::epilogue() {
  e(movSp(SP, FP));
  e(ldpPost(FP, LR, SP, 16));
  e(ret());
}

void FuncGen::edgeMoves(Block *dest, OperandRange args) {
  if (args.empty()) return;
  // Parallel move: sources to the temporary area, then to the arguments.
  uint64_t t = tempArea;
  std::vector<uint64_t> tmp;
  for (Value v : args) {
    uint64_t sz = (sizeOf(v.getType()) + 7) & ~uint64_t(7);
    copyBytes(SP, t, SP, of(v), std::max<uint64_t>(sz, 8));
    tmp.push_back(t);
    t += std::max<uint64_t>(sz, 8);
  }
  for (unsigned i = 0; i < args.size(); i++) {
    BlockArgument a = dest->getArgument(i);
    uint64_t sz = (sizeOf(a.getType()) + 7) & ~uint64_t(7);
    copyBytes(SP, of(a), SP, tmp[i], std::max<uint64_t>(sz, 8));
  }
}

void FuncGen::branchTo(Block *dest) {
  fixups.push_back({here(), dest, Fixup::B, AL, 0});
  e(b(0));
}

void FuncGen::emitCall(LL::CallOp call) {
  if (call.getVarCalleeType()) unsupported("variadic call");
  auto args = call.getArgOperands();
  // Arguments split into register pieces, as LLVM's calling convention
  // lowering assigns them.
  struct Piece {
    Value v;
    uint64_t off;
    Type t;
  };
  std::vector<Piece> gp, fp;
  for (Value a : args) {
    std::vector<std::pair<uint64_t, Type>> parts;
    flatten(a.getType(), 0, parts);
    for (auto [off, t] : parts) (isFloat(t) ? fp : gp).push_back({a, off, t});
  }
  if (gp.size() > 8 || fp.size() > 8) unsupported("arguments on the stack");
  for (unsigned i = 0; i < fp.size(); i++) {
    Piece &p = fp[i];
    if (isAggregate(p.v.getType())) {
      addrOf(R2, of(p.v) + p.off);
      memLoad(R1, R2, sizeOf(p.t));
    } else {
      get(R1, p.v);
    }
    e(fmovFromGp(i, R1, p.t.isF64()));
  }
  // Indirect callee first (into x16, not an argument register).
  bool indirect = !call.getCallee();
  if (indirect) get(17, call.getCalleeOperands().front());
  for (unsigned i = 0; i < gp.size(); i++) {
    Piece &p = gp[i];
    if (isAggregate(p.v.getType())) {
      addrOf(R2, of(p.v) + p.off);
      memLoad(i, R2, sizeOf(p.t));
    } else {
      get(i, p.v);
    }
  }
  if (indirect) {
    e(blr(17));
  } else {
    StringRef callee = *call.getCallee();
    if (callee == "marmot_gpu_for") {
      // CPU path: the call returns false.
      e(movz(0, 0, 0));
    } else {
      uint64_t at = here();
      e(bl(0));
      w.reloc(Sect::Text, at, Reloc::Branch26, w.symbol(machoName(callee)));
    }
  }
  if (call.getNumResults() == 0) return;
  Value r = call.getResult();
  std::vector<std::pair<uint64_t, Type>> parts;
  flatten(r.getType(), 0, parts);
  unsigned ngp = 0, nfp = 0;
  for (auto [off, t] : parts) {
    if (isFloat(t) ? nfp >= 8 : ngp >= 8) unsupported("result returned in memory");
    if (isFloat(t)) {
      e(fmovToGp(R1, nfp++, t.isF64()));
      if (t.isF32()) e(uxtw(R1, R1));
    } else {
      e(movReg(R1, ngp++));
      normalize(R1, t);
    }
    if (isAggregate(r.getType())) {
      addrOf(R2, of(r) + off);
      memStore(R1, R2, sizeOf(t));
    } else {
      stSlot(R1, of(r), 8);
    }
  }
}

void FuncGen::emitReturn(LL::ReturnOp ret) {
  if (ret.getNumOperands() == 1) {
    Value v = ret.getOperand(0);
    std::vector<std::pair<uint64_t, Type>> parts;
    flatten(v.getType(), 0, parts);
    unsigned ngp = 0, nfp = 0;
    for (auto [off, t] : parts) {
      if (isFloat(t) ? nfp >= 8 : ngp >= 8) unsupported("result returned in memory");
      if (isAggregate(v.getType())) {
        addrOf(R2, of(v) + off);
        memLoad(R1, R2, sizeOf(t));
      } else {
        get(R1, v);
      }
      if (isFloat(t))
        e(fmovFromGp(nfp++, R1, t.isF64()));
      else
        e(movReg(ngp++, R1));
    }
  }
  epilogue();
}

Cond icmpCond(LL::ICmpPredicate p) {
  switch (p) {
  case LL::ICmpPredicate::eq: return EQ;
  case LL::ICmpPredicate::ne: return NE;
  case LL::ICmpPredicate::slt: return LT;
  case LL::ICmpPredicate::sle: return LE;
  case LL::ICmpPredicate::sgt: return GT;
  case LL::ICmpPredicate::sge: return GE;
  case LL::ICmpPredicate::ult: return LO;
  case LL::ICmpPredicate::ule: return LS;
  case LL::ICmpPredicate::ugt: return HI;
  case LL::ICmpPredicate::uge: return HS;
  }
  return AL;
}

bool isSignedPred(LL::ICmpPredicate p) {
  return p == LL::ICmpPredicate::slt || p == LL::ICmpPredicate::sle || p == LL::ICmpPredicate::sgt ||
         p == LL::ICmpPredicate::sge;
}

void FuncGen::emitOp(Operation &op) {
  auto bin = [&](auto enc) {
    get(R1, op.getOperand(0));
    get(R2, op.getOperand(1));
    e(enc(R1, R1, R2));
    put(R1, op.getResult(0));
  };
  auto fbin = [&](auto enc) {
    bool dbl = op.getResult(0).getType().isF64();
    getF(F1, op.getOperand(0));
    getF(F2, op.getOperand(1));
    e(enc(F1, F1, F2, dbl));
    putF(F1, op.getResult(0));
  };
  llvm::TypeSwitch<Operation *>(&op)
      .Case<LL::ConstantOp>([&](auto c) {
        Type t = c.getType();
        uint64_t bits = 0;
        if (auto i = dyn_cast<IntegerAttr>(c.getValue()))
          bits = i.getValue().getZExtValue();
        else if (auto f = dyn_cast<FloatAttr>(c.getValue()))
          bits = f.getValue().bitcastToAPInt().getZExtValue();
        else
          unsupported("constant kind");
        if (isAggregate(t)) unsupported("aggregate constant");
        movImm(R1, bits);
        put(R1, c.getResult());
      })
      .Case<LL::ZeroOp, LL::UndefOp, LL::PoisonOp>([&](auto z) {
        Type t = z.getType();
        uint64_t n = sizeOf(t);
        n = std::max<uint64_t>((n + 7) & ~uint64_t(7), 8);
        for (uint64_t i = 0; i < n; i += 8) stSlot(XZR, of(z.getResult()) + i, 8);
      })
      .Case<LL::AddressOfOp>([&](auto a) {
        std::string name = machoName(a.getGlobalName());
        unsigned sym = w.symbol(name);
        uint64_t at = here();
        e(adrp(R1));
        if (mg.isDefinedData(a.getGlobalName())) {
          w.reloc(Sect::Text, at, Reloc::Page21, sym);
          at = here();
          e(addImm(R1, R1, 0));
          w.reloc(Sect::Text, at, Reloc::PageOff12, sym);
        } else {
          w.reloc(Sect::Text, at, Reloc::GotPage21, sym);
          at = here();
          e(ldrImm(R1, R1, 0, 3));
          w.reloc(Sect::Text, at, Reloc::GotPageOff12, sym);
        }
        put(R1, a.getResult());
      })
      .Case<LL::AddOp>([&](auto) { bin([](unsigned d, unsigned n, unsigned m) { return addReg(d, n, m); }); })
      .Case<LL::SubOp>([&](auto) { bin([](unsigned d, unsigned n, unsigned m) { return subReg(d, n, m); }); })
      .Case<LL::MulOp>([&](auto) { bin([](unsigned d, unsigned n, unsigned m) { return mul(d, n, m); }); })
      .Case<LL::AndOp>([&](auto) { bin([](unsigned d, unsigned n, unsigned m) { return andReg(d, n, m); }); })
      .Case<LL::OrOp>([&](auto) { bin([](unsigned d, unsigned n, unsigned m) { return orrReg(d, n, m); }); })
      .Case<LL::XOrOp>([&](auto) { bin([](unsigned d, unsigned n, unsigned m) { return eorReg(d, n, m); }); })
      .Case<LL::ShlOp>([&](auto) { bin([](unsigned d, unsigned n, unsigned m) { return lslv(d, n, m); }); })
      .Case<LL::LShrOp>([&](auto) { bin([](unsigned d, unsigned n, unsigned m) { return lsrv(d, n, m); }); })
      .Case<LL::UDivOp>([&](auto) { bin([](unsigned d, unsigned n, unsigned m) { return udiv(d, n, m); }); })
      .Case<LL::AShrOp>([&](auto o) {
        getSigned(R1, o.getLhs());
        get(R2, o.getRhs());
        e(asrv(R1, R1, R2));
        put(R1, o.getResult());
      })
      .Case<LL::SDivOp>([&](auto o) {
        getSigned(R1, o.getLhs());
        getSigned(R2, o.getRhs());
        e(sdiv(R1, R1, R2));
        put(R1, o.getResult());
      })
      .Case<LL::SRemOp>([&](auto o) {
        getSigned(R1, o.getLhs());
        getSigned(R2, o.getRhs());
        e(sdiv(R3, R1, R2));
        e(msub(R1, R3, R2, R1));
        put(R1, o.getResult());
      })
      .Case<LL::URemOp>([&](auto o) {
        get(R1, o.getLhs());
        get(R2, o.getRhs());
        e(udiv(R3, R1, R2));
        e(msub(R1, R3, R2, R1));
        put(R1, o.getResult());
      })
      .Case<LL::ICmpOp>([&](auto c) {
        if (isSignedPred(c.getPredicate())) {
          getSigned(R1, c.getLhs());
          getSigned(R2, c.getRhs());
        } else {
          get(R1, c.getLhs());
          get(R2, c.getRhs());
        }
        e(cmpReg(R1, R2));
        e(cset(R1, icmpCond(c.getPredicate())));
        put(R1, c.getResult());
      })
      .Case<LL::SelectOp>([&](auto s) {
        Type t = s.getType();
        get(R3, s.getCondition());
        if (isAggregate(t)) {
          // Copy whichever value the condition picks.
          uint64_t sz = std::max<uint64_t>((sizeOf(t) + 7) & ~uint64_t(7), 8);
          uint64_t at = here();
          e(cbz(R3, 0));
          copyBytes(SP, of(s.getResult()), SP, of(s.getTrueValue()), sz);
          uint64_t skip = here();
          e(b(0));
          uint64_t elseAt = here();
          copyBytes(SP, of(s.getResult()), SP, of(s.getFalseValue()), sz);
          uint64_t end = here();
          auto patch = [&](uint64_t pos, uint32_t word) { std::memcpy(&w.bytes(Sect::Text)[pos], &word, 4); };
          patch(at, cbz(R3, int32_t(elseAt - at)));
          patch(skip, b(int32_t(end - skip)));
          return;
        }
        get(R1, s.getTrueValue());
        get(R2, s.getFalseValue());
        e(cmpImm(R3, 0));
        e(csel(R1, R1, R2, NE));
        put(R1, s.getResult());
      })
      .Case<LL::ZExtOp, LL::TruncOp, LL::PtrToIntOp, LL::IntToPtrOp>([&](auto c) {
        get(R1, c.getArg());
        put(R1, c.getResult());
      })
      .Case<LL::SExtOp>([&](auto c) {
        getSigned(R1, c.getArg());
        put(R1, c.getResult());
      })
      .Case<LL::BitcastOp>([&](auto c) {
        if (isAggregate(c.getType())) unsupported("aggregate bitcast");
        get(R1, c.getArg());
        stSlot(R1, of(c.getResult()), 8);
      })
      .Case<LL::FAddOp>([&](auto) { fbin([](unsigned d, unsigned n, unsigned m, bool x) { return fadd(d, n, m, x); }); })
      .Case<LL::FSubOp>([&](auto) { fbin([](unsigned d, unsigned n, unsigned m, bool x) { return fsub(d, n, m, x); }); })
      .Case<LL::FMulOp>([&](auto) { fbin([](unsigned d, unsigned n, unsigned m, bool x) { return fmul(d, n, m, x); }); })
      .Case<LL::FDivOp>([&](auto) { fbin([](unsigned d, unsigned n, unsigned m, bool x) { return fdiv(d, n, m, x); }); })
      .Case<LL::FNegOp>([&](auto o) {
        bool dbl = o.getType().isF64();
        getF(F1, o.getOperand());
        e(fneg(F1, F1, dbl));
        putF(F1, o.getResult());
      })
      .Case<LL::FCmpOp>([&](auto c) {
        bool dbl = c.getLhs().getType().isF64();
        getF(F1, c.getLhs());
        getF(F2, c.getRhs());
        e(fcmp(F1, F2, dbl));
        using P = LL::FCmpPredicate;
        auto one = [&](Cond k) { e(cset(R1, k)); };
        auto either = [&](Cond k1, Cond k2) {
          e(cset(R1, k1));
          e(cset(R2, k2));
          e(orrReg(R1, R1, R2));
        };
        switch (c.getPredicate()) {
        case P::_false: e(movz(R1, 0, 0)); break;
        case P::_true: e(movz(R1, 1, 0)); break;
        case P::oeq: one(EQ); break;
        case P::ogt: one(GT); break;
        case P::oge: one(GE); break;
        case P::olt: one(MI); break;
        case P::ole: one(LS); break;
        case P::one: either(MI, GT); break;
        case P::ord: one(VC); break;
        case P::ueq: either(EQ, VS); break;
        case P::ugt: one(HI); break;
        case P::uge: one(PL); break;
        case P::ult: one(LT); break;
        case P::ule: one(LE); break;
        case P::une: one(NE); break;
        case P::uno: one(VS); break;
        }
        put(R1, c.getResult());
      })
      .Case<LL::SIToFPOp, LL::UIToFPOp>([&](auto c) {
        bool dbl = c.getType().isF64();
        bool sgn = isa<LL::SIToFPOp>(c.getOperation());
        if (sgn)
          getSigned(R1, c.getArg());
        else
          get(R1, c.getArg());
        e(sgn ? scvtf(F1, R1, dbl) : ucvtf(F1, R1, dbl));
        putF(F1, c.getResult());
      })
      .Case<LL::FPToSIOp, LL::FPToUIOp>([&](auto c) {
        bool dbl = c.getArg().getType().isF64();
        getF(F1, c.getArg());
        e(isa<LL::FPToSIOp>(c.getOperation()) ? fcvtzs(R1, F1, dbl) : fcvtzu(R1, F1, dbl));
        put(R1, c.getResult());
      })
      .Case<LL::FPExtOp>([&](auto c) {
        getF(F1, c.getArg());
        e(fcvtStoD(F1, F1));
        putF(F1, c.getResult());
      })
      .Case<LL::FPTruncOp>([&](auto c) {
        getF(F1, c.getArg());
        e(fcvtDtoS(F1, F1));
        putF(F1, c.getResult());
      })
      .Case<LL::LoadOp>([&](auto l) {
        Type t = l.getType();
        get(R2, l.getAddr());
        if (isAggregate(t)) {
          copyBytes(SP, of(l.getResult()), R2, 0, sizeOf(t));
          return;
        }
        uint64_t sz = sizeOf(t);
        if (l.getOrdering() != LL::AtomicOrdering::not_atomic)
          e(ldar(R1, R2, log2Size(sz)));
        else
          memLoad(R1, R2, sz);
        stSlot(R1, of(l.getResult()), 8);
        if (intWidth(t) == 1) {
          e(ubfx0(R1, R1, 1));
          stSlot(R1, of(l.getResult()), 8);
        }
      })
      .Case<LL::StoreOp>([&](auto s) {
        Type t = s.getValue().getType();
        get(R2, s.getAddr());
        if (isAggregate(t)) {
          copyBytes(R2, 0, SP, of(s.getValue()), sizeOf(t));
          return;
        }
        get(R1, s.getValue());
        uint64_t sz = sizeOf(t);
        if (s.getOrdering() != LL::AtomicOrdering::not_atomic)
          e(stlr(R1, R2, log2Size(sz)));
        else
          memStore(R1, R2, sz);
      })
      .Case<LL::AllocaOp>([&](auto a) {
        addrOf(R1, allocaStorage[a.getOperation()]);
        stSlot(R1, of(a.getResult()), 8);
      })
      .Case<LL::GEPOp>([&](auto g) {
        get(R1, g.getBase());
        Type et = g.getElemType();
        bool first = true;
        int64_t constOff = 0;
        for (auto idx : g.getIndices()) {
          Type cur = et;
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
          (void)cur;
          first = false;
          if (auto ia = llvm::dyn_cast_if_present<IntegerAttr>(idx)) {
            constOff += ia.getInt() * int64_t(scale);
          } else {
            Value iv = llvm::cast<Value>(idx);
            getSigned(R2, iv);
            if (scale != 1) {
              movImm(R3, scale);
              e(mul(R2, R2, R3));
            }
            e(addReg(R1, R1, R2));
          }
        }
        if (constOff) {
          movImm(R2, uint64_t(constOff));
          e(addReg(R1, R1, R2));
        }
        stSlot(R1, of(g.getResult()), 8);
      })
      .Case<LL::ExtractValueOp>([&](auto x) {
        auto [off, t] = elementAt(x.getContainer().getType(), x.getPosition());
        uint64_t src = of(x.getContainer()) + off;
        if (isAggregate(t)) {
          copyBytes(SP, of(x.getResult()), SP, src, std::max<uint64_t>((sizeOf(t) + 7) & ~uint64_t(7), 8));
          return;
        }
        addrOf(R2, src);
        memLoad(R1, R2, sizeOf(t));
        stSlot(R1, of(x.getResult()), 8);
      })
      .Case<LL::InsertValueOp>([&](auto ins) {
        Type at = ins.getType();
        uint64_t sz = std::max<uint64_t>((sizeOf(at) + 7) & ~uint64_t(7), 8);
        copyBytes(SP, of(ins.getResult()), SP, of(ins.getContainer()), sz);
        auto [off, t] = elementAt(at, ins.getPosition());
        uint64_t dst = of(ins.getResult()) + off;
        if (isAggregate(t)) {
          copyBytes(SP, dst, SP, of(ins.getValue()), sizeOf(t));
          return;
        }
        get(R1, ins.getValue());
        addrOf(R2, dst);
        memStore(R1, R2, sizeOf(t));
      })
      .Case<LL::CallOp>([&](auto c) { emitCall(c); })
      .Case<LL::MemcpyOp, LL::MemmoveOp>([&](auto m) {
        get(0, m.getDst());
        get(1, m.getSrc());
        get(2, m.getLen());
        uint64_t at = here();
        e(bl(0));
        w.reloc(Sect::Text, at, Reloc::Branch26, w.symbol("_memmove"));
      })
      .Case<LL::MemsetOp>([&](auto m) {
        get(0, m.getDst());
        get(1, m.getVal());
        get(2, m.getLen());
        uint64_t at = here();
        e(bl(0));
        w.reloc(Sect::Text, at, Reloc::Branch26, w.symbol("_memset"));
      })
      .Case<LL::FMAOp, LL::FMulAddOp>([&](auto f) {
        bool dbl = f.getType().isF64();
        getF(F1, f.getA());
        getF(F2, f.getB());
        getF(F3, f.getC());
        e(fmadd(F1, F1, F2, F3, dbl));
        putF(F1, f.getResult());
      })
      .Case<LL::AtomicRMWOp>([&](auto a) {
        Type t = a.getVal().getType();
        unsigned ls = log2Size(sizeOf(t));
        get(R2, a.getPtr());
        get(R3, a.getVal());
        using B = LL::AtomicBinOp;
        switch (a.getBinOp()) {
        case B::add: e(ldaddal(R3, R1, R2, ls)); break;
        case B::sub:
          e(neg(R3, R3));
          e(ldaddal(R3, R1, R2, ls));
          break;
        case B::_and:
          e(mvn(R3, R3));
          e(ldclral(R3, R1, R2, ls));
          break;
        case B::_or: e(ldsetal(R3, R1, R2, ls)); break;
        case B::_xor: e(ldeoral(R3, R1, R2, ls)); break;
        case B::xchg: e(swpal(R3, R1, R2, ls)); break;
        default: unsupported("atomicrmw operation");
        }
        put(R1, a.getResult());
      })
      .Case<LL::AtomicCmpXchgOp>([&](auto x) {
        Type t = x.getVal().getType();
        unsigned ls = log2Size(sizeOf(t));
        get(R2, x.getPtr());
        get(R1, x.getCmp());
        e(movReg(R4, R1));
        get(R3, x.getVal());
        e(casal(R1, R3, R2, ls));  // R1 <- old
        // Result {old, success}.
        uint64_t res = of(x.getResult());
        auto st = cast<LL::LLVMStructType>(x.getType());
        addrOf(RA, res + fieldOffset(st, 0));
        memStore(R1, RA, sizeOf(t));
        normalize(R1, t);
        normalize(R4, t);
        e(cmpReg(R1, R4));
        e(cset(R1, EQ));
        addrOf(RA, res + fieldOffset(st, 1));
        memStore(R1, RA, 1);
      })
      .Case<LL::FenceOp>([&](auto) { e(dmbIsh()); })
      .Case<LL::UnreachableOp>([&](auto) { e(brk(1)); })
      .Case<LL::ReturnOp>([&](auto r) { emitReturn(r); })
      .Case<LL::BrOp>([&](auto br_) {
        edgeMoves(br_.getDest(), br_.getDestOperands());
        branchTo(br_.getDest());
      })
      .Case<LL::CondBrOp>([&](auto c) {
        get(R3, c.getCondition());
        // cbnz to the true edge's moves, the false edge falls through.
        uint64_t at = here();
        e(cbnz(R3, 0));
        edgeMoves(c.getFalseDest(), c.getFalseDestOperands());
        branchTo(c.getFalseDest());
        uint64_t trueAt = here();
        uint32_t word = cbnz(R3, int32_t(trueAt - at));
        std::memcpy(&w.bytes(Sect::Text)[at], &word, 4);
        edgeMoves(c.getTrueDest(), c.getTrueDestOperands());
        branchTo(c.getTrueDest());
      })
      .Case<LL::SwitchOp>([&](auto s) {
        get(R3, s.getValue());
        auto cases = s.getCaseValues();
        if (!cases) {
          edgeMoves(s.getDefaultDestination(), s.getDefaultOperands());
          branchTo(s.getDefaultDestination());
          return;
        }
        unsigned i = 0;
        for (const APInt &v : cases->template getValues<APInt>()) {
          movImm(R4, v.getZExtValue() & (intWidth(s.getValue().getType()) == 64
                                             ? ~uint64_t(0)
                                             : ((uint64_t(1) << intWidth(s.getValue().getType())) - 1)));
          e(cmpReg(R3, R4));
          uint64_t at = here();
          e(bcond(NE, 0));
          edgeMoves(s.getCaseDestinations()[i], s.getCaseOperands(i));
          branchTo(s.getCaseDestinations()[i]);
          uint32_t word = bcond(NE, int32_t(here() - at));
          std::memcpy(&w.bytes(Sect::Text)[at], &word, 4);
          i++;
        }
        edgeMoves(s.getDefaultDestination(), s.getDefaultOperands());
        branchTo(s.getDefaultDestination());
      })
      .Default([&](Operation *o) { unsupported("op " + o->getName().getStringRef().str()); });
}

void FuncGen::run() {
  if (fn.isVarArg()) unsupported("variadic function");
  layout();
  uint64_t start = here();
  prologue();
  for (Block &b : fn.getBody()) {
    blockStart[&b] = here();
    for (Operation &op : b) emitOp(op);
  }
  for (const Fixup &f : fixups) {
    int32_t off = int32_t(int64_t(blockStart[f.dest]) - int64_t(f.at));
    uint32_t word = b(off);
    std::memcpy(&w.bytes(Sect::Text)[f.at], &word, 4);
  }
  // Defined only now: a function that falls back leaves no symbol.
  auto lk = fn.getLinkage();
  w.define(machoName(fn.getSymName()), Sect::Text, start, !isLocalLinkage(lk), isWeakLinkage(lk));
}

bool ModuleGen::run(std::string &fragment, std::vector<std::string> &fallback,
                    std::vector<std::string> &reasons, Stats &stats, std::string &err) {
  for (Operation &op : module.getBody()->getOperations()) {
    if (auto f = dyn_cast<LL::LLVMFuncOp>(op)) {
      if (!f.isExternal()) definedData[f.getSymNameAttr()] = true;
    } else if (auto g = dyn_cast<LL::GlobalOp>(op)) {
      if (g.getValueOrNull() || !g.getInitializerRegion().empty()) definedData[g.getSymNameAttr()] = true;
    }
  }
  for (Operation &op : module.getBody()->getOperations()) {
    if (auto g = dyn_cast<LL::GlobalOp>(op)) {
      if (!definedData.count(g.getSymNameAttr())) continue;  // a declaration
      try {
        emitGlobal(g);
        stats.globals++;
      } catch (const Unsupported &u) {
        err = "global " + g.getSymName().str() + ": " + u.why;
        return false;
      }
    }
  }
  for (Operation &op : module.getBody()->getOperations()) {
    auto f = dyn_cast<LL::LLVMFuncOp>(op);
    if (!f || f.isExternal()) continue;
    // A function that falls back leaves no code or relocations behind.
    auto cp = w.checkpoint();
    try {
      FuncGen(*this, f).run();
      stats.functions++;
    } catch (const Unsupported &u) {
      w.rollback(cp);
      fallback.push_back(f.getSymName().str());
      reasons.push_back(u.why);
      stats.fallbacks++;
    }
  }
  stats.codeBytes = w.size(Sect::Text);
  fragment.clear();  // line tables come later
  return true;
}

} // namespace

bool emit(ModuleOp module, const std::string &outObject, std::string &fragment, std::vector<std::string> &fallback,
          std::vector<std::string> &reasons, Stats &stats, std::string &err) {
  ModuleGen mg(module);
  if (!mg.run(fragment, fallback, reasons, stats, err)) return false;
  return mg.w.write(outObject, err);
}

} // namespace tier0
} // namespace marmot
