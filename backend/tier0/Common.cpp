// Common.cpp: see Common.h.
#include "Common.h"

namespace marmot {
namespace tier0 {

[[noreturn]] void unsupported(const std::string &why) { throw Unsupported{why}; }

thread_local bool elfNames = false;
std::mutex opBytesMu;
std::map<std::string, uint64_t> opBytesAll;
const bool opStatsOn = getenv("TIER0_OPSTATS") != nullptr;

uint64_t sizeOf(Type t) {
  if (auto i = dyn_cast<IntegerType>(t)) {
    unsigned w = i.getWidth();
    if (w == 128) return 16;
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
  if (sizeOf(t) > 8) unsupported("i128 in a call or aggregate value");  // also rejects unsupported scalars
  out.push_back({base, t});
}

bool scalarConst(Value v, uint64_t &bits) {
  Operation *d = v.getDefiningOp();
  if (!d || isAggregate(v.getType()) || sizeOf(v.getType()) > 8) return false;
  if (isa<LL::ZeroOp, LL::UndefOp, LL::PoisonOp>(d)) {
    bits = 0;
    return true;
  }
  auto c = dyn_cast<LL::ConstantOp>(d);
  if (!c) return false;
  if (auto i = dyn_cast<IntegerAttr>(c.getValue()))
    bits = i.getValue().getZExtValue();
  else if (auto f = dyn_cast<FloatAttr>(c.getValue()))
    bits = f.getValue().bitcastToAPInt().getZExtValue();
  else
    return false;
  unsigned wdt = isFloat(v.getType()) ? unsigned(8 * sizeOf(v.getType())) : intWidth(v.getType());
  if (wdt < 64) bits &= (uint64_t(1) << wdt) - 1;
  return true;
}

bool isSignedPred(LL::ICmpPredicate p) {
  return p == LL::ICmpPredicate::slt || p == LL::ICmpPredicate::sle || p == LL::ICmpPredicate::sgt ||
         p == LL::ICmpPredicate::sge;
}

std::optional<std::pair<StringRef, unsigned>> lineOf(Location l) {
  if (auto f = dyn_cast<FileLineColLoc>(l)) {
    // Line 0, and positions in the input module itself (ops the front end
    // gave no location, before the backend renames them).
    StringRef n = f.getFilename().getValue();
    if (f.getLine() == 0 || n.ends_with(".mlir") || n.ends_with(".mlirbc")) return std::nullopt;
    return std::make_pair(f.getFilename().getValue(), f.getLine());
  }
  if (auto n = dyn_cast<NameLoc>(l)) return lineOf(n.getChildLoc());
  if (auto c = dyn_cast<CallSiteLoc>(l)) {
    if (auto r = lineOf(c.getCallee())) return r;
    return lineOf(c.getCaller());
  }
  if (auto f = dyn_cast<FusedLoc>(l))
    for (Location s : f.getLocations())
      if (auto r = lineOf(s)) return r;
  return std::nullopt;
}

} // namespace tier0
} // namespace marmot
