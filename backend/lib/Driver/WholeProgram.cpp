// WholeProgram.cpp: the batch driver (dev mode and the -wpo release mode).
//
// Phase 1 (parallel, cached): each module is parsed only if the cache cannot
//   answer for it. Escape summaries are propagated across modules in rounds.
//   Then each module is run through the go passes and translated to LLVM IR,
//   summarized (defined symbols and what each one references) and kept as
//   bitcode.
// Dev mode: each module is optimized and emitted on its own (cached).
// -wpo:
//   Phase 2: reachability over the whole program from the roots, internalize
//     decisions, and the cross-module import plan.
//   Phase 3 (parallel, cached): each module is loaded lazily (dead functions
//     are never parsed), dead definitions become declarations, symbols nobody
//     else uses become internal, imported functions are linked in as
//     available_externally, then the normal optimize and emit step runs.
// Rules and cache keys are in docs/passes.md (section "Batch driver").
#include "marmot/Driver/WholeProgram.h"
#include "marmot/Dialect/Go/Passes.h"
#include "marmot/Driver/RuntimeBitcode.h"
#include "marmot/Driver/Watchdog.h"

#include "mlir/Dialect/LLVMIR/LLVMDialect.h"
#include "mlir/IR/BuiltinOps.h"

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
#include "llvm/Support/Path.h"
#include "llvm/Support/ThreadPool.h"
#include "llvm/Support/raw_ostream.h"
#include "llvm/Support/xxhash.h"
#include "llvm/Target/TargetMachine.h"

#include <atomic>
#include <chrono>
#include <map>
#include <mutex>
#include <set>

using namespace llvm;

namespace marmot {

namespace {

// Bump when the meaning of a cache entry changes.
constexpr const char *kCacheVersion = "marmot-backend-cache-3";

// Largest function, in LLVM instructions, imported into other modules.
constexpr unsigned kImportLimit = 40;

// Local symbols are written "@:<name>" in summaries (so cache entries do not
// depend on job order) and become "<module index>:<name>" when loaded.
constexpr StringLiteral kLocal = "@:";

std::string hashHex(StringRef data) {
  XXH128_hash_t h = xxh3_128bits(arrayRefFromStringRef(data));
  std::string out;
  raw_string_ostream os(out);
  os << format_hex_no_prefix(h.high64, 16) << format_hex_no_prefix(h.low64, 16);
  return out;
}

class Cache {
public:
  explicit Cache(std::string dir) : dir(std::move(dir)) {
    if (!this->dir.empty())
      sys::fs::create_directories(this->dir);
  }
  bool enabled() const { return !dir.empty(); }

  std::optional<std::string> get(const std::string &key, StringRef kind) {
    if (!enabled())
      return std::nullopt;
    auto buf = MemoryBuffer::getFile(path(key, kind), /*IsText=*/false,
                                     /*RequiresNullTerminator=*/false);
    if (!buf)
      return std::nullopt;
    ++hits;
    return (*buf)->getBuffer().str();
  }

  void put(const std::string &key, StringRef kind, StringRef data) {
    if (!enabled())
      return;
    ++misses;
    std::string final = path(key, kind);
    SmallString<128> tmp;
    if (sys::fs::createUniqueFile(final + ".tmp%%%%%%", tmp))
      return;
    {
      std::error_code ec;
      raw_fd_ostream os(tmp, ec, sys::fs::OF_None);
      if (ec)
        return;
      os << data;
    }
    if (sys::fs::rename(tmp, final))
      sys::fs::remove(tmp);
  }

  std::atomic<unsigned> hits{0}, misses{0};

private:
  std::string path(const std::string &key, StringRef kind) const {
    SmallString<256> p(dir);
    sys::path::append(p, key.substr(0, 2), key + "." + kind.str());
    sys::fs::create_directories(sys::path::parent_path(p));
    return std::string(p);
  }
  std::string dir;
};

struct Def {
  std::string qname; // global name, or "<module>:<name>" for local symbols
  bool isFunc = false, local = false, importable = false;
  std::vector<std::string> refs; // qualified names
};

struct ModuleInfo {
  WpoJob job;
  std::string mlirHash;
  ParsedModulePtr parsed;
  std::vector<std::pair<std::string, unsigned>> decls; // external functions, arg count
  std::string declTable;                               // restricted escape table
  std::string keyB;                                    // translation cache key
  std::string bitcode;
  std::vector<Def> defs;
  std::vector<std::string> extraRoots; // from appending globals (llvm.used, ctors)
  bool ok = false;
};

std::string qualifyRaw(const GlobalValue &gv) {
  if (gv.hasLocalLinkage() || !gv.hasName())
    return (kLocal + gv.getName()).str();
  return gv.getName().str();
}

std::string localize(StringRef q, unsigned mod) {
  if (q.starts_with(kLocal))
    return std::to_string(mod) + ":" + q.drop_front(kLocal.size()).str();
  return q.str();
}

std::string qualify(const GlobalValue &gv, unsigned mod) {
  return localize(qualifyRaw(gv), mod);
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

// Reference summary in cache form: "D\t<flags>\t<qname>" then "R\t<ref>"
// lines; "X\t<root>" for appending-global roots. Names hold no tabs or
// newlines.
std::string summarize(Module &m) {
  std::string out;
  raw_string_ostream os(out);
  auto add = [&](GlobalValue &gv) {
    if (gv.isDeclaration())
      return;
    SmallPtrSet<const GlobalValue *, 16> refs;
    refsOf(gv, refs);
    if (gv.hasAppendingLinkage()) {
      for (const GlobalValue *r : refs)
        os << "X\t" << qualifyRaw(*r) << "\n";
      return;
    }
    bool refsLocal = false;
    for (const GlobalValue *r : refs)
      refsLocal |= r->hasLocalLinkage();
    bool importable = false;
    if (auto *f = dyn_cast<Function>(&gv))
      importable = !gv.hasLocalLinkage() && !refsLocal && f->hasExternalLinkage() &&
                   !f->hasFnAttribute(Attribute::NoInline) &&
                   !f->hasFnAttribute(Attribute::OptimizeNone) &&
                   !f->getFunctionType()->isVarArg() &&
                   f->getInstructionCount() <= kImportLimit;
    os << "D\t" << (isa<Function>(gv) ? 'f' : 'v') << (gv.hasLocalLinkage() ? 'l' : 'g')
       << (importable ? 'i' : '-') << "\t" << qualifyRaw(gv) << "\n";
    for (const GlobalValue *r : refs)
      os << "R\t" << qualifyRaw(*r) << "\n";
  };
  for (Function &f : m)
    add(f);
  for (GlobalVariable &v : m.globals())
    add(v);
  for (GlobalAlias &a : m.aliases())
    add(a);
  return out;
}

void loadSummary(StringRef text, unsigned idx, ModuleInfo &info) {
  SmallVector<StringRef, 0> lines;
  text.split(lines, '\n', -1, false);
  for (StringRef l : lines) {
    auto [tag, rest] = l.split('\t');
    if (tag == "D") {
      auto [flags, name] = rest.split('\t');
      Def d;
      d.isFunc = flags[0] == 'f';
      d.local = flags[1] == 'l';
      d.importable = flags[2] == 'i';
      d.qname = localize(name, idx);
      info.defs.push_back(std::move(d));
    } else if (tag == "R" && !info.defs.empty()) {
      info.defs.back().refs.push_back(localize(rest, idx));
    } else if (tag == "X") {
      info.extraRoots.push_back(localize(rest, idx));
    }
  }
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

// Everything besides the module's own text that changes what a cache entry
// holds.
std::string fingerprint(const CompileOptions &opts) {
  std::string fp;
  raw_string_ostream os(fp);
  os << kCacheVersion << "|O" << opts.optLevel << "|" << int(opts.emit) << "|"
     << opts.triple << "|" << opts.cpu << "|" << opts.features << "|"
     << opts.strings << opts.devirt << opts.escape << opts.bce << opts.openDefers
     << opts.initOptNone << opts.wpoImport << "|loops" << opts.loops.enable
     << opts.loops.parallel << opts.loops.fastFP << opts.loops.remarks << "|";
  if (opts.rtBitcode)
    os << hashHex(opts.rtBitcode->bitcode->getBuffer());
  return fp;
}

using EscapeTable = StringMap<std::vector<bool>>;

// The part of the table about the functions module m declares, as text.
std::string restrictTable(const EscapeTable &table, const ModuleInfo &m) {
  std::string out;
  for (auto &[name, nargs] : m.decls) {
    auto it = table.find(name);
    if (it == table.end() || it->second.size() != nargs)
      continue;
    std::string bits;
    for (bool b : it->second)
      bits += b ? '1' : '0';
    if (bits.find('1') == std::string::npos)
      continue;
    out += name + "\t" + bits + "\n";
  }
  return out;
}

// Puts go.noescape on m's declarations as the restricted table says.
void applyTable(mlir::ModuleOp m, StringRef table) {
  StringMap<std::string> t;
  SmallVector<StringRef, 0> lines;
  table.split(lines, '\n', -1, false);
  for (StringRef l : lines) {
    auto [n, bits] = l.split('\t');
    t[n] = bits.str();
  }
  mlir::UnitAttr unit = mlir::UnitAttr::get(m.getContext());
  m.walk([&](mlir::LLVM::LLVMFuncOp f) {
    if (!f.isExternal())
      return;
    auto it = t.find(f.getSymName());
    if (it == t.end() || it->second.size() != f.getNumArguments())
      return;
    for (unsigned a = 0; a < f.getNumArguments(); ++a)
      if (it->second[a] == '1')
        f.setArgAttr(a, "go.noescape", unit);
  });
}

// The summaries of the non-local functions m defines, as "name\tbits" lines.
std::string escapeSummaries(mlir::ModuleOp m) {
  go::summarizeEscapes(m);
  std::string out;
  m.walk([&](mlir::LLVM::LLVMFuncOp f) {
    if (f.isExternal() || f.getLinkage() == mlir::LLVM::Linkage::Internal ||
        f.getLinkage() == mlir::LLVM::Linkage::Private)
      return;
    out += f.getSymName().str() + "\t";
    for (unsigned a = 0; a < f.getNumArguments(); ++a)
      out += f.getArgAttr(a, "go.noescape") ? '1' : '0';
    out += "\n";
  });
  return out;
}

bool writeFile(const std::string &path, StringRef data) {
  std::error_code ec;
  raw_fd_ostream os(path, ec, sys::fs::OF_None);
  if (ec)
    return false;
  os << data;
  return !os.has_error();
}

} // namespace

bool compileWholeProgram(const std::vector<WpoJob> &jobs, const CompileOptions &opts,
                         unsigned threads, bool wpo, const std::string &cacheDir,
                         PhaseTimes &total, WpoStats &stats, raw_ostream &errs) {
  std::vector<ModuleInfo> mods(jobs.size());
  std::vector<PhaseTimes> perJob(jobs.size());
  Cache cache(cacheDir);
  // Objects are cached only for -emit=obj.
  bool cacheObjects = cache.enabled() && opts.emit == Emit::Obj;
  const std::string fp = fingerprint(opts);
  std::mutex mu;
  bool ok = true;
  auto report = [&](unsigned i, const std::string &msgs, const PhaseTimes &t) {
    std::lock_guard<std::mutex> lock(mu);
    errs << msgs;
    if (!msgs.empty())
      ok = false;
    total.add(t);
    perJob[i].add(t);
  };

  // Largest first, so a big module does not start last.
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
  auto parallel = [&](auto fn) {
    DefaultThreadPool pool(hardware_concurrency(threads));
    for (unsigned i : order)
      pool.async([&, i] {
        watchdogBegin(jobs[i].in);
        std::string msgs;
        raw_string_ostream es(msgs);
        PhaseTimes t;
        fn(i, es, t);
        watchdogEnd(jobs[i].in);
        report(i, msgs, t);
      });
    pool.wait();
  };
  // Parses module i if not parsed yet.
  auto ensureParsed = [&](unsigned i, raw_ostream &es, PhaseTimes &t) -> bool {
    if (mods[i].parsed)
      return true;
    auto t0 = std::chrono::steady_clock::now();
    mods[i].parsed = parseFile(jobs[i].in, opts, es);
    t.parse += msSince(t0);
    return bool(mods[i].parsed);
  };
  auto moduleOp = [&](unsigned i) {
    return mlir::ModuleOp::getFromOpaquePointer(parsedModuleOp(*mods[i].parsed));
  };

  // Phase 1a: hash inputs; declared functions (cached by input hash).
  parallel([&](unsigned i, raw_ostream &es, PhaseTimes &t) {
    ModuleInfo &m = mods[i];
    m.job = jobs[i];
    auto t0 = std::chrono::steady_clock::now();
    auto buf = MemoryBuffer::getFile(jobs[i].in);
    if (!buf) {
      es << jobs[i].in << ": " << buf.getError().message() << "\n";
      return;
    }
    m.mlirHash = hashHex((*buf)->getBuffer());
    t.parse += msSince(t0);
    std::optional<std::string> decls = cache.get(m.mlirHash, "decls");
    if (!decls) {
      if (!ensureParsed(i, es, t))
        return;
      std::string text;
      moduleOp(i).walk([&](mlir::LLVM::LLVMFuncOp f) {
        if (f.isExternal())
          text += f.getSymName().str() + "\t" + std::to_string(f.getNumArguments()) + "\n";
      });
      cache.put(m.mlirHash, "decls", text);
      decls = text;
    }
    SmallVector<StringRef, 0> lines;
    StringRef(*decls).split(lines, '\n', -1, false);
    for (StringRef l : lines) {
      auto [n, c] = l.split('\t');
      unsigned nargs = 0;
      c.getAsInteger(10, nargs);
      m.decls.push_back({n.str(), nargs});
    }
  });
  if (!ok)
    return false;

  // Phase 1b: escape summaries across modules. Each round recomputes every
  // module's summaries given the table from the previous round, starting
  // from "everything escapes"; every round is sound and the facts only grow.
  // Go's import graph has no cycles, so this ends after about as many rounds
  // as the import depth. A round of a module is cached by its input hash and
  // the part of the table it declares.
  EscapeTable table;
  if (opts.escape && opts.optLevel != '0') {
    auto t0 = std::chrono::steady_clock::now();
    for (unsigned round = 0; round < 16; ++round) {
      std::vector<std::string> found(mods.size());
      parallel([&](unsigned i, raw_ostream &es, PhaseTimes &t) {
        std::string in = restrictTable(table, mods[i]);
        std::string key = hashHex(mods[i].mlirHash + "|esc|" + in);
        if (auto hit = cache.get(key, "esc")) {
          found[i] = *hit;
          return;
        }
        if (!ensureParsed(i, es, t))
          return;
        applyTable(moduleOp(i), in);
        found[i] = escapeSummaries(moduleOp(i));
        cache.put(key, "esc", found[i]);
      });
      if (!ok)
        return false;
      // A name defined in several modules (linkonce) gets the intersection.
      EscapeTable next;
      for (const std::string &text : found) {
        SmallVector<StringRef, 0> lines;
        StringRef(text).split(lines, '\n', -1, false);
        for (StringRef l : lines) {
          auto [n, bits] = l.split('\t');
          std::vector<bool> v;
          for (char c : bits)
            v.push_back(c == '1');
          auto [it, fresh] = next.try_emplace(n, v);
          if (!fresh)
            for (size_t a = 0; a < it->second.size() && a < v.size(); ++a)
              it->second[a] = it->second[a] && v[a];
        }
      }
      bool same = next.size() == table.size();
      for (auto &e : next) {
        if (!same)
          break;
        auto it = table.find(e.getKey());
        same = it != table.end() && it->second == e.getValue();
      }
      table = std::move(next);
      stats.escapeRounds = round + 1;
      if (same)
        break;
    }
    for (auto &e : table)
      for (bool b : e.getValue())
        stats.noEscapeParams += b;
    total.link += msSince(t0);
  }

  // Phase 1c: go passes, lowering, translation, summary, bitcode (cached).
  parallel([&](unsigned i, raw_ostream &es, PhaseTimes &t) {
    ModuleInfo &m = mods[i];
    m.declTable = restrictTable(table, m);
    m.keyB = hashHex(m.mlirHash + "|bc|" + m.declTable + "|" + fp);
    auto bc = cache.get(m.keyB, "bc");
    auto sum = bc ? cache.get(m.keyB, "sum") : std::nullopt;
    if (bc && sum) {
      m.bitcode = std::move(*bc);
      loadSummary(*sum, i, m);
      m.parsed = nullptr;
      m.ok = true;
      return;
    }
    if (!ensureParsed(i, es, t))
      return;
    applyTable(moduleOp(i), m.declTable);
    std::string err;
    auto tm = createTargetMachine(opts, err);
    if (!tm) {
      es << jobs[i].in << ": " << err << "\n";
      return;
    }
    LLVMContext ctx;
    auto lm = lowerAndTranslate(*m.parsed, jobs[i].in, opts, *tm, ctx, t, es);
    m.parsed = nullptr;
    if (!lm)
      return;
    auto t0 = std::chrono::steady_clock::now();
    std::string summary = summarize(*lm);
    loadSummary(summary, i, m);
    {
      raw_string_ostream os(m.bitcode);
      WriteBitcodeToFile(*lm, os);
    }
    cache.put(m.keyB, "bc", m.bitcode);
    cache.put(m.keyB, "sum", summary);
    m.ok = true;
    t.write += msSince(t0);
  });
  if (!ok)
    return false;

  // Dev mode: every module on its own.
  if (!wpo) {
    parallel([&](unsigned i, raw_ostream &es, PhaseTimes &t) {
      ModuleInfo &m = mods[i];
      std::string key = hashHex(m.keyB + "|obj");
      if (cacheObjects) {
        if (auto obj = cache.get(key, "o")) {
          if (!writeFile(m.job.out, *obj))
            es << m.job.out << ": cannot write\n";
          return;
        }
      }
      std::string err;
      auto tm = createTargetMachine(opts, err);
      LLVMContext ctx;
      auto mOr = parseBitcodeFile(MemoryBufferRef(m.bitcode, m.job.in), ctx);
      if (!tm || !mOr) {
        es << m.job.in << ": " << (tm ? toString(mOr.takeError()) : err) << "\n";
        return;
      }
      if (!optimizeAndEmit(**mOr, *tm, m.job.in, m.job.out, opts, t, es))
        return;
      if (cacheObjects)
        if (auto obj = MemoryBuffer::getFile(m.job.out, false, false))
          cache.put(key, "o", (*obj)->getBuffer());
    });
    stats.cacheHits = cache.hits;
    stats.cacheMisses = cache.misses;
    if (opts.timePhases)
      for (unsigned i = 0; i < jobs.size(); ++i)
        perJob[i].print(errs, jobs[i].in);
    return ok;
  }

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
    for (const Def &d : m.defs) {
      stats.functions += d.isFunc;
      stats.liveFunctions += d.isFunc && live.contains(d.qname);
    }
  // Functions each module internalizes: live, external, defined only there,
  // used by no other module, not roots.
  std::vector<std::vector<std::string>> internal(mods.size());
  for (unsigned i = 0; i < mods.size(); ++i) {
    for (const Def &d : mods[i].defs) {
      if (!d.isFunc || d.local || !live.contains(d.qname) || roots.contains(d.qname))
        continue;
      if (defsByName.lookup(d.qname).size() != 1)
        continue;
      auto u = usedBy.find(d.qname);
      if (u != usedBy.end() && llvm::any_of(u->second, [&](unsigned x) { return x != i; }))
        continue;
      internal[i].push_back(d.qname);
    }
    llvm::sort(internal[i]);
  }
  total.link += msSince(t2);

  // Phase 3, largest live code first.
  std::stable_sort(order.begin(), order.end(), [&](unsigned a, unsigned b) {
    return mods[a].bitcode.size() > mods[b].bitcode.size();
  });
  std::atomic<unsigned> internalized{0};
  parallel([&](unsigned i, raw_ostream &es, PhaseTimes &t) {
    ModuleInfo &info = mods[i];
    auto t0 = std::chrono::steady_clock::now();
    // The key: this module's bitcode, which of its definitions are live, what
    // it internalizes, and the bitcode of every module it imports from.
    std::string key;
    if (cacheObjects) {
      std::string k = info.keyB + "|wpo|";
      std::vector<std::string> liveDefs;
      for (const Def &d : info.defs)
        if (live.contains(d.qname))
          liveDefs.push_back(d.local ? "@:" + d.qname.substr(d.qname.find(':') + 1)
                                     : d.qname);
      llvm::sort(liveDefs);
      for (const std::string &n : liveDefs)
        k += n + "\n";
      k += "|int|";
      for (const std::string &n : internal[i])
        k += n + "\n";
      k += "|imp|";
      for (auto &[si, names] : imports[i]) {
        k += mods[si].keyB + ":";
        for (const std::string &n : names)
          k += n + ",";
        k += "\n";
      }
      key = hashHex(k);
      if (auto obj = cache.get(key, "o")) {
        if (!writeFile(info.job.out, *obj))
          es << info.job.out << ": cannot write\n";
        internalized += internal[i].size();
        t.link += msSince(t0);
        return;
      }
    }
    std::string err;
    auto tm = createTargetMachine(opts, err);
    LLVMContext ctx;
    auto mOr = getLazyBitcodeModule(MemoryBufferRef(info.bitcode, info.job.in), ctx);
    if (!tm || !mOr) {
      es << info.job.in << ": " << (tm ? toString(mOr.takeError()) : err) << "\n";
      return;
    }
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
      if (!v.isDeclaration() && !v.hasAppendingLinkage() && !live.contains(qualify(v, i))) {
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
    // Internalize functions only this module uses. Never variables: a Go
    // package variable is a GC root even if nothing reads it (its object's
    // finalizer must not run), and LLVM deletes internal globals that are
    // only stored to.
    for (const std::string &n : internal[i])
      if (Function *f = m->getFunction(n); f && !f->isDeclaration()) {
        f->setLinkage(GlobalValue::InternalLinkage);
        ++internalized;
      }
    if (Error e = m->materializeAll()) {
      es << info.job.in << ": " << toString(std::move(e)) << "\n";
      return;
    }
    // Imports.
    for (auto &[si, names] : imports[i]) {
      auto srcOr = getLazyBitcodeModule(MemoryBufferRef(mods[si].bitcode, mods[si].job.in), ctx);
      if (!srcOr) {
        es << info.job.in << ": " << toString(srcOr.takeError()) << "\n";
        return;
      }
      std::unique_ptr<Module> src = std::move(*srcOr);
      // Keep linkonce/weak definitions: an imported body may use one that only
      // the source module defines, and the source module may drop its copy.
      // The linker copies only those that are needed.
      for (Function &f : *src)
        if (!f.isDeclaration() && !names.count(f.getName().str()) &&
            !f.hasLinkOnceLinkage() && !f.hasWeakLinkage()) {
          f.deleteBody();
          f.setComdat(nullptr);
          if (f.hasLocalLinkage())
            f.setLinkage(GlobalValue::ExternalLinkage);
        }
      for (GlobalVariable &v : make_early_inc_range(src->globals())) {
        if (v.hasAppendingLinkage()) {
          v.eraseFromParent();
          continue;
        }
        if (!v.isDeclaration() && !v.hasLinkOnceLinkage() && !v.hasWeakLinkage()) {
          v.setInitializer(nullptr);
          v.setComdat(nullptr);
          v.setLinkage(GlobalValue::ExternalLinkage);
        }
      }
      if (Error e = src->materializeAll()) {
        es << info.job.in << ": " << toString(std::move(e)) << "\n";
        return;
      }
      if (Linker::linkModules(*m, std::move(src), Linker::Flags::LinkOnlyNeeded)) {
        es << info.job.in << ": cross-module import failed\n";
        return;
      }
      for (const std::string &n : names)
        if (Function *f = m->getFunction(n); f && !f->isDeclaration())
          f->setLinkage(GlobalValue::AvailableExternallyLinkage);
    }
    t.link += msSince(t0);
    if (!optimizeAndEmit(*m, *tm, info.job.in, info.job.out, opts, t, es))
      return;
    if (cacheObjects)
      if (auto obj = MemoryBuffer::getFile(info.job.out, false, false))
        cache.put(key, "o", (*obj)->getBuffer());
  });
  stats.internalized = internalized;
  stats.cacheHits = cache.hits;
  stats.cacheMisses = cache.misses;
  if (opts.timePhases)
    for (unsigned i = 0; i < jobs.size(); ++i)
      perJob[i].print(errs, jobs[i].in);
  return ok;
}

} // namespace marmot
