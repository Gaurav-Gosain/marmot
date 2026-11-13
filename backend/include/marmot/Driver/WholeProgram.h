// WholeProgram.h: -wpo batch mode.
#ifndef MARMOT_DRIVER_WHOLEPROGRAM_H
#define MARMOT_DRIVER_WHOLEPROGRAM_H

#include "marmot/Driver/Pipeline.h"

#include <atomic>
#include <functional>
#include <map>
#include <string>
#include <vector>

namespace llvm {
class raw_ostream;
}

namespace marmot {

struct WpoJob {
  std::string in, out;
  // A key of the input's contents given by the caller (the jobs file's third
  // field): the input is then read only when a cached result is missing.
  std::string key;
};

struct WpoStats {
  unsigned functions = 0, liveFunctions = 0, internalized = 0, imported = 0;
  unsigned escapeRounds = 0, noEscapeParams = 0;
  unsigned cacheHits = 0, cacheMisses = 0, prunedTypes = 0;
  std::atomic<unsigned> devirtualized{0};
  std::atomic<unsigned> compiled{0}; // objects optimized and emitted (not from the cache)
  unsigned splitModules = 0, groups = 0; // -split-large
  // -tier0: functions tier0 compiled and left to LLVM, by first reason.
  unsigned tier0Functions = 0, tier0Fallbacks = 0, tier0Rest = 0;
  std::atomic<unsigned> tier1Hits{0}; // -tier1-spool: -O2 objects of background tier 1 used
  std::map<std::string, unsigned> tier0Reasons;
};

// Compiles a batch: escape summaries are propagated across modules, then
// each job gets its object. With wpo the jobs are one program: functions no
// root reaches are dropped, symbols used by one module only become internal,
// and small functions are imported across modules for inlining. A non-empty
// cacheDir caches every step. Returns false if any job
// failed.
bool compileWholeProgram(const std::vector<WpoJob> &jobs, const CompileOptions &opts,
                         unsigned threads, bool wpo, const std::string &cacheDir,
                         PhaseTimes &total, WpoStats &stats, llvm::raw_ostream &errs);

// -tier0 batch whose jobs arrive one by one: next fills in a job and returns
// true, or returns false at the end. Each job starts as it arrives (dev mode
// has no cross-module phase), with memory capped by MARMOT_MAX_MEM_MB.
bool compileTier0(const std::function<bool(WpoJob &)> &next, const CompileOptions &opts,
                  unsigned threads, const std::string &cacheDir, PhaseTimes &total,
                  WpoStats &stats, llvm::raw_ostream &errs);

// Background tier 1: compiles the modules a tier0 batch left in
// opts.tier1Spool (KEY.mlir, KEY.fp) at -O2, one at a time, newest first,
// and caches each object under KEY, where the next tier0 batch with the
// same options looks first. opts are the tier0 batch's. A second runner on
// the same spool returns at once. False if a module failed (it is dropped).
bool runTier1(const CompileOptions &opts, const std::string &cacheDir, llvm::raw_ostream &errs);

} // namespace marmot

#endif
