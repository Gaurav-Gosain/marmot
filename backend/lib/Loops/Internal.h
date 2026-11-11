// Internal.h: pass constructors shared inside MarmotLoops.
#ifndef MARMOT_LOOPS_INTERNAL_H
#define MARMOT_LOOPS_INTERNAL_H

#include "marmot/Loops/Loops.h"
#include "llvm/IR/PassManager.h"

namespace marmot {
namespace loops {

// loop-parallelize (Parallelize.cpp).
void addParallelize(llvm::ModulePassManager &mpm, const Options &opts);

} // namespace loops
} // namespace marmot

#endif
