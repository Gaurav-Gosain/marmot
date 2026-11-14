// Tier0.h: direct code generation from the lowered llvm dialect module to a
// relocatable object, without LLVM (see DESIGN.md).
#ifndef MARMOT_TIER0_TIER0_H
#define MARMOT_TIER0_TIER0_H

#include <cstdint>
#include <map>
#include <string>
#include <vector>

namespace mlir {
class ModuleOp;
}

namespace marmot {
namespace tier0 {

// The object kind: arm64 Mach-O (macOS) or x86-64 ELF (Linux).
enum class Target { Arm64MachO, X64Elf };

struct Stats {
  unsigned functions = 0;  // compiled by tier0
  unsigned fallbacks = 0;  // left to LLVM
  unsigned globals = 0;
  size_t codeBytes = 0;
};

// Emits every function of module that tier0 supports, and every global,
// into a relocatable object at outObject. Functions it cannot compile are
// left in module and their names appended to fallback (with the first reason
// in reasons, same index). The module is rewritten in place so that what is
// left is exactly what LLVM must compile into a second object: when some
// function falls back, internal and private symbols get names unique to unit
// (any string unique per object, e.g. the object path's hash) and become
// hidden globals, so both objects see them; functions tier0 compiled become
// declarations; globals become declarations. fragment receives the pc table
// fragment (Pcln.h format: files, functions, line rows from the operations'
// locations) for the emitted functions. Returns false with err set on an
// internal error.
bool emit(mlir::ModuleOp module, const std::string &unit, const std::string &outObject,
          std::string &fragment, std::vector<std::string> &fallback, std::vector<std::string> &reasons,
          Stats &stats, std::string &err, Target target = Target::Arm64MachO, unsigned threads = 1);

// With TIER0_OPSTATS set, the arm64 code bytes of every emit so far by
// operation (name, ".agg" for an aggregate result or operand, and
// "prologue"), for finding what makes code large.
std::map<std::string, uint64_t> opBytes();

} // namespace tier0
} // namespace marmot

#endif
