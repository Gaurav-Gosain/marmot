// MARMOT_IRSTATS=<file>: per compiled unit, one JSON line appended to file
// with the LLVM IR size before and after the optimizer (functions, blocks,
// instructions, debug records, instructions with a location, distinct
// DILocations, the largest functions), the optimizer's exclusive time per
// pass and analysis, how often each pass ran and how often it changed
// nothing (it preserved all analyses), and the optimizer and code generation times. For build
// time profiles (docs/status/llvm-profile.md); it changes no output.
#pragma once

#include "llvm/ADT/StringMap.h"
#include <chrono>
#include <cstdint>
#include <string>
#include <utility>
#include <vector>

namespace llvm {
class Module;
class PassInstrumentationCallbacks;
} // namespace llvm

namespace marmot {

// The file named by MARMOT_IRSTATS, or null when it is not set.
const char *irStatsPath();

struct IRCounts {
  uint64_t fns = 0, blocks = 0, insts = 0, dbgRecords = 0, withLoc = 0, distinctLocs = 0;
  std::vector<std::pair<std::string, uint64_t>> top; // largest functions by instructions
};

IRCounts countIR(const llvm::Module &m, unsigned topN);

// MARMOT_IRSTATS_FNS=<text>: per defined function whose name contains text,
// its instructions by kind and its direct callees.
const char *irStatsFnFilter();

struct FnCounts {
  std::string name;
  uint64_t insts = 0, blocks = 0, cold = 0; // cold: in blocks ending in unreachable
  uint64_t loads = 0, stores = 0, geps = 0, cmps = 0, branches = 0, phis = 0, calls = 0,
           icalls = 0, allocas = 0, casts = 0, arith = 0, other = 0;
  std::vector<std::pair<std::string, uint32_t>> callees;
};

std::vector<FnCounts> countFns(const llvm::Module &m, const char *filter);

// Exclusive wall time per pass and analysis of one pipeline run: the time
// between two callbacks goes to the innermost pass or analysis running.
struct PassClock {
  llvm::StringMap<double> ms;
  llvm::StringMap<uint64_t> runs;
  llvm::StringMap<uint64_t> idle; // runs that preserved all analyses
  std::vector<std::string> stack;
  std::chrono::steady_clock::time_point last;
  void registerOn(llvm::PassInstrumentationCallbacks &pic);
  void charge();
  void push(llvm::StringRef name);
  void pop();
};

// Set by optimizeAndEmit around runLLVMPipeline when MARMOT_IRSTATS is set.
extern thread_local PassClock *currentPassClock;

void writeIRStats(const std::string &unit, const IRCounts &pre, const IRCounts &post,
                  const PassClock &clock, double optMs, double codegenMs,
                  const std::vector<FnCounts> &preFns = {},
                  const std::vector<FnCounts> &postFns = {});

} // namespace marmot
