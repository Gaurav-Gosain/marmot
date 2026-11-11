// WholeProgram.cpp: the -wpo batch mode.
//
// Phase 1 (parallel): each module is parsed, run through the go passes,
//   translated to LLVM IR, summarized (defined symbols and what each one
//   references) and kept as bitcode in memory.
// Phase 2: reachability over the whole program from the roots, internalize
//   decisions, and the cross-module import plan.
// Phase 3 (parallel): each module is loaded lazily (dead functions are never
//   parsed), dead definitions become declarations, symbols nobody else uses
//   become internal, imported functions are linked in as
//   available_externally, then the normal optimize and emit step runs.
// The rules are in docs/passes.md (section whole-program mode).
#include "marmot/Driver/WholeProgram.h"
#include "marmot/Driver/RuntimeBitcode.h"
#include "marmot/Driver/Watchdog.h"

#include "llvm/ADT/DenseSet.h"
#include "llvm/ADT/StringMap.h"
#include "llvm/ADT/StringSet.h"
#include "llvm/Bitcode/BitcodeReader.h"
#include "llvm/Bitcode/BitcodeWriter.h"
#include "llvm/IR/Constants.h"
#include "llvm/IR/LLVMContext.h"
#include "llvm/IR/Module.h"
#include "llvm/Linker/Linker.h"
#include "llvm/Support/FileSystem.h"
#include "llvm/Support/MemoryBuffer.h"
#include "llvm/Support/ThreadPool.h"
#include "llvm/Support/raw_ostream.h"
#include "llvm/Target/TargetMachine.h"

#include <atomic>
#include <chrono>
#include <map>
#include <mutex>
#include <set>

using namespace llvm;

namespace marmot {

namespace {

// Largest function, in LLVM instructions, imported into other modules.
constexpr unsigned kImportLimit = 40;

struct Def {
  std::string qname; // global name, or "<module>:<name>" for local symbols
  bool isFunc = false, local = false, importable = false;
  std::vector<std::string> refs; // qualified names
};

struct ModuleInfo {
  WpoJob job;
  SmallVector<char, 0> bitcode;
  std::vector<Def> defs;
  std::vector<std::string> extraRoots; // from appending globals (llvm.used, ctors)
  bool ok = false;
};

std::string qualify(const GlobalValue &gv, unsigned mod) {
  if (gv.hasLocalLinkage() || !gv.hasName())
    return std::to_string(mod) + ":" + gv.getName().str();
  return gv.getName().str();
}

void collectRefs(const Constant *c, SmallPtrSetImpl<const GlobalValue *> &out,
                 SmallPtrSetImpl<const Constant *> &seen) {
  SmallVector<const Constant *, 16> work{c};
  while (!work.empty()) {
    const Constant *k = work.pop_back_val();
    if (!seen.insert(k).second)
      continue;
    if (auto *gv = dyn_cast<GlobalValue>(k)) {
      out.insert(gv);
      continue;
    }
    for (const Use &op : k->operands())
      if (auto *cc = dyn_cast<Constant>(op.get()))
        work.push_back(cc);
  }
}

void refsOf(const GlobalValue &gv, SmallPtrSetImpl<const GlobalValue *> &out) {
  SmallPtrSet<const Constant *, 32> seen;
  if (auto *f = dyn_cast<Function>(&gv)) {
    for (const BasicBlock &bb : *f)
      for (const Instruction &inst : bb)
        for (const Use &op : inst.operands())
          if (auto *c = dyn_cast<Constant>(op.get()))
            collectRefs(c, out, seen);
    if (f->hasPersonalityFn())
      collectRefs(f->getPersonalityFn(), out, seen);
  } else if (auto *v = dyn_cast<GlobalVariable>(&gv)) {
    if (v->hasInitializer())
      collectRefs(v->getInitializer(), out, seen);
  } else if (auto *a = dyn_cast<GlobalAlias>(&gv)) {
    collectRefs(a->getAliasee(), out, seen);
  }
}

void summarize(Module &m, unsigned idx, ModuleInfo &info) {
  auto add = [&](GlobalValue &gv) {
    if (gv.isDeclaration())
      return;
    SmallPtrSet<const GlobalValue *, 16> refs;
    refsOf(gv, refs);
    if (gv.hasAppendingLinkage()) {
      for (const GlobalValue *r : refs)
        info.extraRoots.push_back(qualify(*r, idx));
      return;
    }
    Def d;
    d.qname = qualify(gv, idx);
    d.local = gv.hasLocalLinkage();
    d.isFunc = isa<Function>(gv);
    bool refsLocal = false;
    for (const GlobalValue *r : refs) {
      d.refs.push_back(qualify(*r, idx));
      refsLocal |= r->hasLocalLinkage();
    }
    if (auto *f = dyn_cast<Function>(&gv))
      d.importable = !d.local && !refsLocal && f->hasExternalLinkage() &&
                     !f->hasFnAttribute(Attribute::NoInline) &&
                     !f->hasFnAttribute(Attribute::OptimizeNone) &&
                     !f->getFunctionType()->isVarArg() &&
                     f->getInstructionCount() <= kImportLimit;
    info.defs.push_back(std::move(d));
  };
  for (Function &f : m)
    add(f);
  for (GlobalVariable &v : m.globals())
    add(v);
  for (GlobalAlias &a : m.aliases())
    add(a);
}

// A name with no '.' is a C-level symbol (main, marmot_main, functions the
// runtime calls by C name): always a root.
bool isCName(StringRef qname) {
  return !qname.contains('.') && !qname.contains(':') && !qname.starts_with("llvm.");
}

double msSince(std::chrono::steady_clock::time_point t) {
  return std::chrono::duration<double, std::milli>(std::chrono::steady_clock::now() - t)
      .count();
}

} // namespace

bool compileWholeProgram(const std::vector<WpoJob> &jobs, const CompileOptions &opts,
                         unsigned threads, PhaseTimes &total, WpoStats &stats,
                         raw_ostream &errs) {
  std::vector<ModuleInfo> mods(jobs.size());
  std::mutex mu;
  bool ok = true;
  std::vector<PhaseTimes> perJob(jobs.size());
  auto report = [&](const std::string &msgs, const PhaseTimes &t) {
    std::lock_guard<std::mutex> lock(mu);
    errs << msgs;
    total.add(t);
  };

  // Largest first, so a big module does not start last (both phases).
  std::vector<unsigned> order(jobs.size());
  for (unsigned i = 0; i < order.size(); ++i)
    order[i] = i;
  {
    std::vector<uint64_t> size(jobs.size(), 0);
    for (unsigned i = 0; i < jobs.size(); ++i)
      sys::fs::file_size(jobs[i].in, size[i]);
    std::stable_sort(order.begin(), order.end(),
                     [&](unsigned a, unsigned b) { return size[a] > size[b]; });
  }

  // Phase 1.
  {
    DefaultThreadPool pool(hardware_concurrency(threads));
    for (unsigned i : order)
      pool.async([&, i] {
        ModuleInfo &info = mods[i];
        info.job = jobs[i];
        watchdogBegin(jobs[i].in);
        std::string msgs;
        raw_string_ostream es(msgs);
        PhaseTimes t;
        std::string err;
        auto tm = createTargetMachine(opts, err);
        if (!tm) {
          es << jobs[i].in << ": " << err << "\n";
        } else {
          LLVMContext ctx;
          auto m = translateFile(jobs[i].in, opts, *tm, ctx, t, es);
          if (m) {
            auto t0 = std::chrono::steady_clock::now();
            summarize(*m, i, info);
            raw_svector_ostream os(info.bitcode);
            WriteBitcodeToFile(*m, os);
            info.ok = true;
            t.write += msSince(t0);
          }
        }
        watchdogEnd(jobs[i].in);
        perJob[i].add(t);
        report(msgs, t);
      });
    pool.wait();
  }
  for (ModuleInfo &m : mods)
    ok &= m.ok;
  if (!ok)
    return false;

  // Phase 2: definitions by name, roots, reachability.
  auto t2 = std::chrono::steady_clock::now();
  StringMap<std::vector<std::pair<unsigned, unsigned>>> defsByName;
  for (unsigned i = 0; i < mods.size(); ++i)
    for (unsigned j = 0; j < mods[i].defs.size(); ++j)
      defsByName[mods[i].defs[j].qname].push_back({i, j});

  StringSet<> roots;
  roots.insert("main");
  roots.insert("marmot_main");
  if (opts.rtBitcode)
    for (const auto &e : runtimeReferencedSymbols(*opts.rtBitcode))
      roots.insert(e.getKey());
  for (auto &e : defsByName)
    if (isCName(e.getKey()))
      roots.insert(e.getKey());
  for (ModuleInfo &m : mods)
    for (const std::string &r : m.extraRoots)
      roots.insert(r);

  StringSet<> live;
  SmallVector<StringRef, 256> work;
  auto mark = [&](StringRef n) {
    if (live.insert(n).second)
      work.push_back(live.find(n)->getKey());
  };
  for (const auto &r : roots)
    mark(r.getKey());
  while (!work.empty()) {
    StringRef n = work.pop_back_val();
    auto it = defsByName.find(n);
    if (it == defsByName.end())
      continue;
    for (auto [mi, di] : it->second)
      for (const std::string &r : mods[mi].defs[di].refs)
        mark(r);
  }

  // Imports: a live function in module T calling an importable function
  // defined only in another module S.
  std::vector<std::map<unsigned, std::set<std::string>>> imports(mods.size());
  // Modules whose (live or imported) code references a name.
  StringMap<std::set<unsigned>> usedBy;
  for (unsigned i = 0; i < mods.size(); ++i)
    for (const Def &d : mods[i].defs) {
      if (!live.contains(d.qname))
        continue;
      ++stats.liveFunctions;
      for (const std::string &r : d.refs) {
        usedBy[r].insert(i);
        if (!opts.wpoImport || opts.optLevel == '0' || !d.isFunc)
          continue;
        auto it = defsByName.find(r);
        if (it == defsByName.end() || it->second.size() != 1)
          continue;
        auto [si, sj] = it->second.front();
        const Def &callee = mods[si].defs[sj];
        if (si == i || !callee.isFunc || !callee.importable)
          continue;
        if (imports[i][si].insert(r).second) {
          ++stats.imported;
          for (const std::string &rr : callee.refs)
            usedBy[rr].insert(i);
        }
      }
    }
  for (ModuleInfo &m : mods)
    for (const Def &d : m.defs)
      stats.functions += d.isFunc;
  stats.liveFunctions = 0;
  for (ModuleInfo &m : mods)
    for (const Def &d : m.defs)
      stats.liveFunctions += d.isFunc && live.contains(d.qname);
  {
    std::lock_guard<std::mutex> lock(mu);
    total.link += msSince(t2);
  }

  // Phase 3, largest live code first.
  std::stable_sort(order.begin(), order.end(), [&](unsigned a, unsigned b) {
    return mods[a].bitcode.size() > mods[b].bitcode.size();
  });
  std::atomic<unsigned> internalized{0};
  {
    DefaultThreadPool pool(hardware_concurrency(threads));
    for (unsigned i : order)
      pool.async([&, i] {
        ModuleInfo &info = mods[i];
        watchdogBegin(info.job.in);
        std::string msgs;
        raw_string_ostream es(msgs);
        PhaseTimes t;
        auto t0 = std::chrono::steady_clock::now();
        auto fail = [&](const Twine &msg) {
          es << info.job.in << ": " << msg << "\n";
          std::lock_guard<std::mutex> lock(mu);
          ok = false;
        };
        std::string err;
        auto tm = createTargetMachine(opts, err);
        LLVMContext ctx;
        auto mOr = getLazyBitcodeModule(
            MemoryBufferRef(StringRef(info.bitcode.data(), info.bitcode.size()), info.job.in),
            ctx);
        if (!tm || !mOr) {
          fail(tm ? toString(mOr.takeError()) : err);
        } else {
          std::unique_ptr<Module> m = std::move(*mOr);
          // Dead definitions become declarations, never parsed.
          SmallVector<GlobalValue *, 64> deadLocal;
          for (Function &f : *m)
            if (!f.isDeclaration() && !live.contains(qualify(f, i))) {
              f.deleteBody();
              f.setComdat(nullptr);
              if (f.hasLocalLinkage())
                deadLocal.push_back(&f);
            }
          for (GlobalVariable &v : m->globals())
            if (!v.isDeclaration() && !v.hasAppendingLinkage() &&
                !live.contains(qualify(v, i))) {
              v.setInitializer(nullptr);
              v.setComdat(nullptr);
              if (v.hasLocalLinkage())
                deadLocal.push_back(&v);
              else
                v.setLinkage(GlobalValue::ExternalLinkage);
            }
          for (GlobalValue *gv : deadLocal) {
            if (gv->use_empty()) {
              gv->eraseFromParent();
            } else {
              gv->setLinkage(GlobalValue::ExternalLinkage);
              gv->setVisibility(GlobalValue::DefaultVisibility);
            }
          }
          // Internalize what only this module uses.
          for (GlobalValue &gv : m->global_values()) {
            if (gv.isDeclaration() || !gv.hasExternalLinkage() || !gv.hasName())
              continue;
            StringRef n = gv.getName();
            if (roots.contains(n))
              continue;
            auto u = usedBy.find(n);
            bool otherUser = u != usedBy.end() &&
                             llvm::any_of(u->second, [&](unsigned x) { return x != i; });
            if (otherUser || defsByName.lookup(n).size() != 1)
              continue;
            gv.setLinkage(GlobalValue::InternalLinkage);
            ++internalized;
          }
          if (Error e = m->materializeAll()) {
            fail(toString(std::move(e)));
          } else {
            // Imports.
            for (auto &[si, names] : imports[i]) {
              auto srcOr = getLazyBitcodeModule(
                  MemoryBufferRef(StringRef(mods[si].bitcode.data(), mods[si].bitcode.size()),
                                  mods[si].job.in),
                  ctx);
              if (!srcOr) {
                fail(toString(srcOr.takeError()));
                break;
              }
              std::unique_ptr<Module> src = std::move(*srcOr);
              // Keep linkonce/weak definitions: an imported body may use one
              // that only the source module defines, and the source module may
              // drop its copy. The linker copies only those that are needed.
              for (Function &f : *src)
                if (!f.isDeclaration() && !names.count(f.getName().str()) &&
                    !f.hasLinkOnceLinkage() && !f.hasWeakLinkage()) {
                  f.deleteBody();
                  f.setComdat(nullptr);
                  if (f.hasLocalLinkage())
                    f.setLinkage(GlobalValue::ExternalLinkage);
                }
              for (GlobalVariable &v : src->globals())
                if (!v.isDeclaration() && !v.hasLinkOnceLinkage() && !v.hasWeakLinkage()) {
                  v.setInitializer(nullptr);
                  v.setComdat(nullptr);
                  if (v.hasAppendingLinkage())
                    continue;
                  v.setLinkage(GlobalValue::ExternalLinkage);
                }
              // Drop appending globals so they are not merged twice.
              for (GlobalVariable &v : make_early_inc_range(src->globals()))
                if (v.hasAppendingLinkage())
                  v.eraseFromParent();
              if (Error e = src->materializeAll()) {
                fail(toString(std::move(e)));
                break;
              }
              if (Linker::linkModules(*m, std::move(src), Linker::Flags::LinkOnlyNeeded)) {
                fail("cross-module import failed");
                break;
              }
              for (const std::string &n : names)
                if (Function *f = m->getFunction(n); f && !f->isDeclaration())
                  f->setLinkage(GlobalValue::AvailableExternallyLinkage);
            }
            t.link += msSince(t0);
            if (msgs.empty() &&
                !optimizeAndEmit(*m, *tm, info.job.in, info.job.out, opts, t, es)) {
              std::lock_guard<std::mutex> lock(mu);
              ok = false;
            }
          }
        }
        watchdogEnd(info.job.in);
        perJob[i].add(t);
        report(msgs, t);
      });
    pool.wait();
  }
  stats.internalized = internalized;
  if (opts.timePhases)
    for (unsigned i = 0; i < jobs.size(); ++i)
      perJob[i].print(errs, jobs[i].in);
  return ok;
}

} // namespace marmot
