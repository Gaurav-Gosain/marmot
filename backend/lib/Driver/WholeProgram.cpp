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
#include "marmot/Driver/WholeProgram.h"
#include "marmot/Dialect/Go/Passes.h"
#include "marmot/Driver/Pcln.h"
#include "marmot/Loops/Loops.h"
#include "marmot/Driver/RuntimeBitcode.h"
#include "marmot/Driver/Watchdog.h"
#include "tier0/Tier0.h"

#include "mlir/Dialect/LLVMIR/LLVMDialect.h"
#include "mlir/IR/BuiltinOps.h"

#include "llvm/ADT/BitVector.h"
#include "llvm/ADT/DenseSet.h"
#include "llvm/ADT/StringMap.h"
#include "llvm/ADT/StringSet.h"
#include "llvm/Bitcode/BitcodeReader.h"
#include "llvm/Bitcode/BitcodeWriter.h"
#include "llvm/IR/Constants.h"
#include "llvm/IR/LLVMContext.h"
#include "llvm/IR/Module.h"
#include "llvm/IR/ModuleSlotTracker.h"
#include "llvm/Linker/Linker.h"
#include "llvm/Support/FileSystem.h"
#include "llvm/Support/MemoryBuffer.h"
#include "llvm/TargetParser/Host.h"
#include "llvm/Support/Path.h"
#include "llvm/Support/Process.h"
#include "llvm/Support/ThreadPool.h"
#include "llvm/Support/raw_ostream.h"
#include "llvm/Support/xxhash.h"
#include "llvm/Target/TargetMachine.h"
#include "llvm/Transforms/Utils/CallPromotionUtils.h"
#include "llvm/Transforms/Utils/Cloning.h"
#include "llvm/IR/IRBuilder.h"
#include "llvm/IR/MDBuilder.h"

#include <atomic>
#include <chrono>
#include <functional>
#include <condition_variable>
#include <cstdlib>
#include <map>
#include <mutex>
#include <set>

using namespace llvm;

namespace marmot {

namespace {

// Bump when the meaning of a cache entry changes.
constexpr const char *kCacheVersion = "marmot-backend-cache-24";

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

// Background tier 1 spool (see runTier1): at most this many modules wait.
constexpr int kTier1SpoolMax = 64;

// The keys of the modules in a tier 1 spool, newest first.
std::vector<std::string> tier1Spooled(StringRef dir) {
  std::vector<std::pair<sys::TimePoint<>, std::string>> found;
  std::error_code ec;
  for (sys::fs::directory_iterator it(dir, ec), end; !ec && it != end; it.increment(ec)) {
    StringRef name = sys::path::filename(it->path());
    if (!name.consume_back(".mlir"))
      continue;
    sys::fs::file_status st;
    if (sys::fs::status(it->path(), st))
      continue;
    found.push_back({st.getLastModificationTime(), name.str()});
  }
  std::sort(found.begin(), found.end(), [](auto &a, auto &b) { return a.first > b.first; });
  std::vector<std::string> keys;
  for (auto &f : found)
    keys.push_back(f.second);
  return keys;
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
    std::string p = path(key, kind);
    auto buf = MemoryBuffer::getFile(p, /*IsText=*/false, /*RequiresNullTerminator=*/false);
    if (!buf)
      return std::nullopt;
    ++hits;
    // Recently used entries survive eviction.
    int fd;
    if (!sys::fs::openFileForReadWrite(p, fd, sys::fs::CD_OpenExisting, sys::fs::OF_None)) {
      sys::fs::setLastAccessAndModificationTime(fd, std::chrono::system_clock::now());
      sys::Process::SafelyCloseFileDescriptor(fd);
    }
    return (*buf)->getBuffer().str();
  }

  // Keeps the cache under MARMOT_CACHE_MAX_MB (default 4096): a running
  // total in <dir>/.size; past the limit, a scan deletes the least recently
  // used entries down to 75% of it.
  void evict() {
    if (!enabled())
      return;
    uint64_t maxMB = 4096;
    if (const char *e = getenv("MARMOT_CACHE_MAX_MB"))
      if (uint64_t v = strtoull(e, nullptr, 10))
        maxMB = v;
    uint64_t limit = maxMB << 20;
    SmallString<256> sizeFile(dir);
    sys::path::append(sizeFile, ".size");
    uint64_t total = 0;
    if (auto buf = MemoryBuffer::getFile(sizeFile))
      (*buf)->getBuffer().trim().getAsInteger(10, total);
    total += written;
    if (total > limit) {
      struct Entry {
        std::string path;
        uint64_t size;
        sys::TimePoint<> used;
      };
      std::vector<Entry> entries;
      total = 0;
      std::error_code ec;
      for (sys::fs::recursive_directory_iterator it(dir, ec), end; it != end && !ec;
           it.increment(ec)) {
        sys::fs::file_status st;
        if (sys::fs::status(it->path(), st) || st.type() != sys::fs::file_type::regular_file ||
            sys::path::filename(it->path()) == ".size")
          continue;
        entries.push_back({it->path(), st.getSize(), st.getLastModificationTime()});
        total += st.getSize();
      }
      llvm::sort(entries, [](const Entry &a, const Entry &b) { return a.used < b.used; });
      for (const Entry &e : entries) {
        if (total <= limit * 3 / 4)
          break;
        if (!sys::fs::remove(e.path))
          total -= e.size;
      }
    }
    std::error_code ec;
    raw_fd_ostream os(sizeFile, ec);
    if (!ec)
      os << total << "\n";
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
    else
      written += data.size();
  }

  // Whether an entry exists (no hit is counted, no time touched).
  bool has(const std::string &key, StringRef kind) const {
    return enabled() && sys::fs::exists(path(key, kind));
  }

  std::atomic<unsigned> hits{0}, misses{0};
  std::atomic<uint64_t> written{0};

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
  bool copyable = false; // linkonce or weak: any module may hold a copy
  std::string hash;      // importable functions: hash of the body
  std::vector<std::string> refs; // qualified names
  // Type descriptors this definition uses for more than allocation (see
  // descriptorEscapes); their reflection tables must stay.
  std::vector<std::string> typeUses;
};

struct ModuleInfo {
  WpoJob job;
  std::string mlirHash;
  ParsedModulePtr parsed;
  std::vector<std::pair<std::string, unsigned>> decls; // external functions, arg count
  std::string declTable;                               // restricted escape table
  std::string pkg; // Go package (module attribute marmot.package); dev imports stay inside it
  std::string keyB;                                    // translation cache key
  std::string bitcode;
  std::vector<Def> defs;
  std::vector<std::string> extraRoots; // from appending globals (llvm.used, ctors)
  // Functions whose address the module takes, with their LLVM type.
  std::vector<std::pair<std::string, std::string>> addrTaken;
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

std::string typeString(Type *t) {
  std::string out;
  raw_string_ostream os(out);
  t->print(os);
  return out;
}

// A type descriptor symbol "type:T" (not one of its sub-tables).
bool isDescriptor(StringRef n) {
  if (!n.starts_with("type:"))
    return false;
  for (StringRef suf : {".fields", ".methods", ".uncommon", ".gcdata", ".imethods", ".params"})
    if (n.ends_with(suf))
      return false;
  return true;
}

bool isAllocCallee(StringRef n) {
  return n == "runtime.newobject" || n == "runtime.mallocgc" || n == "runtime.makeslice" ||
         n == "runtime.makeslice64" || n == "runtime.growslice" || n == "runtime.makechan" ||
         n == "runtime.makechan64" || n == "runtime.newarray";
}

// Descriptors gv uses for more than allocation: anything other than an
// argument of a runtime allocation call (functions), or anything other than
// the type's own pointer type and its own sub-tables (global initializers).
// Those uses can reach getitab, typehash or typeequal (lib/src/runtime), which
// read the fields and methods tables.
void descriptorEscapes(const GlobalValue &gv, std::set<std::string> &out) {
  SmallPtrSet<const GlobalValue *, 16> refs;
  if (auto *f = dyn_cast<Function>(&gv)) {
    for (const BasicBlock &bb : *f)
      for (const Instruction &inst : bb) {
        auto *cb = dyn_cast<CallBase>(&inst);
        bool alloc = cb && cb->getCalledFunction() &&
                     isAllocCallee(cb->getCalledFunction()->getName());
        if (alloc)
          continue;
        SmallPtrSet<const Constant *, 16> seen;
        for (const Use &op : inst.operands())
          if (auto *c = dyn_cast<Constant>(op.get()))
            collectRefs(c, refs, seen);
      }
  } else {
    refsOf(gv, refs);
  }
  StringRef self = gv.getName();
  for (const GlobalValue *r : refs) {
    StringRef n = r->getName();
    if (!isDescriptor(n))
      continue;
    StringRef t = n.drop_front(5); // "type:"
    // The type's own sub-tables and its pointer type refer to it.
    if (self.starts_with((n + ".").str()) || self == ("type:*" + t).str() ||
        self.starts_with(("type:*" + t + ".").str()))
      continue;
    out.insert(n.str());
  }
}

// Reference summary in cache form: "D\t<flags>\t<qname>" then "R\t<ref>"
// lines; "X\t<root>" for appending-global roots. Names hold no tabs or
// newlines.
// Hash of a function body that does not depend on the rest of the module:
// the instruction text without metadata numbers, the debug line and column
// of each instruction, the shallow text of other attached metadata, and the
// attributes. mst: one slot tracker for the module (printing without one
// renumbers the whole module per instruction).
std::string bodyHash(Function &f, ModuleSlotTracker &mst) {
  std::string text, line;
  raw_string_ostream os(text);
  f.getAttributes().print(os);
  os << "\n" << typeString(f.getFunctionType()) << "\n";
  mst.incorporateFunction(f);
  SmallVector<std::pair<unsigned, MDNode *>, 4> mds;
  auto strip = [&](StringRef l) { // "!123" -> "!"
    for (size_t c = 0; c < l.size(); ++c) {
      os << l[c];
      if (l[c] == '!')
        while (c + 1 < l.size() && isdigit((unsigned char)l[c + 1]))
          ++c;
    }
  };
  for (BasicBlock &bb : f)
    for (Instruction &inst : bb) {
      line.clear();
      raw_string_ostream ls(line);
      inst.print(ls, mst);
      strip(line);
      if (const DebugLoc &dl = inst.getDebugLoc())
        os << " @" << dl.getLine() << ":" << dl.getCol();
      inst.getAllMetadataOtherThanDebugLoc(mds);
      for (auto &[kind, md] : mds) {
        line.clear();
        md->print(ls, mst);
        os << " " << kind << "=";
        strip(line);
      }
      os << "\n";
    }
  return hashHex(text);
}

std::string summarize(Module &m, StringRef pkg) {
  std::string out;
  raw_string_ostream os(out);
  ModuleSlotTracker mst(&m, /*ShouldInitializeAllMetadata=*/false);
  if (!pkg.empty())
    os << "P\t" << pkg << "\n";
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
       << (importable ? 'i' : '-')
       << (gv.hasLinkOnceLinkage() || gv.hasWeakLinkage() ? 'o' : '-') << "\t"
       << qualifyRaw(gv) << "\n";
    if (importable)
      os << "H\t" << bodyHash(cast<Function>(gv), mst) << "\n";
    for (const GlobalValue *r : refs)
      os << "R\t" << qualifyRaw(*r) << "\n";
    std::set<std::string> uses;
    descriptorEscapes(gv, uses);
    for (const std::string &u : uses)
      os << "E\t" << u << "\n";
  };
  for (Function &f : m)
    add(f);
  for (GlobalVariable &v : m.globals())
    add(v);
  for (GlobalAlias &a : m.aliases())
    add(a);
  // Address-taken functions: any use other than as the callee of a call.
  for (Function &f : m) {
    if (f.isIntrinsic())
      continue;
    bool taken = llvm::any_of(f.uses(), [](const Use &u) {
      auto *cb = dyn_cast<CallBase>(u.getUser());
      return !cb || !cb->isCallee(&u);
    });
    if (taken)
      os << "A\t" << qualifyRaw(f) << "\t" << typeString(f.getFunctionType()) << "\n";
  }
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
      d.copyable = flags.size() > 3 && flags[3] == 'o';
      d.qname = localize(name, idx);
      info.defs.push_back(std::move(d));
    } else if (tag == "P") {
      info.pkg = rest.str();
    } else if (tag == "H" && !info.defs.empty()) {
      info.defs.back().hash = rest.str();
    } else if (tag == "R" && !info.defs.empty()) {
      info.defs.back().refs.push_back(localize(rest, idx));
    } else if (tag == "E" && !info.defs.empty()) {
      info.defs.back().typeUses.push_back(rest.str());
    } else if (tag == "X") {
      info.extraRoots.push_back(localize(rest, idx));
    } else if (tag == "A") {
      auto [name, type] = rest.split('\t');
      info.addrTaken.push_back({localize(name, idx), type.str()});
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
     << "|snc" << opts.stringsNoCopy << "|mf" << opts.mergeFunctions << "|kcs" << opts.keepCallSites << "|" << opts.triple << "|" << opts.cpu
     << (opts.cpu == "native" ? "=" + sys::getHostCPUName().str() : std::string()) << "|"
     << opts.features << "|"
     << opts.strings << opts.devirt << opts.boolPack << opts.escape << opts.bce << opts.openDefers
     << opts.initOptNone << opts.wpoImport << opts.wpoClosedWorld << opts.wpoPruneTypes
     << "|loops" << opts.loops.enable
     << opts.loops.parallel << opts.loops.fastFP << opts.loops.remarks
     << opts.loops.versioning << opts.loops.typeAlias << opts.loops.gpu << "|";
  if (opts.rtBitcode)
    os << opts.rtBitcode->fileHash();
  os << "|opts:" << opts.optionKey << "|loopopts:" << loops::cacheKey();
  os << "|g" << opts.debugLevel << "|ktc" << opts.keepTailCalls << "|gpu=" << opts.gpuArch << "|pgo-gen=" << opts.pgoGen << "|pgo=";
  if (!opts.pgoUse.empty()) {
    if (auto buf = MemoryBuffer::getFile(opts.pgoUse))
      os << hashHex((*buf)->getBuffer());
    else
      os << "missing:" << opts.pgoUse;
  }
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

// Links the bodies of the named functions from other modules into m as
// available_externally definitions (inlining only; the source module keeps
// the real definition). imp: source module index -> function names.
// The bitcode imports read from a module: its bitcode without what no
// import uses (importBodies drops the same from it before linking): bodies of
// functions that are not importable and not linkonce or weak, initializers of
// globals that are not linkonce or weak, and appending globals. Every import
// read the whole module again, and its constant data took most of that.
// Declarations stay, even unused ones: the linker merges their visibility
// and unnamed_addr into the importer's definitions. "" if the bitcode does
// not load.
std::string importSource(StringRef bitcode, StringRef name, const StringSet<> &importable) {
  LLVMContext ctx;
  auto mOr = getLazyBitcodeModule(MemoryBufferRef(bitcode, name), ctx);
  if (!mOr) {
    consumeError(mOr.takeError());
    return "";
  }
  Module &src = **mOr;
  for (Function &f : src)
    if (!f.isDeclaration() && !importable.contains(f.getName()) && !f.hasLinkOnceLinkage() &&
        !f.hasWeakLinkage()) {
      f.deleteBody();
      f.setComdat(nullptr);
      if (f.hasLocalLinkage())
        f.setLinkage(GlobalValue::ExternalLinkage);
    }
  for (GlobalVariable &v : make_early_inc_range(src.globals())) {
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
  if (Error e = src.materializeAll()) {
    consumeError(std::move(e));
    return "";
  }
  std::string out;
  raw_string_ostream os(out);
  WriteBitcodeToFile(src, os);
  return out;
}

// source(si) is the bitcode of module si to read (importSource).
bool importBodies(Module &m, const std::map<unsigned, std::set<std::string>> &imp,
                  std::vector<ModuleInfo> &mods, LLVMContext &ctx, raw_ostream &es,
                  StringRef in, function_ref<StringRef(unsigned)> source) {
  // One linker for all sources: each new one scanned all of m's types.
  Linker linker(m);
  for (auto &[si, names] : imp) {
    auto srcOr = getLazyBitcodeModule(MemoryBufferRef(source(si), mods[si].job.in), ctx);
    if (!srcOr) {
      es << in << ": " << toString(srcOr.takeError()) << "\n";
      return false;
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
    // The linker reads the bodies it links (the imported functions and the
    // linkonce/weak definitions they use) from the lazy module; reading all
    // of them first took most of the import time.
    if (linker.linkInModule(std::move(src), Linker::Flags::LinkOnlyNeeded)) {
      es << in << ": cross-module import failed\n";
      return false;
    }
    for (const std::string &n : names)
      if (Function *f = m.getFunction(n); f && !f->isDeclaration())
        f->setLinkage(GlobalValue::AvailableExternallyLinkage);
  }
  return true;
}

// Large modules (dev batch, -split-large N): a module above N instructions
// is split into K function groups that are optimized and emitted in
// parallel and cached one by one. Local symbols become hidden external
// symbols with a suffix that is stable for the module (so other groups can
// reference them and two modules never clash); every definition goes to the
// group given by a hash of its name, so an edit changes few groups; small
// callees from other groups are copied in available_externally, so inlining
// across groups stays. linkonce/weak definitions go to every group that may
// use them (the linker keeps one). Appending globals (constructors, used)
// stay in group 0. Returns the groups as bitcode. A group's cache key is its
// bitcode: exact, and stable across edits elsewhere in the module because
// unused declarations are dropped and no location refers to the input .mlir
// (parseFile), so metadata numbering inside an unchanged group stays the same.
std::vector<std::string> splitModule(Module &m, unsigned k, StringRef fallback) {
  // Stable suffix: the smallest strong external definition of the module
  // (a symbol only this module defines).
  std::string anchor;
  for (GlobalValue &gv : m.global_values())
    if (!gv.isDeclaration() && gv.hasExternalLinkage() && gv.hasName() &&
        (anchor.empty() || gv.getName() < anchor))
      anchor = gv.getName().str();
  if (anchor.empty())
    anchor = fallback.str();
  std::string suffix = ".g" + hashHex(anchor).substr(0, 8);
  // Local constants (string literals, boxed values) are numbered in emission
  // order (.str.302, .box.507): one more or less renumbers the rest and
  // changes every group that uses them. Name them by their contents instead.
  // unnamed_addr constants with equal contents become one; others keep their
  // identity and get the ordinal among equal contents. Data arrays first,
  // then constants that refer to other globals (printed after those have
  // their final names).
  {
    std::map<std::string, GlobalVariable *> merged;
    std::map<std::string, unsigned> seen;
    auto canon = [&](GlobalVariable &v, const std::string &content) {
      std::string key = typeString(v.getValueType()) + "|" +
                        std::to_string(v.getAlign() ? v.getAlign()->value() : 0) + "|" + content;
      if (v.hasGlobalUnnamedAddr()) {
        auto [it, fresh] = merged.try_emplace(key, &v);
        if (!fresh) {
          v.replaceAllUsesWith(it->second);
          return false; // erase v
        }
        v.setName(".k" + hashHex(key).substr(0, 16));
      } else {
        unsigned n = seen[key]++;
        v.setName(".k" + hashHex(key).substr(0, 16) + "." + std::to_string(n));
      }
      return true;
    };
    auto eligible = [](GlobalVariable &v) {
      return v.hasLocalLinkage() && v.isConstant() && v.hasInitializer() && !v.hasSection() &&
             !v.isThreadLocal();
    };
    SmallVector<GlobalVariable *, 0> dead;
    for (GlobalVariable &v : m.globals())
      if (eligible(v))
        if (auto *cds = dyn_cast<ConstantDataSequential>(v.getInitializer()))
          if (!canon(v, "d" + cds->getRawDataValues().str()))
            dead.push_back(&v);
    for (GlobalVariable *v : dead)
      v->eraseFromParent();
    dead.clear();
    SmallVector<GlobalVariable *, 0> rest;
    for (GlobalVariable &v : m.globals())
      if (eligible(v) && !isa<ConstantDataSequential>(v.getInitializer()))
        rest.push_back(&v);
    for (GlobalVariable *v : rest) {
      std::string text;
      raw_string_ostream os(text);
      os << "c";
      v->getInitializer()->print(os);
      if (!canon(*v, text))
        dead.push_back(v);
    }
    for (GlobalVariable *v : dead)
      v->eraseFromParent();
  }
  for (GlobalValue &gv : m.global_values()) {
    if (gv.isDeclaration() || !gv.hasLocalLinkage())
      continue;
    if (auto *v = dyn_cast<GlobalVariable>(&gv); v && v->hasAppendingLinkage())
      continue;
    gv.setName((gv.hasName() ? gv.getName().str() : std::string("anon")) + suffix);
    gv.setLinkage(GlobalValue::ExternalLinkage);
    gv.setVisibility(GlobalValue::HiddenVisibility);
    gv.setDSOLocal(true);
  }
  auto groupOf = [&](const GlobalValue &gv) {
    return unsigned(xxh3_64bits(gv.getName()) % k);
  };
  auto copyable = [](const GlobalValue &gv) {
    return gv.hasLinkOnceLinkage() || gv.hasWeakLinkage() ||
           gv.hasAvailableExternallyLinkage();
  };
  auto small = [](const Function &f) {
    return !f.isDeclaration() && f.getInstructionCount() <= kImportLimit &&
           !f.hasFnAttribute(Attribute::NoInline) &&
           !f.hasFnAttribute(Attribute::OptimizeNone) && !f.getFunctionType()->isVarArg();
  };
  // Small callees of each group's functions, two levels deep.
  std::vector<DenseSet<const GlobalValue *>> imports(k);
  for (Function &f : m) {
    if (f.isDeclaration() || copyable(f))
      continue;
    unsigned g = groupOf(f);
    SmallVector<const Function *, 16> work{&f};
    for (unsigned depth = 0; depth < 2 && !work.empty(); ++depth) {
      SmallVector<const Function *, 16> next;
      for (const Function *w : work)
        for (const BasicBlock &bb : *w)
          for (const Instruction &inst : bb)
            if (auto *cb = dyn_cast<CallBase>(&inst))
              if (auto *c = cb->getCalledFunction();
                  c && small(*c) && !copyable(*c) && groupOf(*c) != g &&
                  imports[g].insert(c).second)
                next.push_back(c);
      work = std::move(next);
    }
  }
  std::vector<std::string> out(k);
  for (unsigned g = 0; g < k; ++g) {
    ValueToValueMapTy vmap;
    std::unique_ptr<Module> part = CloneModule(m, vmap, [&](const GlobalValue *gv) {
      return copyable(*gv) || groupOf(*gv) == g || imports[g].contains(gv);
    });
    for (const GlobalValue *gv : imports[g])
      if (auto *f = dyn_cast<Function>(vmap[gv]); f && !f->isDeclaration()) {
        f->setLinkage(GlobalValue::AvailableExternallyLinkage);
        f->setVisibility(GlobalValue::DefaultVisibility);
      }
    if (g != 0)
      for (GlobalVariable &v : make_early_inc_range(part->globals()))
        if (v.hasAppendingLinkage())
          v.eraseFromParent();
    // The key of a group is its bitcode: no temporary paths in it.
    // Declarations nothing in the group uses would make every group depend
    // on every function of the module (a new function changes all keys).
    for (Function &f : make_early_inc_range(*part))
      if (f.isDeclaration() && f.use_empty() && !f.isIntrinsic())
        f.eraseFromParent();
    for (GlobalVariable &v : make_early_inc_range(part->globals()))
      if (v.isDeclaration() && v.use_empty())
        v.eraseFromParent();
    part->setModuleIdentifier("");
    part->setSourceFileName("");
    raw_string_ostream os(out[g]);
    WriteBitcodeToFile(*part, os);
  }
  return out;
}

// -tier0 (dev): each module on its own, in one pass, started as its job
// arrives. tier0 compiles what it supports into OUT; LLVM -O0 compiles the
// functions it leaves into OUT.1.o (both listed in OUT.parts). No escape
// rounds, summaries, bitcode or splitting: at -O0 none of them changes the
// code.
bool compileTier0(const std::function<bool(WpoJob &)> &next, const CompileOptions &opts,
                  unsigned threads, const std::string &cacheDir, PhaseTimes &total,
                  WpoStats &stats, raw_ostream &errs) {
  Cache cache(cacheDir);
  struct EvictAtEnd {
    Cache &c;
    ~EvictAtEnd() { c.evict(); }
  } evictAtEnd{cache};
  bool cacheObjects = cache.enabled() && opts.emit == Emit::Obj;
  const std::string fp = fingerprint(opts);
  std::mutex mu;
  bool ok = true;
  // The pc table fragment of an object made by LLVM: OUT.pcln, cached with
  // the object. A failure only costs the table lines of the object.
  auto fragment = [&](const std::string &out, StringRef obj, const std::string &key) {
    if (!opts.pclnFragments || opts.prepareOnly || opts.emit != Emit::Obj)
      return;
    std::optional<std::string> f = cacheObjects ? cache.get(key, "pcln") : std::nullopt;
    if (!f) {
      std::string text, err;
      if (!pcln::makeFragment(MemoryBufferRef(obj, out), 3, text, err)) {
        std::lock_guard<std::mutex> lock(mu);
        errs << "marmot-backend: warning: " << out << ": no pc table fragment: " << err << "\n";
        return;
      }
      if (cacheObjects)
        cache.put(key, "pcln", text);
      f = std::move(text);
    }
    if (!writeFile(out + ".pcln", *f)) {
      std::lock_guard<std::mutex> lock(mu);
      errs << "marmot-backend: warning: " << out << ".pcln: cannot write\n";
    }
  };
  // Memory: a parsed and lowered module peaks at 8 to 14 times its text
  // (measured), plus what the allocator keeps across threads, and largest
  // first starts the biggest modules together (11 at once passed 3 GB on
  // cmd/marmot). A job parses only when the text of the jobs in flight plus
  // its own fits in a thirty-second of the memory limit; one job always
  // runs.
  uint64_t textBudget = 3072ull << 20;
  if (const char *e = std::getenv("MARMOT_MAX_MEM_MB"))
    textBudget = std::strtoull(e, nullptr, 10) << 20;
  textBudget = textBudget ? textBudget / 32 : UINT64_MAX;
  std::mutex admitMu;
  std::condition_variable admitCv;
  uint64_t inFlight = 0;
  struct Admit {
    std::mutex &mu;
    std::condition_variable &cv;
    uint64_t &inFlight;
    uint64_t n;
    Admit(std::mutex &mu, std::condition_variable &cv, uint64_t &inFlight, uint64_t n,
          uint64_t budget)
        : mu(mu), cv(cv), inFlight(inFlight), n(n) {
      std::unique_lock<std::mutex> l(mu);
      cv.wait(l, [&] { return inFlight == 0 || inFlight + n <= budget; });
      inFlight += n;
    }
    ~Admit() {
      {
        std::lock_guard<std::mutex> l(mu);
        inFlight -= n;
      }
      cv.notify_all();
    }
  };
  // A large module parses with the threads of the budget (opts.parseThreads
  // or the pool) that no other job parsing uses: a deep edit recompiles a
  // few modules, one of them large. At most 4 (parseFile).
  // MARMOT_PARSE_THREADS caps them; 1 parses every module serially.
  unsigned parseCap = 4;
  if (const char *e = std::getenv("MARMOT_PARSE_THREADS"))
    parseCap = std::max(1, std::atoi(e));
  std::atomic<unsigned> busy{0};
  // Background tier 1: every module compiled here is linked (copied if
  // that fails) into the spool as KEY.mlir with KEY.fp, the options it was
  // compiled with; at most kTier1SpoolMax entries wait.
  const bool tier1 = cacheObjects && !opts.tier1Spool.empty();
  int spoolRoom = -1; // entries the spool takes; counted on first use
  const std::string fpHash = hashHex(fp);
  auto spoolTier1 = [&](const std::string &in, const std::string &t1Key) {
    {
      std::lock_guard<std::mutex> lock(mu);
      if (spoolRoom < 0) {
        sys::fs::create_directories(opts.tier1Spool);
        spoolRoom = kTier1SpoolMax - int(tier1Spooled(opts.tier1Spool).size());
      }
      if (spoolRoom <= 0)
        return;
      --spoolRoom;
    }
    SmallString<256> base(opts.tier1Spool);
    sys::path::append(base, t1Key);
    std::string mlir = (base + ".mlir").str(), tmp = (base + ".tmp").str();
    if (sys::fs::exists(mlir) || !writeFile((base + ".fp").str(), fpHash))
      return;
    sys::fs::remove(tmp);
    if (sys::fs::create_hard_link(in, tmp) && sys::fs::copy_file(in, tmp))
      return;
    sys::fs::rename(tmp, mlir);
  };
  auto compileJob = [&](const WpoJob &j, raw_ostream &es, PhaseTimes &t) {
    auto t0 = std::chrono::steady_clock::now();
    std::string inHash;
    if (!j.key.empty()) {
      inHash = hashHex("key|" + j.key);
    } else {
      auto buf = MemoryBuffer::getFile(j.in);
      if (!buf) {
        es << j.in << ": " << buf.getError().message() << "\n";
        return;
      }
      inHash = hashHex((*buf)->getBuffer());
    }
    t.parse += msSince(t0);
    const std::string key = hashHex(inHash + "|t0|" + fp);
    const std::string restKey = hashHex(key + "|rest");
    const std::string rest = j.out + ".1.o";
    // Background tier 1 (opts.tier1Spool): the -O2 object of the same
    // input, made after the build by runTier1, replaces the tier0 ones.
    const std::string t1Key = tier1 ? hashHex(inHash + "|t1|" + fp) : std::string();
    auto writeOut = [&](const std::string &path, StringRef obj) {
      if (!opts.prepareOnly && !writeFile(path, obj))
        es << path << ": cannot write\n";
    };
    auto writeParts = [&](bool two) {
      if (opts.prepareOnly)
        return;
      if (!two) {
        sys::fs::remove(j.out + ".parts");
        return;
      }
      if (!writeFile(j.out + ".parts", j.out + "\n" + rest + "\n"))
        es << j.out << ".parts: cannot write\n";
    };
    if (tier1) {
      if (auto obj = cache.get(t1Key, "o")) {
        writeOut(j.out, *obj);
        fragment(j.out, *obj, t1Key);
        writeParts(false);
        ++stats.tier1Hits;
        return;
      }
    }
    if (cacheObjects) {
      auto obj = cache.get(key, "o");
      auto parts = obj ? cache.get(key, "t0parts") : std::nullopt;
      auto obj2 = parts && *parts == "2" ? cache.get(restKey, "o") : std::nullopt;
      if (obj && parts && (*parts == "1" || obj2)) {
        writeOut(j.out, *obj);
        fragment(j.out, *obj, key);
        if (obj2) {
          writeOut(rest, *obj2);
          fragment(rest, *obj2, restKey);
        }
        writeParts(bool(obj2));
        return;
      }
    }
    if (tier1 && !opts.prepareOnly)
      spoolTier1(j.in, t1Key);
    uint64_t textSize = 0;
    sys::fs::file_size(j.in, textSize);
    Admit admit(admitMu, admitCv, inFlight, textSize, textBudget);
    ParsedModulePtr pm;
    {
      unsigned others = busy++;
      auto tp = std::chrono::steady_clock::now();
      CompileOptions po = opts;
      po.parseThreads = std::clamp(int(std::max(threads, opts.parseThreads)) - int(others), 1,
                                   int(parseCap));
      pm = parseFile(j.in, po, es);
      busy--;
      t.parse += msSince(tp);
    }
    if (!pm)
      return;
    std::string err;
    auto tm = createTargetMachine(opts, err);
    if (!tm) {
      es << j.in << ": " << err << "\n";
      return;
    }
    if (!lowerModule(*pm, j.in, opts, *tm, t, es))
      return;
    mlir::ModuleOp module = mlir::ModuleOp::getFromOpaquePointer(parsedModuleOp(*pm));
    const std::string out0 = opts.prepareOnly ? j.out + ".prepare.tmp" : j.out;
    std::string frag;
    std::vector<std::string> fallback, reasons;
    tier0::Stats st;
    // tier0's targets: arm64 Mach-O and x86-64 ELF. Any other target gets
    // the whole module from LLVM -O0 in OUT.
    const Triple &tt = tm->getTargetTriple();
    std::optional<tier0::Target> target;
    if (tt.isAArch64() && tt.isOSBinFormatMachO())
      target = tier0::Target::Arm64MachO;
    else if (tt.getArch() == Triple::x86_64 && tt.isOSBinFormatELF())
      target = tier0::Target::X64Elf;
    if (!target) {
      LLVMContext ctx;
      auto lm = translateLowered(*pm, j.in, *tm, ctx, t, es);
      if (!lm || !optimizeAndEmit(*lm, *tm, j.in, out0, opts, t, es))
        return;
      pm = nullptr;
      auto o0 = MemoryBuffer::getFile(out0);
      if (!o0) {
        es << j.out << ": cannot read the object back\n";
        return;
      }
      ++stats.compiled;
      fragment(j.out, (*o0)->getBuffer(), key);
      if (cacheObjects) {
        cache.put(key, "o", (*o0)->getBuffer());
        cache.put(key, "t0parts", "1");
      }
      if (opts.prepareOnly)
        sys::fs::remove(out0);
      writeParts(false);
      return;
    }
    auto tc = std::chrono::steady_clock::now();
    // The functions on the threads other jobs leave (all of them in a leaf
    // edit, where the main module is the only job: 46 ms serial on
    // cmd/marmot's).
    unsigned others = busy++;
    unsigned cgThreads = unsigned(std::max(1, int(threads) - int(others)));
    bool emitted =
        tier0::emit(module, key.substr(0, 16), out0, frag, fallback, reasons, st, err, *target, cgThreads);
    busy--;
    if (!emitted) {
      es << j.in << ": tier0: " << err << "\n";
      return;
    }
    t.codegen += msSince(tc);
    // LLVM gets the module when tier0 left a function or anything that is
    // not a function or global (constructors, aliases).
    bool needRest = !fallback.empty();
    for (mlir::Operation &op : module.getBody()->getOperations())
      if (!isa<mlir::LLVM::LLVMFuncOp, mlir::LLVM::GlobalOp>(op))
        needRest = true;
    {
      std::lock_guard<std::mutex> lock(mu);
      stats.tier0Functions += st.functions;
      stats.tier0Fallbacks += st.fallbacks;
      stats.tier0Rest += needRest;
      for (const std::string &r : reasons)
        ++stats.tier0Reasons[r];
    }
    ++stats.compiled;
    const std::string out1 = opts.prepareOnly ? rest + ".prepare.tmp" : rest;
    if (needRest) {
      // Functions tier0 compiled are declarations now; a declaration
      // must not carry its subprogram (LLVM's verifier rejects it).
      module.walk([](mlir::LLVM::LLVMFuncOp f) {
        if (!f.isExternal())
          return;
        if (auto fl = dyn_cast<mlir::FusedLoc>(f.getLoc()))
          if (fl.getMetadata() && isa<mlir::LLVM::DISubprogramAttr>(fl.getMetadata()))
            f->setLoc(mlir::FusedLoc::get(fl.getLocations(), mlir::Attribute(), f->getContext()));
      });
      LLVMContext ctx;
      auto lm = translateLowered(*pm, j.in, *tm, ctx, t, es);
      if (!lm || !optimizeAndEmit(*lm, *tm, j.in, out1, opts, t, es))
        return;
    }
    pm = nullptr;
    auto o0 = MemoryBuffer::getFile(out0);
    auto o1 = needRest ? MemoryBuffer::getFile(out1) : nullptr;
    if (!o0 || (needRest && !o1)) {
      es << j.out << ": cannot read the object back\n";
      return;
    }
    if (!frag.empty() && !opts.prepareOnly && opts.pclnFragments) {
      if (!writeFile(j.out + ".pcln", frag))
        es << j.out << ".pcln: cannot write\n";
      if (cacheObjects)
        cache.put(key, "pcln", frag);
    } else {
      fragment(j.out, (*o0)->getBuffer(), key);
    }
    if (needRest)
      fragment(rest, (*o1)->getBuffer(), restKey);
    if (cacheObjects) {
      cache.put(key, "o", (*o0)->getBuffer());
      if (needRest)
        cache.put(restKey, "o", (*o1)->getBuffer());
      cache.put(key, "t0parts", needRest ? "2" : "1");
    }
    if (opts.prepareOnly) {
      sys::fs::remove(out0);
      if (needRest)
        sys::fs::remove(out1);
    }
    writeParts(needRest);
  };
  DefaultThreadPool pool(hardware_concurrency(threads));
  std::vector<std::pair<std::string, PhaseTimes>> perJob;
  WpoJob job;
  while (next(job))
    pool.async([&, j = job] {
      watchdogBegin(j.in);
      std::string msgs;
      raw_string_ostream es(msgs);
      PhaseTimes t;
      compileJob(j, es, t);
      watchdogEnd(j.in);
      std::lock_guard<std::mutex> lock(mu);
      errs << msgs;
      if (!msgs.empty())
        ok = false;
      total.add(t);
      perJob.push_back({j.in, t});
    });
  pool.wait();
  stats.cacheHits = cache.hits;
  stats.cacheMisses = cache.misses;
  if (opts.timePhases)
    for (auto &[in, t] : perJob)
      t.print(errs, sys::path::filename(in).str());
  return ok;
}

bool runTier1(const CompileOptions &opts, const std::string &cacheDir, raw_ostream &errs) {
  if (opts.tier1Spool.empty() || cacheDir.empty() || opts.emit != Emit::Obj)
    return true;
  sys::fs::create_directories(opts.tier1Spool);
  // One runner at a time; a second one leaves the spool to the first.
  SmallString<256> lockPath(opts.tier1Spool);
  sys::path::append(lockPath, ".lock");
  int lockFd;
  if (sys::fs::openFileForWrite(lockPath, lockFd, sys::fs::CD_OpenAlways))
    return true;
  struct CloseFd {
    int fd;
    ~CloseFd() { sys::Process::SafelyCloseFileDescriptor(fd); }
  } closeLock{lockFd};
  if (sys::fs::tryLockFile(lockFd))
    return true;
  Cache cache(cacheDir);
  struct EvictAtEnd {
    Cache &c;
    ~EvictAtEnd() { c.evict(); }
  } evictAtEnd{cache};
  const std::string fpHash = hashHex(fingerprint(opts));
  // The lowering is the tier0 batch's (same MLIR passes, so the object
  // links with tier0 objects as its LLVM fallback does); LLVM optimizes and
  // emits at -O2.
  CompileOptions o2 = opts;
  o2.tier0 = false;
  o2.optLevel = '2';
  o2.parseThreads = 1;
  // A module whose text passes a thirty-second of the memory limit waits
  // for a build to compile it (same rule as the tier0 batch).
  uint64_t textBudget = 3072ull << 20;
  if (const char *e = std::getenv("MARMOT_MAX_MEM_MB"))
    textBudget = std::strtoull(e, nullptr, 10) << 20;
  textBudget = textBudget ? textBudget / 32 : UINT64_MAX;
  bool ok = true;
  // Newest first: the module edited last is the one the next build needs.
  // Entries that arrive while this runs are taken in the next round.
  for (;;) {
    std::vector<std::string> keys = tier1Spooled(opts.tier1Spool);
    if (keys.empty())
      break;
    for (const std::string &k : keys) {
      SmallString<256> base(opts.tier1Spool);
      sys::path::append(base, k);
      const std::string in = (base + ".mlir").str(), fpFile = (base + ".fp").str();
      auto drop = [&] {
        sys::fs::remove(in);
        sys::fs::remove(fpFile);
      };
      auto fpBuf = MemoryBuffer::getFile(fpFile);
      uint64_t size = 0;
      sys::fs::file_size(in, size);
      if (!fpBuf || (*fpBuf)->getBuffer() != fpHash || size > textBudget ||
          cache.has(k, "o")) {
        drop();
        continue;
      }
      std::string msgs;
      raw_string_ostream es(msgs);
      PhaseTimes t;
      std::string err, obj;
      auto tm = createTargetMachine(o2, err);
      ParsedModulePtr pm = tm ? parseFile(in, opts, es) : nullptr;
      bool good = pm && lowerModule(*pm, in, opts, *tm, t, es);
      if (good) {
        LLVMContext ctx;
        auto lm = translateLowered(*pm, in, *tm, ctx, t, es);
        pm = nullptr;
        const std::string out = (base + ".o").str();
        good = lm && optimizeAndEmit(*lm, *tm, in, out, o2, t, es);
        if (good) {
          auto buf = MemoryBuffer::getFile(out);
          good = bool(buf);
          if (good)
            obj = (*buf)->getBuffer().str();
        }
        sys::fs::remove(out);
      }
      std::string text;
      if (good && opts.pclnFragments &&
          !pcln::makeFragment(MemoryBufferRef(obj, in), 3, text, err))
        good = false;
      if (good) {
        // The fragment first: a build that finds the object finds its table.
        if (opts.pclnFragments)
          cache.put(k, "pcln", text);
        cache.put(k, "o", obj);
      } else {
        ok = false;
        errs << "marmot-backend: tier1: " << in << ": " << (tm ? msgs : err) << "\n";
      }
      drop();
    }
  }
  return ok;
}

bool compileWholeProgram(const std::vector<WpoJob> &jobs, const CompileOptions &opts,
                         unsigned threads, bool wpo, const std::string &cacheDir,
                         PhaseTimes &total, WpoStats &stats, raw_ostream &errs) {
  std::vector<ModuleInfo> mods(jobs.size());
  std::vector<PhaseTimes> perJob(jobs.size());
  Cache cache(cacheDir);
  struct EvictAtEnd {
    Cache &c;
    ~EvictAtEnd() { c.evict(); }
  } evictAtEnd{cache};
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

  // The pc table fragment of an object: OUT.pcln,
  // cached with the object. A failure only costs the table lines of the
  // object, so it is a warning.
  auto fragment = [&](const std::string &out, StringRef obj, const std::string &key) {
    if (!opts.pclnFragments || opts.prepareOnly || opts.emit != Emit::Obj)
      return;
    std::optional<std::string> f =
        cacheObjects && !key.empty() ? cache.get(key, "pcln") : std::nullopt;
    if (!f) {
      std::string text, err;
      if (!pcln::makeFragment(MemoryBufferRef(obj, out), 3, text, err)) {
        std::lock_guard<std::mutex> lock(mu);
        errs << "marmot-backend: warning: " << out << ": no pc table fragment: " << err << "\n";
        return;
      }
      if (cacheObjects && !key.empty())
        cache.put(key, "pcln", text);
      f = std::move(text);
    }
    if (!writeFile(out + ".pcln", *f)) {
      std::lock_guard<std::mutex> lock(mu);
      errs << "marmot-backend: warning: " << out << ".pcln: cannot write\n";
    }
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

  if (opts.tier0 && !wpo) {
    size_t k = 0;
    return compileTier0(
        [&](WpoJob &j) {
          if (k == order.size())
            return false;
          j = jobs[order[k++]];
          return true;
        },
        opts, threads, cacheDir, total, stats, errs);
  }

  // Phase 1a: hash inputs; declared functions (cached by input hash).
  parallel([&](unsigned i, raw_ostream &es, PhaseTimes &t) {
    ModuleInfo &m = mods[i];
    m.job = jobs[i];
    auto t0 = std::chrono::steady_clock::now();
    if (!jobs[i].key.empty()) {
      // The caller's key stands for the contents: no read of a 20 MB text
      // whose every result is cached.
      m.mlirHash = hashHex("key|" + jobs[i].key);
    } else {
      auto buf = MemoryBuffer::getFile(jobs[i].in);
      if (!buf) {
        es << jobs[i].in << ": " << buf.getError().message() << "\n";
        return;
      }
      m.mlirHash = hashHex((*buf)->getBuffer());
    }
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
    std::string pkg;
    if (auto a = moduleOp(i)->getAttrOfType<mlir::StringAttr>("marmot.package"))
      pkg = a.getValue().str();
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
    std::string summary = summarize(*lm, pkg);
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

  // -prepare-only with -wpo: the program dependent part is not cacheable
  // ahead of time; phase 1 (escape rounds, bitcode, summaries) is done.
  if (wpo && opts.prepareOnly) {
    stats.cacheHits = cache.hits;
    stats.cacheMisses = cache.misses;
    return ok;
  }

  // The import sources (importSource), made on first use and cached.
  std::vector<std::string> importSrc(mods.size());
  std::vector<std::once_flag> importSrcOnce(mods.size());
  auto source = [&](unsigned si) -> StringRef {
    std::call_once(importSrcOnce[si], [&] {
      ModuleInfo &m = mods[si];
      std::string key = hashHex(m.keyB + "|import-source");
      if (auto c = cache.get(key, "bc")) {
        importSrc[si] = std::move(*c);
        return;
      }
      StringSet<> importable;
      for (const Def &d : m.defs)
        if (d.importable)
          importable.insert(d.qname);
      importSrc[si] = importSource(m.bitcode, m.job.in, importable);
      if (!importSrc[si].empty())
        cache.put(key, "bc", importSrc[si]);
    });
    return importSrc[si].empty() ? StringRef(mods[si].bitcode) : StringRef(importSrc[si]);
  };

  // Dev mode: every module on its own, plus available_externally copies of
  // small functions from other modules of the same Go package (the frontend
  // splits a package into one module per source file). The object's cache key
  // includes the bodies of what it imports, so an edit elsewhere in the
  // package recompiles a module only if a body it inlines changed.
  if (!wpo) {
    std::vector<std::map<unsigned, std::set<std::string>>> imports(mods.size());
    std::vector<std::string> importKey(mods.size());
    if (opts.wpoImport && opts.optLevel != '0') {
      StringMap<std::pair<unsigned, unsigned>> byName;
      StringSet<> dup;
      for (unsigned mi = 0; mi < mods.size(); ++mi)
        for (unsigned di = 0; di < mods[mi].defs.size(); ++di) {
          const Def &d = mods[mi].defs[di];
          if (d.isFunc && !d.local && !byName.try_emplace(d.qname, mi, di).second)
            dup.insert(d.qname);
        }
      constexpr unsigned kDevImportBudget = 200;
      for (unsigned i = 0; i < mods.size(); ++i) {
        if (mods[i].pkg.empty())
          continue;
        SmallVector<const std::string *, 64> work;
        for (const Def &d : mods[i].defs)
          if (d.isFunc)
            for (const std::string &r : d.refs)
              work.push_back(&r);
        unsigned count = 0;
        std::map<std::string, std::string> taken; // name -> body hash
        while (!work.empty() && count < kDevImportBudget) {
          const std::string &r = *work.pop_back_val();
          auto it = byName.find(r);
          if (it == byName.end() || dup.contains(r))
            continue;
          auto [si, sj] = it->second;
          const Def &callee = mods[si].defs[sj];
          if (si == i || !callee.importable || mods[si].pkg != mods[i].pkg)
            continue;
          if (!imports[i][si].insert(r).second)
            continue;
          ++count;
          ++stats.imported;
          taken[r] = callee.hash;
          for (const std::string &rr : callee.refs)
            work.push_back(&rr);
        }
        for (auto &[n, h] : taken)
          importKey[i] += n + "=" + h + "\n";
      }
    }
    // Groups of split modules still to compile: (job, group, bitcode, key).
    struct Group {
      unsigned job, index;
      std::string bitcode, key;
    };
    std::vector<Group> groups;
    auto partPath = [&](unsigned i, unsigned g) {
      return g == 0 ? jobs[i].out : jobs[i].out + "." + std::to_string(g) + ".o";
    };
    // OUT.parts lists the objects of a split job.
    auto writeParts = [&](unsigned i, unsigned k, raw_ostream &es) {
      std::string list;
      for (unsigned g = 0; g < k; ++g)
        list += partPath(i, g) + "\n";
      if (!opts.prepareOnly && !writeFile(jobs[i].out + ".parts", list))
        es << jobs[i].out << ".parts: cannot write\n";
    };
    parallel([&](unsigned i, raw_ostream &es, PhaseTimes &t) {
      ModuleInfo &m = mods[i];
      // Output "-": the module is part of the program (its summaries and
      // bodies serve the others) but its own output is not wanted, as for
      // every module but main's with marmot build -emit llvm.
      if (m.job.out == "-")
        return;
      std::string key = hashHex(m.keyB + "|obj|" + importKey[i]);
      if (!opts.prepareOnly)
        sys::fs::remove(m.job.out + ".parts");
      if (cacheObjects) {
        if (auto obj = cache.get(key, "o")) {
          if (!opts.prepareOnly && !writeFile(m.job.out, *obj))
            es << m.job.out << ": cannot write\n";
          fragment(m.job.out, *obj, key);
          return;
        }
        // A split module: the list of its group keys (only when splitting).
        if (auto list = opts.splitInstrs ? cache.get(key, "parts") : std::nullopt) {
          SmallVector<StringRef, 16> keys;
          StringRef(*list).split(keys, '\n', -1, false);
          std::vector<std::string> objs;
          for (StringRef gk : keys)
            if (auto obj = cache.get(gk.str(), "o"))
              objs.push_back(std::move(*obj));
          if (objs.size() == keys.size()) {
            for (unsigned g = 0; g < objs.size() && !opts.prepareOnly; ++g) {
              if (!writeFile(partPath(i, g), objs[g]))
                es << partPath(i, g) << ": cannot write\n";
              fragment(partPath(i, g), objs[g], keys[g].str());
            }
            writeParts(i, objs.size(), es);
            return;
          }
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
      if (!imports[i].empty() && !importBodies(**mOr, imports[i], mods, ctx, es, m.job.in, source))
        return;
      // Large module: split into groups, compiled below in parallel.
      if (opts.splitInstrs && opts.emit == Emit::Obj && opts.optLevel != '0') {
        unsigned n = 0, nfuncs = 0;
        for (Function &f : **mOr) {
          n += f.getInstructionCount();
          nfuncs += !f.isDeclaration();
        }
        if (n > opts.splitInstrs) {
          auto t0 = std::chrono::steady_clock::now();
          unsigned half = std::max(1u, opts.splitInstrs / 2);
          unsigned k = std::min({16u, nfuncs, (n + half - 1) / half});
          auto bcs = splitModule(**mOr, k, m.keyB);
          std::string list;
          std::lock_guard<std::mutex> lock(mu);
          for (unsigned g = 0; g < k; ++g) {
            if (const char *d = getenv("MARMOT_SPLIT_DUMP")) // debugging aid
              writeFile((Twine(d) + "/" + sys::path::filename(m.job.in) + "." + Twine(g) + ".bc").str(),
                        bcs[g]);
            std::string gk = hashHex(fp + "|group|" + bcs[g]);
            list += gk + "\n";
            groups.push_back({i, g, std::move(bcs[g]), gk});
          }
          ++stats.splitModules;
          stats.groups += k;
          if (cacheObjects)
            cache.put(key, "parts", list);
          t.link += msSince(t0);
          return;
        }
      }
      std::string out = m.job.out;
      if (opts.prepareOnly) // the object goes to the cache only
        out = m.job.out + ".prepare.tmp";
      if (!optimizeAndEmit(**mOr, *tm, m.job.in, out, opts, t, es))
        return;
      ++stats.compiled;
      if (auto obj = MemoryBuffer::getFile(out, false, false)) {
        if (cacheObjects)
          cache.put(key, "o", (*obj)->getBuffer());
        fragment(out, (*obj)->getBuffer(), key);
      }
      if (opts.prepareOnly)
        sys::fs::remove(out);
    });
    // The groups, largest first, all threads.
    if (!groups.empty()) {
      llvm::stable_sort(groups, [](const Group &a, const Group &b) {
        return a.bitcode.size() > b.bitcode.size();
      });
      std::vector<unsigned> nparts(jobs.size(), 0);
      for (const Group &g : groups)
        nparts[g.job] = std::max(nparts[g.job], g.index + 1);
      DefaultThreadPool pool(hardware_concurrency(threads));
      for (Group &g : groups)
        pool.async([&, gp = &g] {
          std::string name = jobs[gp->job].in + "#" + std::to_string(gp->index);
          watchdogBegin(name);
          std::string msgs;
          raw_string_ostream es(msgs);
          PhaseTimes t;
          std::string out = partPath(gp->job, gp->index);
          if (opts.prepareOnly)
            out += ".prepare.tmp";
          bool done = false;
          if (cacheObjects)
            if (auto obj = cache.get(gp->key, "o")) {
              if (!opts.prepareOnly && !writeFile(out, *obj))
                es << out << ": cannot write\n";
              fragment(out, *obj, gp->key);
              done = true;
            }
          if (!done) {
            std::string err;
            auto tm = createTargetMachine(opts, err);
            LLVMContext ctx;
            auto mOr = parseBitcodeFile(MemoryBufferRef(gp->bitcode, name), ctx);
            if (!tm || !mOr) {
              es << name << ": " << (tm ? toString(mOr.takeError()) : err) << "\n";
            } else if (optimizeAndEmit(**mOr, *tm, name, out, opts, t, es)) {
              ++stats.compiled;
              if (auto obj = MemoryBuffer::getFile(out, false, false)) {
                if (cacheObjects)
                  cache.put(gp->key, "o", (*obj)->getBuffer());
                fragment(out, (*obj)->getBuffer(), gp->key);
              }
            }
            if (opts.prepareOnly)
              sys::fs::remove(out);
          }
          watchdogEnd(name);
          report(gp->job, msgs, t);
        });
      pool.wait();
      for (unsigned i = 0; i < jobs.size(); ++i)
        if (nparts[i]) {
          std::string msgs;
          raw_string_ostream es(msgs);
          writeParts(i, nparts[i], es);
          report(i, msgs, PhaseTimes());
        }
    }
    stats.cacheHits = cache.hits;
    stats.cacheMisses = cache.misses;
    if (opts.timePhases)
      for (unsigned i = 0; i < jobs.size(); ++i)
        perJob[i].print(errs, jobs[i].in);
    return ok;
  }

  // Phase 2: definitions by name, roots, reachability. Names are interned
  // to ids first, so the sets below are bit vectors and the maps vectors
  // (string sets and maps took most of the 0.37 s plan of cmd/marmot).
  std::string prunableKey;
  auto t2 = std::chrono::steady_clock::now();
  StringMap<unsigned> nameId;
  std::vector<StringRef> nameOf; // keys of nameId, which stay put
  auto intern = [&](StringRef n) {
    auto [it, inserted] = nameId.try_emplace(n, unsigned(nameOf.size()));
    if (inserted)
      nameOf.push_back(it->getKey());
    return it->second;
  };
  struct DefIds {
    unsigned name;
    std::vector<unsigned> refs, typeUses;
  };
  std::vector<std::vector<DefIds>> ids(mods.size());
  std::vector<std::vector<unsigned>> addrIds(mods.size());
  std::vector<unsigned> rootIds;
  {
    // Each module interns its names on its own (in parallel), then its
    // distinct names are interned once and its ids mapped.
    struct Local {
      StringMap<unsigned> id;
      std::vector<StringRef> names;
      std::vector<unsigned> global;
    };
    std::vector<Local> local(mods.size());
    auto each = [&](auto fn) {
      DefaultThreadPool pool(hardware_concurrency(threads));
      for (unsigned i : order)
        pool.async([&, i] { fn(i); });
      pool.wait();
    };
    each([&](unsigned i) {
      Local &l = local[i];
      auto lid = [&](StringRef n) {
        auto [it, inserted] = l.id.try_emplace(n, unsigned(l.names.size()));
        if (inserted)
          l.names.push_back(it->getKey());
        return it->second;
      };
      ids[i].resize(mods[i].defs.size());
      for (unsigned j = 0; j < mods[i].defs.size(); ++j) {
        const Def &d = mods[i].defs[j];
        DefIds &x = ids[i][j];
        x.name = lid(d.qname);
        x.refs.reserve(d.refs.size());
        for (const std::string &r : d.refs)
          x.refs.push_back(lid(r));
        for (const std::string &u : d.typeUses)
          x.typeUses.push_back(lid(u));
      }
      for (auto &[name, type] : mods[i].addrTaken)
        addrIds[i].push_back(lid(name));
    });
    for (unsigned i = 0; i < mods.size(); ++i) {
      Local &l = local[i];
      l.global.reserve(l.names.size());
      for (StringRef n : l.names)
        l.global.push_back(intern(n));
      for (const std::string &r : mods[i].extraRoots)
        rootIds.push_back(intern(r));
    }
    each([&](unsigned i) {
      const std::vector<unsigned> &g = local[i].global;
      for (DefIds &x : ids[i]) {
        x.name = g[x.name];
        for (unsigned &r : x.refs)
          r = g[r];
        for (unsigned &u : x.typeUses)
          u = g[u];
      }
      for (unsigned &n : addrIds[i])
        n = g[n];
      local[i] = Local();
    });
  }
  rootIds.push_back(intern("main"));
  rootIds.push_back(intern("marmot_main"));
  if (opts.rtBitcode) {
    std::string err;
    const RuntimeLib *rt = opts.rtBitcode->get(err);
    if (!rt) {
      errs << "marmot-backend: " << err << "\n";
      return false;
    }
    for (const auto &e : rt->referenced)
      rootIds.push_back(intern(e.getKey()));
  }
  const unsigned nnames = nameOf.size();
  std::vector<SmallVector<std::pair<unsigned, unsigned>, 1>> defsOf(nnames);
  for (unsigned i = 0; i < mods.size(); ++i)
    for (unsigned j = 0; j < ids[i].size(); ++j)
      defsOf[ids[i][j].name].push_back({i, j});

  BitVector isRoot(nnames);
  for (unsigned r : rootIds)
    isRoot.set(r);
  for (unsigned n = 0; n < nnames; ++n)
    if (!defsOf[n].empty() && isCName(nameOf[n]))
      isRoot.set(n);

  BitVector live(nnames);
  std::vector<unsigned> work;
  auto mark = [&](unsigned n) {
    if (!live.test(n)) {
      live.set(n);
      work.push_back(n);
    }
  };
  for (unsigned n : isRoot.set_bits())
    mark(n);
  while (!work.empty()) {
    unsigned n = work.back();
    work.pop_back();
    for (auto [mi, di] : defsOf[n])
      for (unsigned r : ids[mi][di].refs)
        mark(r);
  }
  // Phase 3 asks by name.
  auto isLive = [&](StringRef n) {
    auto it = nameId.find(n);
    return it != nameId.end() && live.test(it->second);
  };

  // Imports: a live function in module T calling an importable function
  // defined only in another module S.
  std::vector<std::map<unsigned, std::set<std::string>>> imports(mods.size());
  // The module whose (live or imported) code references a name: -1 none,
  // -2 more than one.
  std::vector<int> usedBy(nnames, -1);
  auto use = [&](unsigned n, unsigned i) {
    int &u = usedBy[n];
    if (u == -1)
      u = int(i);
    else if (u != int(i))
      u = -2;
  };
  // The plan is transitive: an imported body's callees are candidates too
  // (so runtime.slicecopy imported into main brings runtime.memmove along),
  // up to kImportBudget imported functions per module.
  constexpr unsigned kImportBudget = 400;
  for (unsigned i = 0; i < mods.size(); ++i) {
    SmallVector<unsigned, 64> work;
    DenseSet<unsigned> taken;
    for (unsigned j = 0; j < ids[i].size(); ++j) {
      if (!live.test(ids[i][j].name))
        continue;
      bool isFunc = mods[i].defs[j].isFunc;
      for (unsigned r : ids[i][j].refs) {
        use(r, i);
        if (isFunc)
          work.push_back(r);
      }
    }
    unsigned count = 0;
    while (!work.empty() && opts.wpoImport && opts.optLevel != '0') {
      unsigned r = work.pop_back_val();
      if (defsOf[r].size() != 1)
        continue;
      auto [si, sj] = defsOf[r].front();
      const Def &callee = mods[si].defs[sj];
      if (si == i || !callee.isFunc || !callee.importable || count >= kImportBudget)
        continue;
      if (!taken.insert(r).second)
        continue;
      imports[i][si].insert(nameOf[r].str());
      ++count;
      ++stats.imported;
      for (unsigned rr : ids[si][sj].refs) {
        use(rr, i);
        work.push_back(rr);
      }
    }
  }
  for (unsigned i = 0; i < mods.size(); ++i)
    for (unsigned j = 0; j < ids[i].size(); ++j) {
      bool isFunc = mods[i].defs[j].isFunc;
      stats.functions += isFunc;
      stats.liveFunctions += isFunc && live.test(ids[i][j].name);
    }
  // A linkonce or weak definition (type descriptor, itab, generic instance,
  // wrapper) may be copied into any module, and LLVM may fold its constant
  // contents there (an itab slot becomes a direct call). What it references
  // is therefore used everywhere.
  BitVector shared(nnames);
  for (unsigned i = 0; i < mods.size(); ++i)
    for (unsigned j = 0; j < ids[i].size(); ++j)
      if (mods[i].defs[j].copyable && live.test(ids[i][j].name))
        for (unsigned r : ids[i][j].refs)
          shared.set(r);

  // Reflection tables: with no reflect, reflectlite or
  // rtabi code live, a type whose descriptor is used only to allocate needs
  // neither its fields table (typehash, typeequal) nor its methods table
  // (getitab).
  StringSet<> prunable;
  {
    bool reflectLive = false;
    BitVector keep(nnames);
    for (unsigned i = 0; i < mods.size(); ++i)
      for (unsigned j = 0; j < ids[i].size(); ++j) {
        if (!live.test(ids[i][j].name))
          continue;
        const Def &d = mods[i].defs[j];
        StringRef n = d.qname;
        if (d.isFunc && (n.starts_with("reflect.") || n.starts_with("internal/reflectlite.") ||
                         n.starts_with("internal/rtabi.")))
          reflectLive = true;
        for (unsigned u : ids[i][j].typeUses)
          keep.set(u);
      }
    if (!reflectLive && opts.wpoPruneTypes)
      for (unsigned i = 0; i < mods.size(); ++i)
        for (unsigned j = 0; j < ids[i].size(); ++j) {
          const Def &d = mods[i].defs[j];
          unsigned n = ids[i][j].name;
          if (!d.isFunc && live.test(n) && !keep.test(n) && isDescriptor(d.qname))
            prunable.insert(d.qname);
        }
    std::vector<std::string> sorted;
    for (auto &e : prunable)
      sorted.push_back(e.getKey().str());
    llvm::sort(sorted);
    std::string k;
    for (const std::string &n : sorted)
      k += n + "\n";
    prunableKey = hashHex(k);
    stats.prunedTypes = prunable.size();
  }

  // Indirect call candidates: address-taken functions by LLVM type.
  // A dead function cannot be the target of a live call. A linkonce or weak
  // candidate may be dropped by its defining module, so another module must
  // not start referencing it; such a candidate counts (keeping the call
  // indirect elsewhere) but is promoted only where it is defined.
  std::map<std::string, std::set<std::string>> byType;
  StringSet<> copyableCand;
  for (unsigned i = 0; i < mods.size(); ++i)
    for (unsigned k = 0; k < addrIds[i].size(); ++k) {
      auto &[name, type] = mods[i].addrTaken[k];
      unsigned n = addrIds[i][k];
      if (!defsOf[n].empty()) {
        if (!live.test(n))
          continue;
        for (auto [mi, di] : defsOf[n])
          if (mods[mi].defs[di].copyable)
            copyableCand.insert(name);
      }
      byType[type].insert(name);
    }
  std::string byTypeKey;
  for (auto &[type, names] : byType) {
    byTypeKey += type + ":";
    for (const std::string &n : names)
      byTypeKey += n + ",";
    byTypeKey += "\n";
  }
  byTypeKey = hashHex(byTypeKey);
  // An address-taken function is called directly from other modules after
  // promotion, so it must stay global.
  for (auto &[type, names] : byType)
    if (names.size() <= 2)
      for (const std::string &n : names)
        shared.set(nameId.find(n)->second);

  // Functions each module internalizes: live, external, defined only there,
  // used by no other module, not referenced by copyable definitions, not
  // roots.
  std::vector<std::vector<std::string>> internal(mods.size());
  for (unsigned i = 0; i < mods.size(); ++i) {
    for (unsigned j = 0; j < ids[i].size(); ++j) {
      const Def &d = mods[i].defs[j];
      unsigned n = ids[i][j].name;
      if (!d.isFunc || d.local || !live.test(n) || isRoot.test(n) || shared.test(n))
        continue;
      if (defsOf[n].size() != 1)
        continue;
      int u = usedBy[n];
      if (u == -2 || (u >= 0 && u != int(i)))
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
  // Large modules are split into groups after the whole-program rewrite
  // (as in dev mode, splitModule): the groups compile in parallel and are
  // cached by their bitcode, so an edit recompiles the groups it changed.
  // The main module of cmd/marmot (4.3 MB of MLIR) took 3.1 s on one
  // thread after every edit.
  struct Group {
    unsigned job, index;
    std::string bitcode, key;
  };
  std::vector<Group> groups;
  auto partPath = [&](unsigned i, unsigned g) {
    return g == 0 ? jobs[i].out : jobs[i].out + "." + std::to_string(g) + ".o";
  };
  auto writeParts = [&](unsigned i, unsigned k, raw_ostream &es) {
    std::string list;
    for (unsigned g = 0; g < k; ++g)
      list += partPath(i, g) + "\n";
    if (!writeFile(jobs[i].out + ".parts", list))
      es << jobs[i].out << ".parts: cannot write\n";
  };
  parallel([&](unsigned i, raw_ostream &es, PhaseTimes &t) {
    ModuleInfo &info = mods[i];
    if (info.job.out == "-") // no output wanted (as in dev mode)
      return;
    auto t0 = std::chrono::steady_clock::now();
    sys::fs::remove(info.job.out + ".parts");
    // The key: this module's bitcode, which of its definitions are live, what
    // it internalizes, and the bitcode of every module it imports from.
    std::string key;
    if (cacheObjects) {
      std::string k = info.keyB + "|wpo|" + byTypeKey + "|" + prunableKey + "|";
      std::vector<std::string> liveDefs;
      for (const Def &d : info.defs)
        if (isLive(d.qname))
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
        fragment(info.job.out, *obj, key);
        internalized += internal[i].size();
        t.link += msSince(t0);
        return;
      }
      // A split module whose groups are all cached.
      if (auto list = opts.splitInstrs ? cache.get(key, "parts") : std::nullopt) {
        SmallVector<StringRef, 16> keys;
        StringRef(*list).split(keys, '\n', -1, false);
        std::vector<std::string> objs;
        for (StringRef gk : keys)
          if (auto obj = cache.get(gk.str(), "o"))
            objs.push_back(std::move(*obj));
        if (objs.size() == keys.size()) {
          for (unsigned g = 0; g < objs.size(); ++g) {
            if (!writeFile(partPath(i, g), objs[g]))
              es << partPath(i, g) << ": cannot write\n";
            fragment(partPath(i, g), objs[g], keys[g].str());
          }
          writeParts(i, objs.size(), es);
          internalized += internal[i].size();
          t.link += msSince(t0);
          return;
        }
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
      if (!f.isDeclaration() && !isLive(qualify(f, i))) {
        f.deleteBody();
        f.setComdat(nullptr);
        if (f.hasLocalLinkage())
          deadLocal.push_back(&f);
      }
    for (GlobalVariable &v : m->globals())
      if (!v.isDeclaration() && !v.hasAppendingLinkage() && !isLive(qualify(v, i))) {
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
    // Drop the fields and methods tables of prunable types: null the
    // pointer and zero the count that follows it, in the descriptor
    // (structtype.fields, nfields) and its uncommon part (methods, mcount,
    // xcount). Every copy of a linkonce descriptor gets the same rewrite.
    if (!prunable.empty())
      for (GlobalVariable &v : m->globals()) {
        if (!v.hasInitializer() || !v.getName().starts_with("type:"))
          continue;
        StringRef n = v.getName();
        bool isUncommon = n.ends_with(".uncommon");
        StringRef desc = isUncommon ? n.drop_back(strlen(".uncommon")) : n;
        if (!prunable.contains(desc))
          continue;
        std::string table = (desc + (isUncommon ? ".methods" : ".fields")).str();
        auto *cs = dyn_cast<ConstantStruct>(v.getInitializer());
        if (!cs)
          continue;
        SmallVector<Constant *, 8> elems;
        for (unsigned e = 0; e < cs->getNumOperands(); ++e)
          elems.push_back(cs->getOperand(e));
        bool changed = false;
        for (unsigned e = 0; e < elems.size(); ++e) {
          auto *g = dyn_cast<GlobalValue>(elems[e]->stripPointerCasts());
          if (!g || g->getName() != table)
            continue;
          elems[e] = Constant::getNullValue(elems[e]->getType());
          unsigned counts = isUncommon ? 2 : 1;
          for (unsigned c = 1; c <= counts && e + c < elems.size(); ++c)
            if (elems[e + c]->getType()->isIntegerTy())
              elems[e + c] = Constant::getNullValue(elems[e + c]->getType());
          changed = true;
        }
        if (changed)
          v.setInitializer(ConstantStruct::get(cs->getType(), elems));
      }

    // Imports.
    if (!importBodies(*m, imports[i], mods, ctx, es, info.job.in, source))
      return;
    // Guarded devirtualization of indirect calls (func values, interface
    // methods): when at most two address-taken functions in the program have
    // the call's type, call them directly behind a pointer compare; the
    // original indirect call stays on the fallback path.
    if (opts.optLevel != '0') {
      SmallVector<CallBase *, 64> indirect;
      for (Function &f : *m)
        if (!f.isDeclaration())
          for (BasicBlock &bb : f)
            for (Instruction &inst : bb)
              if (auto *cb = dyn_cast<CallBase>(&inst);
                  cb && cb->isIndirectCall() && !cb->isInlineAsm())
                indirect.push_back(cb);
      for (CallBase *cb : indirect) {
        auto it = byType.find(typeString(cb->getFunctionType()));
        if (it == byType.end() || it->second.size() > 2)
          continue;
        SmallVector<Function *, 2> targets;
        for (const std::string &q : it->second) {
          StringRef name = q;
          if (name.contains(':')) { // local symbol: only from its own module
            auto [mod, local] = name.split(':');
            if (mod != std::to_string(i))
              break;
            name = local;
          }
          Function *f = m->getFunction(name);
          if (copyableCand.contains(q) && (!f || f->isDeclaration()))
            break; // not defined here: may not exist at link time
          if (!f)
            f = Function::Create(cb->getFunctionType(), GlobalValue::ExternalLinkage, name,
                                 m.get());
          if (f->getFunctionType() != cb->getFunctionType())
            break;
          targets.push_back(f);
        }
        if (targets.size() != it->second.size())
          continue;
        CallBase *cur = cb;
        unsigned promoted = 0;
        for (Function *f : targets) {
          if (!isLegalToPromote(*cur, f))
            break;
          MDNode *w = MDBuilder(ctx).createBranchWeights(1000, 1);
          promoteCallWithIfThenElse(*cur, f, w);
          ++promoted;
          ++stats.devirtualized;
          // promoteCallWithIfThenElse leaves cur as the indirect fallback.
        }
        // Closed world: every Go func value and itab entry points at an
        // address-taken function of the program, so with all candidates
        // promoted the fallback cannot run. A trap there (instead of the
        // indirect call) frees the hot path from an unknown call that
        // clobbers memory; if the assumption were ever wrong it fails loudly.
        if (promoted == targets.size() && opts.wpoClosedWorld) {
          IRBuilder<> ib(cur);
          ib.CreateIntrinsic(Intrinsic::trap, ArrayRef<Type *>{}, ArrayRef<Value *>{});
          if (!cur->getType()->isVoidTy())
            cur->replaceAllUsesWith(PoisonValue::get(cur->getType()));
          cur->eraseFromParent();
        }
      }
    }
    if (opts.splitInstrs && opts.emit == Emit::Obj && opts.optLevel != '0') {
      unsigned n = 0, nfuncs = 0;
      for (Function &f : *m) {
        n += f.getInstructionCount();
        nfuncs += !f.isDeclaration();
      }
      if (n > opts.splitInstrs) {
        unsigned half = std::max(1u, opts.splitInstrs / 2);
        unsigned k = std::min({16u, nfuncs, (n + half - 1) / half});
        auto bcs = splitModule(*m, k, info.keyB);
        std::string list;
        std::lock_guard<std::mutex> lock(mu);
        for (unsigned g = 0; g < k; ++g) {
          std::string gk = hashHex(fp + "|wpo-group|" + bcs[g]);
          list += gk + "\n";
          groups.push_back({i, g, std::move(bcs[g]), gk});
        }
        ++stats.splitModules;
        stats.groups += k;
        if (cacheObjects && !key.empty())
          cache.put(key, "parts", list);
        t.link += msSince(t0);
        return;
      }
    }
    t.link += msSince(t0);
    if (!optimizeAndEmit(*m, *tm, info.job.in, info.job.out, opts, t, es))
      return;
    ++stats.compiled;
    if (auto obj = MemoryBuffer::getFile(info.job.out, false, false)) {
      if (cacheObjects)
        cache.put(key, "o", (*obj)->getBuffer());
      fragment(info.job.out, (*obj)->getBuffer(), key);
    }
  });
  // D: the groups, largest first, on all threads.
  if (!groups.empty()) {
    llvm::stable_sort(groups, [](const Group &a, const Group &b) {
      return a.bitcode.size() > b.bitcode.size();
    });
    std::vector<unsigned> nparts(jobs.size(), 0);
    for (const Group &g : groups)
      nparts[g.job] = std::max(nparts[g.job], g.index + 1);
    DefaultThreadPool pool(hardware_concurrency(threads));
    for (Group &g : groups)
      pool.async([&, gp = &g] {
        std::string name = jobs[gp->job].in + "#" + std::to_string(gp->index);
        watchdogBegin(name);
        std::string msgs;
        raw_string_ostream es(msgs);
        PhaseTimes t;
        std::string out = partPath(gp->job, gp->index);
        bool done = false;
        if (cacheObjects)
          if (auto obj = cache.get(gp->key, "o")) {
            if (!writeFile(out, *obj))
              es << out << ": cannot write\n";
            fragment(out, *obj, gp->key);
            done = true;
          }
        if (!done) {
          std::string err;
          auto tm = createTargetMachine(opts, err);
          LLVMContext ctx;
          auto mOr = parseBitcodeFile(MemoryBufferRef(gp->bitcode, name), ctx);
          if (!tm || !mOr) {
            es << name << ": " << (tm ? toString(mOr.takeError()) : err) << "\n";
          } else if (optimizeAndEmit(**mOr, *tm, name, out, opts, t, es)) {
            ++stats.compiled;
            if (auto obj = MemoryBuffer::getFile(out, false, false)) {
              if (cacheObjects)
                cache.put(gp->key, "o", (*obj)->getBuffer());
              fragment(out, (*obj)->getBuffer(), gp->key);
            }
          }
        }
        watchdogEnd(name);
        report(gp->job, msgs, t);
      });
    pool.wait();
    for (unsigned i = 0; i < jobs.size(); ++i)
      if (nparts[i]) {
        std::string msgs;
        raw_string_ostream es(msgs);
        writeParts(i, nparts[i], es);
        report(i, msgs, PhaseTimes());
      }
  }
  stats.internalized = internalized;
  stats.cacheHits = cache.hits;
  stats.cacheMisses = cache.misses;
  if (opts.timePhases)
    for (unsigned i = 0; i < jobs.size(); ++i)
      perJob[i].print(errs, jobs[i].in);
  return ok;
}

} // namespace marmot
