// MARMOT_IRSTATS: see marmot/Driver/IRStats.h.

#include "marmot/Driver/IRStats.h"

#include "llvm/ADT/DenseSet.h"
#include "llvm/ADT/StringMap.h"
#include "llvm/IR/DebugInfoMetadata.h"
#include "llvm/IR/Instructions.h"
#include "llvm/IR/IntrinsicInst.h"
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

const char *irStatsFnFilter() {
  static const char *p = [] {
    const char *e = std::getenv("MARMOT_IRSTATS_FNS");
    return e && *e ? e : nullptr;
  }();
  return p;
}

std::vector<FnCounts> countFns(const llvm::Module &m, const char *filter) {
  std::vector<FnCounts> out;
  if (!filter)
    return out;
  for (const llvm::Function &f : m) {
    if (f.isDeclaration() || !f.getName().contains(filter))
      continue;
    FnCounts c;
    c.name = f.getName().str();
    llvm::StringMap<uint32_t> callees;
    for (const llvm::BasicBlock &bb : f) {
      ++c.blocks;
      bool cold = llvm::isa<llvm::UnreachableInst>(bb.getTerminator());
      for (const llvm::Instruction &i : bb) {
        ++c.insts;
        if (cold)
          ++c.cold;
        if (llvm::isa<llvm::LoadInst>(i))
          ++c.loads;
        else if (llvm::isa<llvm::StoreInst>(i))
          ++c.stores;
        else if (llvm::isa<llvm::GetElementPtrInst>(i))
          ++c.geps;
        else if (llvm::isa<llvm::CmpInst>(i))
          ++c.cmps;
        else if (llvm::isa<llvm::BranchInst>(i) || llvm::isa<llvm::SwitchInst>(i))
          ++c.branches;
        else if (llvm::isa<llvm::PHINode>(i))
          ++c.phis;
        else if (llvm::isa<llvm::AllocaInst>(i))
          ++c.allocas;
        else if (llvm::isa<llvm::CastInst>(i))
          ++c.casts;
        else if (llvm::isa<llvm::BinaryOperator>(i))
          ++c.arith;
        else if (auto *cb = llvm::dyn_cast<llvm::CallBase>(&i)) {
          ++c.calls;
          if (const llvm::Function *t = cb->getCalledFunction())
            ++callees[t->getName()];
          else if (cb->isInlineAsm())
            ++callees["asm"];
          else
            ++c.icalls;
        } else
          ++c.other;
      }
    }
    for (const auto &e : callees)
      c.callees.emplace_back(e.getKey().str(), e.getValue());
    out.push_back(std::move(c));
  }
  return out;
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
      [this](llvm::StringRef p, llvm::Any, const llvm::PreservedAnalyses &pa) {
        if (pa.areAllPreserved())
          ++idle[p];
        pop();
      });
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

static llvm::json::Array fnsJSON(const std::vector<FnCounts> &fs) {
  llvm::json::Array a;
  for (const FnCounts &c : fs) {
    llvm::json::Object callees;
    for (const auto &[n, k] : c.callees)
      callees[n] = k;
    a.push_back(llvm::json::Object{
        {"name", c.name},       {"insts", c.insts},   {"blocks", c.blocks}, {"cold", c.cold},
        {"loads", c.loads},     {"stores", c.stores}, {"geps", c.geps},     {"cmps", c.cmps},
        {"branches", c.branches}, {"phis", c.phis},   {"calls", c.calls},   {"icalls", c.icalls},
        {"allocas", c.allocas}, {"casts", c.casts},   {"arith", c.arith},   {"other", c.other},
        {"callees", std::move(callees)}});
  }
  return a;
}

void writeIRStats(const std::string &unit, const IRCounts &pre, const IRCounts &post,
                  const PassClock &clock, double optMs, double codegenMs,
                  const std::vector<FnCounts> &preFns, const std::vector<FnCounts> &postFns) {
  const char *path = irStatsPath();
  if (!path)
    return;
  llvm::json::Object passes;
  for (const auto &e : clock.ms)
    passes[e.getKey()] = llvm::json::Array{e.getValue(), clock.runs.lookup(e.getKey()), clock.idle.lookup(e.getKey())};
  llvm::json::Object o{{"unit", unit},     {"pre", counts(pre)},       {"post", counts(post)},
                       {"opt_ms", optMs},  {"codegen_ms", codegenMs}, {"passes", std::move(passes)}};
  if (!preFns.empty() || !postFns.empty()) {
    o["fns_pre"] = fnsJSON(preFns);
    o["fns_post"] = fnsJSON(postFns);
  }
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
