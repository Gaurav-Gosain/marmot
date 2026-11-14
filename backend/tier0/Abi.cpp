// Abi.cpp: see Abi.h.
#include "Abi.h"

#include "X64.h"

namespace marmot {
namespace tier0 {

namespace X = marmot::tier0::x64;


uint64_t blockKey(Type t) {
  if (t.isF32()) return 1;
  if (t.isF64()) return 2;
  if (isa<LL::LLVMPointerType>(t) || intWidth(t) == 64) return 3;
  return 0;
}

std::vector<ArgLoc> assignArgs(TypeRange types, uint64_t &stackSize) {
  std::vector<ArgLoc> out;
  unsigned ngp = 0, nfp = 0;
  uint64_t sp = 0;
  auto onStack = [&](ArgLoc &l, uint64_t align) {
    sp = (sp + align - 1) & ~(align - 1);
    l.stack = sp;
    sp += sizeOf(l.t);
  };
  for (unsigned i = 0; i < types.size(); i++) {
    std::vector<std::pair<uint64_t, Type>> parts;
    flatten(types[i], 0, parts);
    bool block = false;
    if (isa<LL::LLVMArrayType>(types[i]) && !parts.empty()) {
      uint64_t k = blockKey(parts[0].second);
      block = k != 0;
      for (auto &p : parts) block = block && blockKey(p.second) == k;
    }
    if (block) {
      bool fp = isFloat(parts[0].second);
      unsigned &n = fp ? nfp : ngp;
      bool regs = n + parts.size() <= 8;
      uint64_t align = std::min<uint64_t>(alignOf(types[i]), 16);
      if (!regs) n = 8;
      for (unsigned j = 0; j < parts.size(); j++) {
        ArgLoc l{i, parts[j].first, parts[j].second, fp, -1, 0};
        if (regs)
          l.reg = int(n++);
        else
          onStack(l, j == 0 ? align : 1);
        out.push_back(l);
      }
      continue;
    }
    for (auto [off, t] : parts) {
      ArgLoc l{i, off, t, isFloat(t), -1, 0};
      unsigned &n = l.fp ? nfp : ngp;
      if (n < 8)
        l.reg = int(n++);
      else
        onStack(l, sizeOf(t));
      out.push_back(l);
    }
  }
  stackSize = (sp + 15) & ~uint64_t(15);
  return out;
}

// True when LLVM returns a value of type t in memory (through the pointer
// in x8): more than 8 general or 8 floating point pieces.
bool returnsInMemory(Type t) {
  std::vector<std::pair<uint64_t, Type>> parts;
  flatten(t, 0, parts);
  unsigned ngp = 0, nfp = 0;
  for (auto &p : parts) (isFloat(p.second) ? nfp : ngp)++;
  return ngp > 8 || nfp > 8;
}

// Where LLVM's x86-64 SysV lowering puts each piece of the arguments:
// integers and pointers in rdi, rsi, rdx, rcx, r8, r9, floats in xmm0-7,
// the rest in 8-byte stack slots in order.
std::vector<ArgLoc> assignArgsX64(TypeRange types, uint64_t &stackSize, bool sret) {
  static const unsigned gpr[6] = {X::RDI, X::RSI, X::RDX, X::RCX, X::R8, X::R9};
  std::vector<ArgLoc> out;
  unsigned ngp = sret ? 1 : 0, nfp = 0;
  uint64_t sp = 0;
  for (unsigned i = 0; i < types.size(); i++) {
    std::vector<std::pair<uint64_t, Type>> parts;
    flatten(types[i], 0, parts);
    for (auto [off, t] : parts) {
      ArgLoc l{i, off, t, isFloat(t), -1, 0};
      if (l.fp ? nfp < 8 : ngp < 6) {
        l.reg = l.fp ? int(nfp++) : int(gpr[ngp++]);
      } else {
        l.stack = sp;
        sp += 8;
      }
      out.push_back(l);
    }
  }
  stackSize = (sp + 15) & ~uint64_t(15);
  return out;
}

// Results: up to three integer pieces (rax, rdx, rcx) and four float pieces
// (xmm0, xmm1, then the x87 st0, st1), as llc assigns them; more and the
// result goes through a pointer passed in rdi, returned again in rax.
bool returnsInMemoryX64(Type t) {
  std::vector<std::pair<uint64_t, Type>> parts;
  flatten(t, 0, parts);
  unsigned ngp = 0, nfp = 0;
  for (auto &p : parts) (isFloat(p.second) ? nfp : ngp)++;
  return ngp > 3 || nfp > 4;
}

const unsigned retGpr[3] = {X::RAX, X::RDX, X::RCX};


} // namespace tier0
} // namespace marmot
