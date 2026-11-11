// marmot-pcln: builds the compact pc table (docs/pcln.md) from the DWARF
// line tables of a linked program: the binary itself (ELF) or its dSYM
// (Mach-O; run dsymutil first). The runtime uses the table for tracebacks,
// runtime.Caller and FuncForPC, without a DWARF parser.
//
//   marmot-pcln -o table.bin [-base-from BINARY] DWARF_FILE
#include "llvm/DebugInfo/DWARF/DWARFContext.h"
#include "llvm/Object/MachO.h"
#include "llvm/Object/ObjectFile.h"
#include "llvm/Support/CommandLine.h"
#include "llvm/Support/InitLLVM.h"
#include "llvm/Support/raw_ostream.h"

#include <cstring>
#include <map>

using namespace llvm;
using namespace llvm::object;

static cl::opt<std::string> input(cl::Positional, cl::desc("<binary or dSYM DWARF file>"),
                                  cl::Required);
static cl::opt<std::string> output("o", cl::desc("Output table"), cl::Required);
static cl::opt<std::string> mainPkg(
    "main-pkg", cl::desc("Package path of the main package; its functions are named main.X, as gc "
                         "names them"));
static cl::opt<std::string> baseFrom(
    "base-from", cl::desc("Binary whose image base the table is relative to (default: input)"));

namespace {

// Image base: the address pcs are stored relative to. Mach-O: the __TEXT
// segment address; ELF: the lowest loadable segment (0 for PIE).
uint64_t imageBase(const ObjectFile &obj) {
  if (auto *m = dyn_cast<MachOObjectFile>(&obj)) {
    for (const auto &lc : m->load_commands())
      if (lc.C.cmd == MachO::LC_SEGMENT_64) {
        auto seg = m->getSegment64LoadCommand(lc);
        if (StringRef(seg.segname) == "__TEXT")
          return seg.vmaddr;
      }
    return 0;
  }
  uint64_t base = UINT64_MAX;
  for (const SectionRef &s : obj.sections())
    if (s.isText() || s.isData() || s.isBSS())
      base = std::min(base, s.getAddress());
  return base == UINT64_MAX ? 0 : base & ~uint64_t(0xfff);
}

void uvarint(std::string &out, uint64_t v) {
  while (v >= 0x80) {
    out.push_back(char(v | 0x80));
    v >>= 7;
  }
  out.push_back(char(v));
}
void svarint(std::string &out, int64_t v) { uvarint(out, (uint64_t(v) << 1) ^ uint64_t(v >> 63)); }
void u32(std::string &out, uint32_t v) {
  for (int i = 0; i < 4; ++i)
    out.push_back(char(v >> (8 * i)));
}
void u64(std::string &out, uint64_t v) {
  for (int i = 0; i < 8; ++i)
    out.push_back(char(v >> (8 * i)));
}

// "PKG.f" -> "main.f" for the main package, also inside type arguments
// ("pkg.F[PKG.T]"): every occurrence that starts the string or follows a
// character that cannot be part of a package path.
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

// Text symbols of a binary, as address -> (name, end of its section),
// Mach-O names without the leading underscore.
void textSymbols(const ObjectFile &obj,
                 std::map<uint64_t, std::pair<std::string, uint64_t>> &out) {
  bool macho = isa<MachOObjectFile>(&obj);
  for (const SymbolRef &sym : obj.symbols()) {
    auto type = sym.getType();
    auto addr = sym.getAddress();
    auto name = sym.getName();
    auto sec = sym.getSection();
    if (!type || !addr || !name || !sec) {
      consumeError(type.takeError());
      consumeError(addr.takeError());
      consumeError(name.takeError());
      consumeError(sec.takeError());
      continue;
    }
    if (*type != SymbolRef::ST_Function || *sec == obj.section_end() || !(*sec)->isText())
      continue;
    StringRef n = *name;
    if (macho && n.starts_with("_"))
      n = n.drop_front();
    if (n.empty() || n.starts_with("ltmp") || n.starts_with("l_") || n.starts_with(".L"))
      continue;
    out.try_emplace(*addr, n.str(), (*sec)->getAddress() + (*sec)->getSize());
  }
}

struct Func {
  uint64_t lo, hi;
  std::string name;
  std::vector<std::tuple<uint64_t, uint32_t, uint32_t>> rows; // pc, line, file index
};

} // namespace

int main(int argc, char **argv) {
  InitLLVM init(argc, argv);
  cl::ParseCommandLineOptions(argc, argv, "marmot pc table builder\n");
  auto objOr = ObjectFile::createObjectFile(input);
  if (!objOr) {
    errs() << "marmot-pcln: " << input << ": " << toString(objOr.takeError()) << "\n";
    return 1;
  }
  const ObjectFile &obj = *objOr->getBinary();
  uint64_t base = imageBase(obj);
  if (!baseFrom.empty()) {
    auto b = ObjectFile::createObjectFile(baseFrom);
    if (!b) {
      errs() << "marmot-pcln: " << baseFrom << ": " << toString(b.takeError()) << "\n";
      return 1;
    }
    base = imageBase(*b->getBinary());
  }
  std::unique_ptr<DWARFContext> dwarf = DWARFContext::create(obj);

  std::map<uint64_t, Func> funcs; // by low pc
  std::map<std::string, uint32_t> fileIndex;
  std::vector<std::string> files;
  auto fileId = [&](const std::string &f) {
    auto [it, fresh] = fileIndex.try_emplace(f, files.size());
    if (fresh)
      files.push_back(f);
    return it->second;
  };

  for (const auto &cu : dwarf->compile_units()) {
    const DWARFDebugLine::LineTable *lt = dwarf->getLineTableForUnit(cu.get());
    if (!lt)
      continue;
    // Subprograms: names and ranges.
    for (const auto &entry : cu->dies()) {
      DWARFDie die(cu.get(), &entry);
      if (die.getTag() != dwarf::DW_TAG_subprogram)
        continue;
      uint64_t lo, hi, idx;
      if (!die.getLowAndHighPC(lo, hi, idx) || hi <= lo)
        continue;
      const char *name = die.getName(DINameKind::LinkageName);
      if (!name)
        name = die.getName(DINameKind::ShortName);
      Func &f = funcs[lo];
      f.lo = lo;
      f.hi = hi;
      f.name = name ? name : "?";
    }
    for (const auto &row : lt->Rows) {
      if (row.EndSequence || row.Line == 0)
        continue;
      auto it = funcs.upper_bound(row.Address.Address);
      if (it == funcs.begin())
        continue;
      --it;
      if (row.Address.Address >= it->second.hi)
        continue;
      std::string fname;
      lt->getFileNameByIndex(row.File, cu->getCompilationDir(),
                             DILineInfoSpecifier::FileLineInfoKind::AbsoluteFilePath, fname);
      it->second.rows.push_back({row.Address.Address, row.Line, fileId(fname)});
    }
  }

  // Every other text symbol (C runtime, assembly) gets an entry with a name
  // and no lines, so that the range of the Go function before it ends there.
  // Symbols inside a function with DWARF ranges are labels, not functions.
  {
    std::map<uint64_t, std::pair<std::string, uint64_t>> syms;
    textSymbols(obj, syms);
    if (!baseFrom.empty())
      if (auto b = ObjectFile::createObjectFile(baseFrom))
        textSymbols(*b->getBinary(), syms);
    std::vector<uint64_t> add;
    for (auto &[addr, sym] : syms) {
      auto it = funcs.upper_bound(addr);
      if (it == funcs.begin() || std::prev(it)->second.hi <= addr)
        add.push_back(addr); // not inside (or at the start of) a DWARF function
    }
    for (uint64_t addr : add) {
      auto &sym = syms[addr];
      Func &f = funcs[addr];
      f.lo = addr;
      f.hi = sym.second; // only for the end of the table; lookups stop at the next start
      f.name = sym.first;
    }
  }
  for (auto &[lo, f] : funcs)
    f.name = renameMain(f.name);

  // Version 2 (docs/pcln.md). Arm64 instructions are 4 bytes, so pc deltas
  // are stored in instructions there.
  uint32_t quantum = obj.getArch() == Triple::aarch64 ? 4 : 1;
  std::string strs;
  std::map<std::string, uint32_t> strOff;
  auto addStr = [&](const std::string &s) {
    auto [it, fresh] = strOff.try_emplace(s, strs.size());
    if (fresh) {
      strs += s;
      strs.push_back('\0');
    }
    return it->second;
  };
  // Prefix tables: directories of files, package paths of functions. A
  // file is uvarint dir + base name; a function name uvarint pkg + rest.
  std::vector<std::string> dirs, pkgs;
  std::map<std::string, uint32_t> dirIdx, pkgIdx;
  auto prefixed = [&](const std::string &full, bool isFunc) {
    size_t cut;
    if (isFunc) {
      size_t slash = full.rfind('/', full.find('('));
      size_t dot = full.find('.', slash == std::string::npos ? 0 : slash + 1);
      cut = dot == std::string::npos ? 0 : dot + 1;
    } else {
      size_t slash = full.rfind('/');
      cut = slash == std::string::npos ? 0 : slash + 1;
    }
    std::string head = full.substr(0, cut);
    auto &idx = isFunc ? pkgIdx : dirIdx;
    auto &list = isFunc ? pkgs : dirs;
    auto [it, fresh] = idx.try_emplace(head, list.size());
    if (fresh)
      list.push_back(head);
    std::string rec;
    uvarint(rec, it->second + 1); // never a zero byte: records are NUL terminated
    rec += full.substr(cut);
    return addStr(rec);
  };
  std::vector<uint32_t> fileRec;
  for (const std::string &f : files)
    fileRec.push_back(prefixed(f, false));

  std::string ftab, lines;
  uint64_t textEnd = 0;
  for (auto &[lo, f] : funcs) {
    std::stable_sort(f.rows.begin(), f.rows.end());
    // Only rows that change the line or the file (DWARF also has rows for
    // columns and statement flags).
    std::vector<std::tuple<uint64_t, uint32_t, uint32_t>> rows;
    for (auto &r : f.rows)
      if (rows.empty() || std::get<1>(rows.back()) != std::get<1>(r) ||
          std::get<2>(rows.back()) != std::get<2>(r))
        rows.push_back(r);
    uint32_t lineOff = lines.size();
    uint32_t entryLine = rows.empty() ? 0 : std::get<1>(rows.front());
    uint32_t entryFile = rows.empty() ? 0 : std::get<2>(rows.front());
    uvarint(lines, rows.empty() ? 0 : rows.size() - 1);
    uvarint(lines, entryLine);
    uvarint(lines, entryFile);
    uint64_t pc = rows.empty() ? f.lo : std::get<0>(rows.front());
    int64_t line = entryLine;
    uint32_t file = entryFile;
    for (size_t r = 1; r < rows.size(); ++r) {
      auto &[rpc, rline, rfile] = rows[r];
      uvarint(lines, (rpc - pc) / quantum);
      int64_t delta = int64_t(rline) - line;
      bool fileChange = rfile != file;
      svarint(lines, delta * 2 + (fileChange ? 1 : 0));
      if (fileChange)
        uvarint(lines, rfile);
      pc = rpc;
      line = rline;
      file = rfile;
    }
    u32(ftab, uint32_t(f.lo - base));
    u32(ftab, prefixed(f.name, true));
    u32(ftab, lineOff);
    textEnd = std::max(textEnd, f.hi - base);
  }
  std::string fileTab, dirTab, pkgTab;
  for (uint32_t o : fileRec)
    u32(fileTab, o);
  for (const std::string &d : dirs)
    u32(dirTab, addStr(d));
  for (const std::string &p : pkgs)
    u32(pkgTab, addStr(p));

  const uint32_t hdr = 8 + 4 * 13;
  uint32_t funcOff = hdr, fileOff = funcOff + ftab.size(), dirOff = fileOff + fileTab.size(),
           pkgOff = dirOff + dirTab.size(), lineOff = pkgOff + pkgTab.size(),
           strOff2 = lineOff + lines.size();
  std::string table = "MRMTPCLN";
  u32(table, 2);
  u32(table, funcs.size());
  u32(table, files.size());
  u32(table, dirs.size());
  u32(table, pkgs.size());
  u32(table, quantum);
  u32(table, uint32_t(textEnd));
  u32(table, funcOff);
  u32(table, fileOff);
  u32(table, dirOff);
  u32(table, pkgOff);
  u32(table, lineOff);
  u32(table, strOff2);
  table += ftab + fileTab + dirTab + pkgTab + lines + strs;

  std::error_code ec;
  raw_fd_ostream os(output, ec);
  if (ec) {
    errs() << "marmot-pcln: " << output << ": " << ec.message() << "\n";
    return 1;
  }
  os << table;
  errs() << "marmot-pcln: " << funcs.size() << " functions, " << files.size() << " files, "
         << table.size() << " bytes (functions " << ftab.size() << ", lines " << lines.size()
         << ", strings " << strs.size() << ")\n";
  return 0;
}
