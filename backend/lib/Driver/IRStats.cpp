// MARMOT_IRSTATS: see marmot/Driver/IRStats.h.

#include "marmot/Driver/IRStats.h"

#include "llvm/ADT/DenseSet.h"
#include "llvm/IR/DebugInfoMetadata.h"
#include "llvm/IR/Module.h"
#include "llvm/IR/PassInstrumentation.h"
#include "llvm/Support/FileSystem.h"
#include "llvm/Support/JSON.h"
#include "llvm/Support/raw_ostream.h"

#include <algorithm>
#include <cstdlib>

namespace marmot {

thread_local PassClock *currentPassClock = nullptr;

const char *irStatsPath() {
  static const char *p = [] {
    const char *e = std::getenv("MARMOT_IRSTATS");
    return e && *e ? e : nullptr;
  }();
  return p;
}

IRCounts countIR(const llvm::Module &m, unsigned topN) {
  IRCounts c;
  llvm::DenseSet<const llvm::DILocation *> locs;
  for (const llvm::Function &f : m) {
    if (f.isDeclaration())
      continue;
    ++c.fns;
    uint64_t n = 0;
    for (const llvm::BasicBlock &bb : f) {
      ++c.blocks;
      for (const llvm::Instruction &i : bb) {
        ++n;
        c.dbgRecords += std::distance(i.getDbgRecordRange().begin(), i.getDbgRecordRange().end());
        if (const llvm::DILocation *l = i.getDebugLoc().get()) {
          ++c.withLoc;
          locs.insert(l);
        }
      }
    }
    c.insts += n;
    c.top.emplace_back(f.getName().str(), n);
  }
  c.distinctLocs = locs.size();
  auto mid = c.top.begin() + std::min<size_t>(topN, c.top.size());
  std::partial_sort(c.top.begin(), mid, c.top.end(),
                    [](const auto &a, const auto &b) { return a.second > b.second; });
  c.top.erase(mid, c.top.end());
  return c;
}

void PassClock::charge() {
  auto now = std::chrono::steady_clock::now();
  if (!stack.empty())
    ms[stack.back()] += std::chrono::duration<double, std::milli>(now - last).count();
  last = now;
}

void PassClock::push(llvm::StringRef name) {
  charge();
  stack.push_back(name.str());
  ++runs[name];
}

void PassClock::pop() {
  charge();
  if (!stack.empty())
    stack.pop_back();
}

void PassClock::registerOn(llvm::PassInstrumentationCallbacks &pic) {
  last = std::chrono::steady_clock::now();
  pic.registerBeforeNonSkippedPassCallback([this](llvm::StringRef p, llvm::Any) { push(p); });
  pic.registerAfterPassCallback(
      [this](llvm::StringRef, llvm::Any, const llvm::PreservedAnalyses &) { pop(); });
  pic.registerAfterPassInvalidatedCallback(
      [this](llvm::StringRef, const llvm::PreservedAnalyses &) { pop(); });
  pic.registerBeforeAnalysisCallback(
      [this](llvm::StringRef p, llvm::Any) { push(("analysis:" + p).str()); });
  pic.registerAfterAnalysisCallback([this](llvm::StringRef, llvm::Any) { pop(); });
}

static llvm::json::Object counts(const IRCounts &c) {
  llvm::json::Array top;
  for (const auto &[name, n] : c.top)
    top.push_back(llvm::json::Array{name, n});
  return llvm::json::Object{{"fns", c.fns},
                            {"blocks", c.blocks},
                            {"insts", c.insts},
                            {"dbg_records", c.dbgRecords},
                            {"with_loc", c.withLoc},
                            {"distinct_locs", c.distinctLocs},
                            {"top", std::move(top)}};
}

void writeIRStats(const std::string &unit, const IRCounts &pre, const IRCounts &post,
                  const PassClock &clock, double optMs, double codegenMs) {
  const char *path = irStatsPath();
  if (!path)
    return;
  llvm::json::Object passes;
  for (const auto &e : clock.ms)
    passes[e.getKey()] = llvm::json::Array{e.getValue(), clock.runs.lookup(e.getKey())};
  llvm::json::Object o{{"unit", unit},     {"pre", counts(pre)},       {"post", counts(post)},
                       {"opt_ms", optMs},  {"codegen_ms", codegenMs}, {"passes", std::move(passes)}};
  std::string line;
  llvm::raw_string_ostream(line) << llvm::json::Value(std::move(o)) << "\n";
  // One write per line with O_APPEND, so parallel units and backend
  // processes do not interleave their lines.
  std::error_code ec;
  llvm::raw_fd_ostream os(path, ec, llvm::sys::fs::OF_Append);
  if (ec)
    return;
  os.SetUnbuffered();
  os << line;
}

} // namespace marmot
