// WholeProgram.h: -wpo batch mode. See docs/passes.md (whole-program mode).
#ifndef MARMOT_DRIVER_WHOLEPROGRAM_H
#define MARMOT_DRIVER_WHOLEPROGRAM_H

#include "marmot/Driver/Pipeline.h"

#include <atomic>
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
  unsigned escapeRounds = 0, noEscapeParams = 0;
  unsigned cacheHits = 0, cacheMisses = 0, prunedTypes = 0;
  std::atomic<unsigned> devirtualized{0};
  std::atomic<unsigned> compiled{0}; // objects optimized and emitted (not from the cache)
};

// Compiles a batch: escape summaries are propagated across modules, then
// each job gets its object. With wpo the jobs are one program: functions no
// root reaches are dropped, symbols used by one module only become internal,
// and small functions are imported across modules for inlining. A non-empty
// cacheDir caches every step (see docs/passes.md). Returns false if any job
// failed.
bool compileWholeProgram(const std::vector<WpoJob> &jobs, const CompileOptions &opts,
                         unsigned threads, bool wpo, const std::string &cacheDir,
                         PhaseTimes &total, WpoStats &stats, llvm::raw_ostream &errs);

} // namespace marmot

#endif
