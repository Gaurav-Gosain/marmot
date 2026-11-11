// Loops.h: the marmot loop optimizer (owner: loops). See docs/loops.md.
//
// The backend links MarmotLoops and calls registerLLVMPasses on the PassBuilder
// it uses for the default -O2/-O3 pipeline.
#ifndef MARMOT_LOOPS_LOOPS_H
#define MARMOT_LOOPS_LOOPS_H

namespace llvm {
class PassBuilder;
} // namespace llvm

namespace marmot {
namespace loops {

struct Options {
  bool enable = true;   // master switch (-no-loops turns it off)
  bool parallel = true; // loop-parallelize: outline loops onto rt_parallel_for (-no-par)
  bool fastFP = false;  // -ffast-fp: allow reordering floating point reductions
  bool remarks = false; // -loops-remarks: print why each loop was or was not transformed
};

// Adds the loop passes to the default pipelines built from pb afterwards.
// Only -O2 and -O3 get them.
void registerLLVMPasses(llvm::PassBuilder &pb, const Options &opts);

} // namespace loops
} // namespace marmot

#endif
