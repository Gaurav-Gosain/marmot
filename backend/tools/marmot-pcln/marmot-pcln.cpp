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
static cl::opt<std::string> binaryOpt("binary",
                                      cl::desc("With -frags -o: the linked binary"));

namespace {

std::string renameMain(const std::string &name) {
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
  if (!patch.empty()) {
    if (!pcln::patchMachO(patch, table, err)) {
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
  return 0;
}

} // namespace

int main(int argc, char **argv) {
  InitLLVM init(argc, argv);
  cl::ParseCommandLineOptions(argc, argv, "marmot pc table builder\n");
  if (const char *v = getenv("MARMOT_PCLN_VERSION"); v && !version.getNumOccurrences())
    version = atoi(v);
  if (!fragsList.empty())
    return fromFragments();
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
