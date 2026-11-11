// WholeProgram.h: -wpo batch mode. See docs/passes.md (whole-program mode).
#ifndef MARMOT_DRIVER_WHOLEPROGRAM_H
#define MARMOT_DRIVER_WHOLEPROGRAM_H

#include "marmot/Driver/Pipeline.h"

#include <string>
#include <vector>

namespace llvm {
class raw_ostream;
}

namespace marmot {

struct WpoJob {
  std::string in, out;
};

struct WpoStats {
  unsigned functions = 0, liveFunctions = 0, internalized = 0, imported = 0;
};

// Compiles all jobs as one program: one object per job, but functions that
// no root reaches are dropped, symbols used by one module only become
// internal, and small functions are imported across modules for inlining.
// Returns false if any job failed.
bool compileWholeProgram(const std::vector<WpoJob> &jobs, const CompileOptions &opts,
                         unsigned threads, PhaseTimes &total, WpoStats &stats,
                         llvm::raw_ostream &errs);

} // namespace marmot

#endif
