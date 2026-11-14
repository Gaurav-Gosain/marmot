// Module.h: the module level of tier0: globals, the functions (on threads),
// the pc table fragment and what is left for LLVM.
#ifndef MARMOT_TIER0_MODULE_H
#define MARMOT_TIER0_MODULE_H

#include "Common.h"
#include "MachO.h"
#include "Tier0.h"

#include "mlir/IR/BuiltinOps.h"
#include "llvm/ADT/DenseMap.h"
#include "llvm/ADT/DenseSet.h"
#include "llvm/ADT/StringMap.h"
#include "llvm/ADT/StringSet.h"

#include <tuple>

namespace marmot {
namespace tier0 {

class ModuleGen {
public:
  ModuleGen(ModuleOp m, const std::string &unit, bool x64) : module(m), ctx(m.getContext()), unit(unit), x64(x64) {
    if (x64) w.setElf();
  }
  bool run(std::string &fragment, std::vector<std::string> &fallback, std::vector<std::string> &reasons,
           Stats &stats, std::string &err);
  MachOWriter w;

  // Symbols defined in this module (functions with bodies and globals with
  // initializers): reached directly; everything else through the GOT.
  llvm::DenseMap<StringAttr, bool> definedData;
  llvm::StringSet<> definedNames;  // the same by name (no context access)
  // Threads for the functions (emit's threads).
  unsigned threads = 1;
  void emitFunctions(std::vector<std::string> &fallback, std::vector<std::string> &reasons, Stats &stats);
  // Functions declared returns_twice (rt_setjmp): their callers get no
  // register allocation.
  llvm::DenseSet<StringAttr> returnsTwice;
  // Functions by name, for the parameter attributes of a direct callee.
  llvm::DenseMap<StringAttr, LL::LLVMFuncOp> funcs;
  // Value types seen and known to tier0 (checkFunction).
  llvm::DenseSet<Type> knownTypes;
  void checkFunction(LL::LLVMFuncOp fn);
  // Whether argument i of call (or the result of fn) is llvm.signext: the
  // caller (callee) extends the narrow integer to 64 bits by its sign.
  // Tier0 keeps narrow integers zero extended, which covers llvm.zeroext.
  bool signExtArg(LL::CallOp call, unsigned i) const {
    auto has = [](DictionaryAttr d) { return d && d.get("llvm.signext"); };
    if (auto aa = call.getArgAttrsAttr(); aa && i < aa.size() && has(dyn_cast<DictionaryAttr>(aa[i])))
      return true;
    if (auto callee = call.getCalleeAttr())
      if (auto it = funcs.find(callee.getAttr()); it != funcs.end()) {
        LL::LLVMFuncOp f = it->second;
        return i < f.getNumArguments() && has(f.getArgAttrDict(i));
      }
    return false;
  }
  static bool signExtResult(LL::LLVMFuncOp fn) {
    auto ra = fn.getResAttrsAttr();
    auto d = ra && ra.size() == 1 ? dyn_cast<DictionaryAttr>(ra[0]) : DictionaryAttr();
    return d && d.get("llvm.signext");
  }
  bool isDefinedData(StringRef name) const;

  // Line rows of the functions emitted, for the pc table fragment.
  using Row = std::tuple<uint64_t, uint32_t, uint32_t>;  // offset, line, file
  struct Lines {
    unsigned sym;
    uint64_t size;
    std::vector<Row> rows;
    // Stack maps (MARMOT_STACKMAPS=1): (nwords, bits) and (return pc offset,
    // map) of each call, for the pc table (Pcln.h StackMap).
    std::vector<std::pair<uint32_t, std::string>> maps;
    std::vector<std::pair<uint64_t, uint32_t>> sites;
  };
  std::vector<Lines> lines;
  std::vector<std::string> files;
  llvm::StringMap<uint32_t> fileIndex;
  uint32_t fileId(StringRef f) {
    auto [it, fresh] = fileIndex.try_emplace(f, uint32_t(files.size()));
    if (fresh) files.push_back(f.str());
    return it->second;
  }

private:
  ModuleOp module;
  MLIRContext *ctx;
  std::string unit;
  bool x64;
  void emitGlobal(LL::GlobalOp g);
};

// The function generators, one per target (EmitArm64.cpp, EmitX64.cpp).
void runX64(ModuleGen &mg, LL::LLVMFuncOp f);
void runArm64(ModuleGen &mg, LL::LLVMFuncOp f);

} // namespace tier0
} // namespace marmot

#endif
