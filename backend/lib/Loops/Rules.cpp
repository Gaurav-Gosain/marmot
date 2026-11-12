// Rules.cpp: which loop rules -par may use.
//
// -par covers the rules that are proved or checked by certificate: Rule P
// (same-base windows, strided form, noalias pairs, integer reductions,
// polynomial inductions, the structural clauses) and Rule V for affine and
// nil checks. Every other rule has its own switch, off until it is proved
// or checked; -loops-all-rules turns them all on (tests, fuzzing, benchmarks
// of the full optimizer).

#include "Internal.h"

#include "llvm/Support/CommandLine.h"

using namespace llvm;

namespace marmot {
namespace loops {

static cl::opt<bool> AllRules("loops-all-rules", cl::Hidden,
                              cl::desc("Loop optimizer: every rule, signed off or not"));
static cl::opt<bool> RtChecks("loops-rtchecks", cl::Hidden,
                              cl::desc("Rule P: run time range checks and row families"));
static cl::opt<bool> Privatize("loops-privatize", cl::Hidden,
                               cl::desc("Rule P: privatized reduction arrays (histograms)"));
static cl::opt<bool> BcvGrowth("loops-bcv-growth", cl::Hidden,
                               cl::desc("Rule V: append growth branches"));
static cl::opt<bool> BcvLoaded("loops-bcv-loaded", cl::Hidden,
                               cl::desc("Rule V: loaded bounds (check loops)"));
static cl::opt<bool> ByteCount("loops-bytecount", cl::Hidden,
                               cl::desc("Rule D: byte count loops to rt_count_byte"));
static cl::opt<bool> Scan("loops-scan", cl::Hidden, cl::desc("Rule D: parallel prefix scans"));
static cl::opt<bool> InterchangeOn("loops-interchange", cl::Hidden,
                                   cl::desc("Rule I: reduction interchange"));
static cl::opt<bool> JamOn("loops-jam", cl::Hidden, cl::desc("loop-jam of chunk loops"));
static cl::opt<bool> MaskedJamOn("loops-masked-jam", cl::Hidden,
                                 cl::desc("Rule M: masked jam (outer loop vectorization)"));

bool ruleOn(Rule r) {
  if (AllRules)
    return true;
  switch (r) {
  case Rule::RtChecks:
    return RtChecks;
  case Rule::Privatize:
    return Privatize;
  case Rule::BcvGrowth:
    return BcvGrowth;
  case Rule::BcvLoaded:
    return BcvLoaded;
  case Rule::ByteCount:
    return ByteCount;
  case Rule::Scan:
    return Scan;
  case Rule::Interchange:
    return InterchangeOn;
  case Rule::Jam:
    return JamOn;
  case Rule::MaskedJam:
    return MaskedJamOn;
  }
  return false;
}

const char *ruleFlag(Rule r) {
  switch (r) {
  case Rule::RtChecks:
    return "-loops-rtchecks";
  case Rule::Privatize:
    return "-loops-privatize";
  case Rule::BcvGrowth:
    return "-loops-bcv-growth";
  case Rule::BcvLoaded:
    return "-loops-bcv-loaded";
  case Rule::ByteCount:
    return "-loops-bytecount";
  case Rule::Scan:
    return "-loops-scan";
  case Rule::Interchange:
    return "-loops-interchange";
  case Rule::Jam:
    return "-loops-jam";
  case Rule::MaskedJam:
    return "-loops-masked-jam";
  }
  return "";
}

} // namespace loops
} // namespace marmot
