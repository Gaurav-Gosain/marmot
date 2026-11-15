// marmot-pcln: builds the compact pc table. The runtime uses
// the table for tracebacks, runtime.Caller and FuncForPC, without a DWARF
// parser.
//
// From DWARF (the linked binary on ELF, its dSYM on Mach-O):
//   marmot-pcln -o table.bin [-base-from BINARY] DWARF_FILE
// From the fragments the backend writes next to each object (no dSYM, no
// relink):
//   marmot-pcln -frags LIST -estimate [-extra-objects LIST]  prints an upper bound
//                                                            of the table size
//   marmot-pcln -frags LIST -patch BINARY    writes the table into BINARY's
//                                            __TEXT,__marmot_pcln placeholder; exit
//                                            3 and the size needed on stdout if
//                                            the placeholder is too small
//   marmot-pcln -frags LIST -binary BINARY -o table.bin
// From a final wasm module (after wasm-opt, whose DWARF is stale):
//   marmot-pcln -wasm-final -o table.bin MODULE.wasm   functions from the code
//                                                     section, names from the
//                                                     name section, no lines
#include "marmot/Driver/Pcln.h"

#include "llvm/ADT/StringMap.h"
#include "llvm/Object/Archive.h"
#include "llvm/Object/MachO.h"
#include "llvm/Object/ObjectFile.h"
#include "llvm/Support/CommandLine.h"
#include "llvm/Support/InitLLVM.h"
#include "llvm/Support/MemoryBuffer.h"
#include "llvm/Support/raw_ostream.h"

#include <algorithm>
#include <atomic>
#include <cstring>
#include <map>
#include <thread>

using namespace llvm;
using namespace llvm::object;
using namespace marmot;

static cl::opt<std::string> input(cl::Positional, cl::desc("<binary or dSYM DWARF file>"));
static cl::opt<std::string> output("o", cl::desc("Output table"));
static cl::opt<std::string> mainPkg(
    "main-pkg", cl::desc("Package path of the main package; its functions are named main.X, as gc "
                         "names them"));
static cl::opt<unsigned> version("table-version",
                                 cl::desc("Table version: 2, or 3 with inlined frames"),
                                 cl::init(2));
static cl::opt<std::string> baseFrom(
    "base-from", cl::desc("Binary whose image base the table is relative to (default: input)"));
static cl::opt<std::string> fragsList("frags",
                                      cl::desc("File listing fragment files, one per line"));
static cl::opt<bool> estimate("estimate", cl::desc("With -frags: print an upper bound of the "
                                                    "table size for any link of these objects"));
static cl::opt<std::string> extraObjects(
    "extra-objects",
    cl::desc("With -estimate: file listing objects and archives without fragments (their "
             "text symbols get name-only entries)"));
static cl::opt<std::string> patch("patch", cl::desc("With -frags: write the table into this "
                                                     "linked Mach-O binary"));
static cl::opt<bool> stripLocals("strip-locals",
                                 cl::desc("With -patch: also drop the local symbols (as ld -x)"));
static cl::opt<std::string> binaryOpt("binary",
                                      cl::desc("With -frags -o: the linked binary"));
static cl::opt<bool> wasmFinal(
    "wasm-final", cl::desc("Input is a final wasm module: functions from its code section and "
                           "name section, without DWARF or lines"));
static cl::opt<bool> namesOnly("names", cl::desc("Print the table name of each symbol read "
                                                 "from stdin, one per line (tests)"));

namespace {

// closing returns the index of the bracket that closes the one at s[i] ('['
// or '('), or npos.
size_t closing(const std::string &s, size_t i) {
  int d = 0;
  for (size_t j = i; j < s.size(); j++) {
    char c = s[j];
    if (c == '[' || c == '(')
      d++;
    else if ((c == ']' || c == ')') && --d == 0)
      return j;
  }
  return std::string::npos;
}

// gcName turns a function symbol into the name gc gives the function, which
// runtime.FuncForPC, tracebacks and profiles show (tools match these names):
//   main.plain$fv                       main.plain (a func value wrapper is
//                                       named for the wrapped function)
//   main.adder$1, main.f$1$2            main.adder.func1, main.f.func1.func2
//   main.Gen$1[int], main.Gen[int]      main.Gen[...].func1, main.Gen[...]
//   (*p.T).Get$bound, (p.T).Val$bound   p.(*T).Get-fm, p.T.Val-fm
//   (*p.T).Get$thunk$fv                 p.(*T).Get (method expression)
//   main.init#1                         main.init.0
// and drops the @pkg suffix of wrappers of unexported methods. Other
// names, the C runtime's among them, are unchanged.
std::string gcName(const std::string &sym) {
  if (sym.find_first_of("$#[(@") == std::string::npos)
    return sym;
  std::string s = sym;
  // The @pkg suffix: the first '@' outside brackets.
  for (size_t i = 0, d = 0; i < s.size(); i++) {
    char c = s[i];
    if (c == '[' || c == '(')
      d++;
    else if (c == ']' || c == ')')
      d--;
    else if (c == '@' && d == 0) {
      s.resize(i);
      break;
    }
  }
  // Wrapper names start with the receiver type: (*p.T[A]).M.
  if (s[0] == '(') {
    size_t c = closing(s, 0);
    if (c == std::string::npos)
      return sym;
    std::string in = s.substr(1, c - 1);
    bool star = !in.empty() && in[0] == '*';
    if (star)
      in.erase(0, 1);
    size_t lb = in.find('[');
    std::string base = in.substr(0, lb);
    size_t dot = base.rfind('.');
    if (dot == std::string::npos || base.find_first_of("{ ") != std::string::npos)
      return sym;
    std::string tn = base.substr(dot + 1) + (lb == std::string::npos ? "" : "[...]");
    s = base.substr(0, dot) + "." + (star ? "(*" + tn + ")" : tn) + s.substr(c + 1);
  }
  // The package path ends at the first '.' after its last '/'.
  size_t p0 = s.find_first_of("([$");
  size_t slash = s.rfind('/', p0);
  size_t dot = s.find('.', slash == std::string::npos ? 0 : slash + 1);
  if (dot == std::string::npos || dot > p0)
    return sym;
  std::string base = s.substr(0, dot + 1), clos;
  bool fm = false, generic = false;
  size_t i = dot + 1;
  auto flush = [&] {
    base += clos;
    clos.clear();
  };
  while (i < s.size()) {
    char c = s[i];
    if (c == '(' || c == '[') {
      size_t e = closing(s, i);
      if (e == std::string::npos)
        return sym;
      if (c == '(') {
        // A receiver: its type arguments print as [...].
        std::string r = s.substr(i, e + 1 - i);
        size_t lb = r.find('[');
        if (lb != std::string::npos) {
          size_t re = closing(r, lb);
          if (re == std::string::npos)
            return sym;
          r = r.substr(0, lb) + "[...]" + r.substr(re + 1);
          generic = true;
        }
        flush();
        base += r;
      } else if (!generic) {
        // Type arguments go after the function, before its closures.
        base += "[...]";
        generic = true;
      }
      i = e + 1;
    } else if (c == '$') {
      size_t e = s.find_first_of(".$[(", i + 1);
      std::string w = s.substr(i + 1, e == std::string::npos ? std::string::npos : e - i - 1);
      if (!w.empty() && std::all_of(w.begin(), w.end(), ::isdigit))
        clos += ".func" + w;
      else if (w == "bound")
        fm = true;
      else if (w != "fv" && w != "thunk" && w != "ifn" && w != "tfn")
        clos += "$" + w;
      i = e == std::string::npos ? s.size() : e;
    } else {
      size_t e = s.find_first_of("([$", i);
      std::string w = s.substr(i, e == std::string::npos ? std::string::npos : e - i);
      flush();
      // init#N: the Nth init function is init.N-1.
      size_t h = w.find("init#");
      if (h != std::string::npos && (h == 0 || w[h - 1] == '.') &&
          std::all_of(w.begin() + h + 5, w.end(), ::isdigit) && h + 5 < w.size())
        w = w.substr(0, h) + "init." + std::to_string(std::stoul(w.substr(h + 5)) - 1);
      base += w;
      i = e == std::string::npos ? s.size() : e;
    }
  }
  return base + clos + (fm ? "-fm" : "");
}

std::string renameMain(const std::string &sym) {
  std::string name = gcName(sym);
  if (mainPkg.empty() || mainPkg == "main")
    return name;
  std::string from = mainPkg + ".", out;
  size_t i = 0;
  while (i < name.size()) {
    bool boundary = i == 0 || strchr("[],*( ", name[i - 1]);
    if (boundary && name.compare(i, from.size(), from) == 0) {
      out += "main.";
      i += from.size();
    } else {
      out += name[i++];
    }
  }
  return out;
}

int fail(const Twine &msg) {
  errs() << "marmot-pcln: " << msg << "\n";
  return 1;
}

bool readList(const std::string &path, std::vector<std::string> &out) {
  auto buf = MemoryBuffer::getFile(path);
  if (!buf)
    return false;
  SmallVector<StringRef, 0> lines;
  (*buf)->getBuffer().split(lines, '\n', -1, false);
  for (StringRef l : lines)
    if (!l.trim().empty())
      out.push_back(l.trim().str());
  return true;
}

uint32_t quantumOf(const ObjectFile &obj) { return obj.getArch() == Triple::aarch64 ? 4 : 1; }

// Text symbol name -> address; names defined twice (local symbols of two
// objects) map to 0, their functions get no entry.
// Reads the function symbols of a linked binary once: their addresses by
// name (0 for a name with two addresses) and the text symbols for
// addTextSymbols (two passes over 45000 long names cost 14 ms more).
void scanSymbols(const ObjectFile &obj, StringMap<uint64_t> &out,
                 std::map<uint64_t, std::pair<std::string, uint64_t>> &text) {
  bool macho = isa<MachOObjectFile>(&obj);
  for (const SymbolRef &sym : obj.symbols()) {
    auto type = sym.getType();
    if (!type) {
      consumeError(type.takeError());
      continue;
    }
    if (*type != SymbolRef::ST_Function)
      continue;
    auto addr = sym.getAddress();
    auto name = sym.getName();
    auto sec = sym.getSection();
    if (!addr || !name || !sec) {
      consumeError(addr.takeError());
      consumeError(name.takeError());
      consumeError(sec.takeError());
      continue;
    }
    StringRef n = *name;
    if (macho && n.starts_with("_"))
      n = n.drop_front();
    auto [it, fresh] = out.try_emplace(n, *addr);
    if (!fresh && it->second != *addr)
      it->second = 0;
    if (*sec == obj.section_end() || !(*sec)->isText() || n.empty() || n.starts_with("ltmp") ||
        n.starts_with("l_") || n.starts_with(".L"))
      continue;
    text.try_emplace(*addr, n.str(), (*sec)->getAddress() + (*sec)->getSize());
  }
}

int fromFragments() {
  std::vector<std::string> frags;
  if (!readList(fragsList, frags))
    return fail(fragsList + ": cannot read");
  pcln::Table t;
  std::string err;
  if (estimate) {
    // Every function present, at made-up addresses: function records and
    // line programs do not depend on the layout, so the real table is not
    // larger (dead stripping only removes functions).
    uint64_t next = 0x1000;
    std::vector<std::string> names;
    for (const std::string &f : frags) {
      auto buf = MemoryBuffer::getFile(f);
      if (!buf)
        return fail(f + ": cannot read");
      if (!pcln::addFragment(
              (*buf)->getBuffer(),
              [&](StringRef) {
                next += 0x10000;
                return next;
              },
              t, err))
        return fail(f + ": " + err);
    }
    std::vector<std::string> extra;
    if (!extraObjects.empty() && !readList(extraObjects, extra))
      return fail(extraObjects + ": cannot read");
    for (const std::string &path : extra) {
      auto buf = MemoryBuffer::getFile(path, false, false);
      if (!buf)
        return fail(path + ": cannot read");
      auto addSyms = [&](const ObjectFile &o) {
        for (const SymbolRef &sym : o.symbols()) {
          auto type = sym.getType();
          auto name = sym.getName();
          if (!type || !name) {
            consumeError(type.takeError());
            consumeError(name.takeError());
            continue;
          }
          if (*type != SymbolRef::ST_Function)
            continue;
          next += 0x10000;
          pcln::Func &f = t.funcs[next];
          f.lo = next;
          f.hi = next + 4;
          f.name = name->str();
        }
      };
      auto bin = createBinary((*buf)->getMemBufferRef());
      if (!bin) {
        consumeError(bin.takeError());
        continue;
      }
      if (auto *o = dyn_cast<ObjectFile>(bin->get())) {
        addSyms(*o);
      } else if (auto *a = dyn_cast<Archive>(bin->get())) {
        Error e = Error::success();
        for (auto &c : a->children(e)) {
          auto cb = c.getAsBinary();
          if (!cb) {
            consumeError(cb.takeError());
            continue;
          }
          if (auto *o = dyn_cast<ObjectFile>(cb->get()))
            addSyms(*o);
        }
        consumeError(std::move(e));
      }
    }
    // Margin for text symbols from neither list (linker-made, system
    // libraries); a placeholder that is still too small makes -patch exit 3.
    std::string table = pcln::encode(t, 0, 1, version, renameMain);
    outs() << table.size() + table.size() / 20 + 256 << "\n";
    return 0;
  }
  std::string binPath = patch.empty() ? binaryOpt : patch;
  if (binPath.empty())
    return fail("-frags needs -estimate, -patch or -binary");
  auto binOr = ObjectFile::createObjectFile(binPath);
  if (!binOr)
    return fail(binPath + ": " + toString(binOr.takeError()));
  const ObjectFile &bin = *binOr->getBinary();
  // The fragments are read on worker threads while this one reads the
  // symbols, then placed in list order (the first of two at one address
  // wins, as before).
  std::vector<pcln::Fragment> parsed(frags.size());
  std::vector<std::string> fragErr(frags.size());
  std::atomic<size_t> next{0};
  auto work = [&] {
    for (size_t i; (i = next++) < frags.size();) {
      auto buf = MemoryBuffer::getFile(frags[i]);
      if (!buf)
        fragErr[i] = "cannot read";
      else if (std::string e; !pcln::parseFragment((*buf)->getBuffer(), parsed[i], e))
        fragErr[i] = e;
    }
  };
  unsigned nt = std::clamp(std::thread::hardware_concurrency(), 1u, 8u);
  std::vector<std::thread> pool;
  for (unsigned k = 0; k < nt; ++k)
    pool.emplace_back(work);
  StringMap<uint64_t> addrs;
  std::map<uint64_t, std::pair<std::string, uint64_t>> text;
  scanSymbols(bin, addrs, text);
  for (std::thread &th : pool)
    th.join();
  auto lookup = [&](StringRef n) {
    auto it = addrs.find(n);
    return it == addrs.end() ? uint64_t(0) : it->second;
  };
  for (size_t i = 0; i < frags.size(); ++i) {
    if (!fragErr[i].empty())
      return fail(frags[i] + ": " + fragErr[i]);
    pcln::addParsed(parsed[i], lookup, t);
  }
  pcln::addTextSymbols(text, t);
  std::string table = pcln::encode(t, pcln::imageBase(bin), quantumOf(bin), version, renameMain);
  uint32_t dropped = 0;
  if (!patch.empty()) {
    if (!pcln::patchMachO(patch, table, err, stripLocals, &dropped)) {
      fail(err);
      // Too small a placeholder: exit 3, so the caller can relink with the
      // size printed on stdout.
      if (err.find("the table needs") != std::string::npos) {
        outs() << table.size() << "\n";
        return 3;
      }
      return 1;
    }
  } else {
    std::error_code ec;
    raw_fd_ostream os(output, ec);
    if (ec)
      return fail(output + ": " + ec.message());
    os << table;
  }
  errs() << "marmot-pcln: " << t.funcs.size() << " functions, " << t.files.size() << " files, "
         << table.size() << " bytes\n";
  if (stripLocals && !patch.empty())
    errs() << "marmot-pcln: " << dropped << " local symbols dropped\n";
  return 0;
}

// readWasmFinal fills t with one function per code section body: lo is the
// body's offset in the code section contents (where its size starts, as
// LLVM's symbol addresses and wasm DWARF count), hi its end, the name from
// the name section. A parser of its own: LLVM's reader rejects the custom
// section order binaryen writes ("out of order section type: 0").
bool readWasmFinal(StringRef data, pcln::Table &t, std::string &err) {
  const uint8_t *p = data.bytes_begin(), *end = data.bytes_end();
  bool bad = false;
  auto u8 = [&](const uint8_t *&q, const uint8_t *e) -> uint8_t {
    if (q >= e) {
      bad = true;
      return 0;
    }
    return *q++;
  };
  auto leb = [&](const uint8_t *&q, const uint8_t *e) -> uint64_t {
    uint64_t v = 0;
    for (unsigned shift = 0; shift < 64; shift += 7) {
      uint8_t b = u8(q, e);
      v |= uint64_t(b & 0x7f) << shift;
      if (!(b & 0x80) || bad)
        return v;
    }
    bad = true;
    return v;
  };
  auto skip = [&](const uint8_t *&q, const uint8_t *e, uint64_t n) {
    if (n > uint64_t(e - q))
      bad = true;
    else
      q += n;
  };
  auto str = [&](const uint8_t *&q, const uint8_t *e) -> StringRef {
    uint64_t n = leb(q, e);
    const uint8_t *s = q;
    skip(q, e, n);
    return bad ? StringRef() : StringRef(reinterpret_cast<const char *>(s), n);
  };
  auto limits = [&](const uint8_t *&q, const uint8_t *e) {
    uint8_t flags = u8(q, e);
    leb(q, e);
    if (flags & 1)
      leb(q, e);
  };
  auto valType = [&](const uint8_t *&q, const uint8_t *e) {
    uint8_t v = u8(q, e);
    if (v == 0x63 || v == 0x64) // (ref null? ht): a heap type follows
      leb(q, e);
  };
  if (data.size() < 8 || memcmp(p, "\0asm", 4) != 0)
    return err = "not a wasm module", false;
  p += 8;
  uint64_t imported = 0;
  std::vector<std::pair<uint64_t, uint64_t>> bodies; // [lo, hi) in code section contents
  std::map<uint64_t, std::string> names;              // function index -> name
  while (p < end && !bad) {
    uint8_t id = u8(p, end);
    uint64_t size = leb(p, end);
    if (bad || size > uint64_t(end - p))
      break;
    const uint8_t *s = p, *e = p + size;
    p = e;
    if (id == 2) { // imports: function indices start after the imported functions
      for (uint64_t n = leb(s, e); n-- && !bad;) {
        str(s, e);
        str(s, e);
        switch (u8(s, e)) {
        case 0:
          leb(s, e);
          imported++;
          break;
        case 1:
          valType(s, e);
          limits(s, e);
          break;
        case 2:
          limits(s, e);
          break;
        case 3:
          valType(s, e);
          u8(s, e);
          break;
        case 4:
          u8(s, e);
          leb(s, e);
          break;
        default:
          bad = true;
        }
      }
    } else if (id == 10) {
      const uint8_t *start = s;
      for (uint64_t n = leb(s, e); n-- && !bad;) {
        uint64_t lo = s - start;
        uint64_t len = leb(s, e);
        skip(s, e, len);
        bodies.push_back({lo, uint64_t(s - start)});
      }
    } else if (id == 0 && str(s, e) == "name") {
      while (s < e && !bad) {
        uint8_t sub = u8(s, e);
        uint64_t len = leb(s, e);
        if (bad || len > uint64_t(e - s)) {
          bad = true;
          break;
        }
        const uint8_t *ss = s, *se = s + len;
        s = se;
        if (sub != 1) // function names
          continue;
        for (uint64_t n = leb(ss, se); n-- && !bad;) {
          uint64_t idx = leb(ss, se);
          StringRef nm = str(ss, se);
          if (!bad)
            names[idx] = nm.str();
        }
      }
    }
  }
  if (bad)
    return err = "malformed wasm module", false;
  if (bodies.empty())
    return err = "no code section", false;
  for (size_t i = 0; i < bodies.size(); ++i) {
    // A function without a name still ends the range of the one before it;
    // it gets the name browsers show for it.
    auto it = names.find(imported + i);
    pcln::Func &f = t.funcs[bodies[i].first];
    f.lo = bodies[i].first;
    f.hi = bodies[i].second;
    f.name = it != names.end() && !it->second.empty()
                 ? it->second
                 : "wasm-function[" + std::to_string(imported + i) + "]";
  }
  return true;
}

} // namespace

int main(int argc, char **argv) {
  InitLLVM init(argc, argv);
  cl::ParseCommandLineOptions(argc, argv, "marmot pc table builder\n");
  if (const char *v = getenv("MARMOT_PCLN_VERSION"); v && !version.getNumOccurrences())
    version = atoi(v);
  if (namesOnly) {
    auto in = MemoryBuffer::getSTDIN();
    if (!in)
      return fail("stdin: " + in.getError().message());
    SmallVector<StringRef, 0> lines;
    (*in)->getBuffer().split(lines, '\n', -1, false);
    for (StringRef l : lines)
      outs() << renameMain(l.str()) << "\n";
    return 0;
  }
  if (!fragsList.empty())
    return fromFragments();
  if (wasmFinal) {
    if (input.empty() || output.empty())
      return fail("usage: marmot-pcln -wasm-final -o table.bin MODULE.wasm");
    auto buf = MemoryBuffer::getFile(input, false, false);
    if (!buf)
      return fail(input + ": " + buf.getError().message());
    pcln::Table t;
    std::string err;
    if (!readWasmFinal((*buf)->getBuffer(), t, err))
      return fail(input + ": " + err);
    std::string table = pcln::encode(t, 0, 1, version, renameMain);
    std::error_code ec;
    raw_fd_ostream os(output, ec);
    if (ec)
      return fail(output + ": " + ec.message());
    os << table;
    errs() << "marmot-pcln: " << t.funcs.size() << " functions, " << table.size() << " bytes\n";
    return 0;
  }
  if (input.empty() || output.empty())
    return fail("usage: marmot-pcln -o table.bin [-base-from BINARY] DWARF_FILE");
  auto objOr = ObjectFile::createObjectFile(input);
  if (!objOr)
    return fail(input + ": " + toString(objOr.takeError()));
  const ObjectFile &obj = *objOr->getBinary();
  uint64_t base = pcln::imageBase(obj);
  OwningBinary<ObjectFile> baseBin;
  if (!baseFrom.empty()) {
    auto b = ObjectFile::createObjectFile(baseFrom);
    if (!b)
      return fail(baseFrom + ": " + toString(b.takeError()));
    baseBin = std::move(*b);
    base = pcln::imageBase(*baseBin.getBinary());
  }
  pcln::Table t;
  pcln::readDwarf(obj, version, t);
  // Every other text symbol (C runtime, assembly) gets an entry with a name
  // and no lines, so that the range of the Go function before it ends there.
  pcln::addTextSymbols(obj, t);
  if (baseBin.getBinary())
    pcln::addTextSymbols(*baseBin.getBinary(), t);
  std::string table = pcln::encode(t, base, quantumOf(obj), version, renameMain);
  std::error_code ec;
  raw_fd_ostream os(output, ec);
  if (ec)
    return fail(output + ": " + ec.message());
  os << table;
  errs() << "marmot-pcln: " << t.funcs.size() << " functions, " << t.files.size() << " files, "
         << table.size() << " bytes\n";
  return 0;
}
