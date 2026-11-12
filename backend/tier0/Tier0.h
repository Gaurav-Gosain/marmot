// Tier0.h: direct code generation from the lowered llvm dialect module to a
// relocatable object, without LLVM (see DESIGN.md).
#ifndef MARMOT_TIER0_TIER0_H
#define MARMOT_TIER0_TIER0_H

#include <string>
#include <vector>

namespace mlir {
class ModuleOp;
}

namespace marmot {
namespace tier0 {

struct Stats {
  unsigned functions = 0;  // compiled by tier0
  unsigned fallbacks = 0;  // left to LLVM
  unsigned globals = 0;
  size_t codeBytes = 0;
};

// Emits every function of module that tier0 supports, and every global,
// into a relocatable object at outObject. Functions it cannot compile are
// left out and their names appended to fallback (with the first reason in
// reasons, same index); the caller compiles those with LLVM into a second
// object, where every global the module defines must then be declared only.
// fragment receives the pc table fragment (Pcln.h format) for the emitted
// functions. Returns false with err set on an internal error.
bool emit(mlir::ModuleOp module, const std::string &outObject, std::string &fragment,
          std::vector<std::string> &fallback, std::vector<std::string> &reasons, Stats &stats,
          std::string &err);

} // namespace tier0
} // namespace marmot

#endif
