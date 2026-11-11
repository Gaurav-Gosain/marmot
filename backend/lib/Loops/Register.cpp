// Register.cpp: puts the loop passes into the backend's LLVM pipeline.
#include "marmot/Loops/Loops.h"
#include "Internal.h"

#include "llvm/Passes/PassBuilder.h"

using namespace llvm;

namespace marmot {
namespace loops {

void registerLLVMPasses(PassBuilder &pb, const Options &opts) {
  if (!opts.enable)
    return;
  // After inlining and loop canonicalization, before the function optimizer
  // pipeline (and so before the vectorizer).
  pb.registerOptimizerEarlyEPCallback(
      [opts](ModulePassManager &mpm, OptimizationLevel ol, ThinOrFullLTOPhase) {
        if (ol.getSpeedupLevel() < 2 || ol.getSizeLevel() > 0)
          return;
        if (opts.parallel)
          addParallelize(mpm, opts);
      });
}

} // namespace loops
} // namespace marmot
