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

#include <map>

using namespace llvm;
using namespace llvm::object;

static cl::opt<std::string> input(cl::Positional, cl::desc("<binary or dSYM DWARF file>"),
                                  cl::Required);
static cl::opt<std::string> output("o", cl::desc("Output table"), cl::Required);
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

  // Strings: function names, then file names.
  std::string strs, lines, table;
  auto addStr = [&](const std::string &s) {
    uint32_t off = strs.size();
    strs += s;
    strs.push_back('\0');
    return off;
  };
  std::vector<uint32_t> fileOff;
  for (const std::string &f : files)
    fileOff.push_back(addStr(f));
  std::string ftab;
  for (auto &[lo, f] : funcs) {
    std::stable_sort(f.rows.begin(), f.rows.end());
    uint32_t lineOff = lines.size();
    uint32_t entryLine = f.rows.empty() ? 0 : std::get<1>(f.rows.front());
    uint32_t entryFile = f.rows.empty() ? 0 : std::get<2>(f.rows.front());
    uvarint(lines, f.rows.size());
    uvarint(lines, entryFile);
    uint64_t pc = f.lo;
    int64_t line = entryLine;
    uint32_t file = entryFile;
    for (auto &[rpc, rline, rfile] : f.rows) {
      uvarint(lines, rpc - pc);
      // The low bit says whether a file index follows.
      int64_t delta = int64_t(rline) - line;
      bool fileChange = rfile != file;
      svarint(lines, delta * 2 + (fileChange ? 1 : 0));
      if (fileChange)
        uvarint(lines, rfile);
      pc = rpc;
      line = rline;
      file = rfile;
    }
    u64(ftab, f.lo - base);
    u32(ftab, uint32_t(f.hi - f.lo));
    u32(ftab, addStr(f.name));
    u32(ftab, lineOff);
    u32(ftab, entryLine);
  }
  std::string ftabFiles;
  for (uint32_t o : fileOff)
    u32(ftabFiles, o);

  // Header: magic, version, counts, offsets (from the start of the table).
  const uint32_t hdr = 8 + 4 * 7;
  uint32_t funcOff = hdr, fileTabOff = funcOff + ftab.size(),
           lineOffAll = fileTabOff + ftabFiles.size(), strOff = lineOffAll + lines.size();
  table += "MRMTPCLN";
  u32(table, 1);
  u32(table, funcs.size());
  u32(table, files.size());
  u32(table, funcOff);
  u32(table, fileTabOff);
  u32(table, lineOffAll);
  u32(table, strOff);
  table += ftab + ftabFiles + lines + strs;

  std::error_code ec;
  raw_fd_ostream os(output, ec);
  if (ec) {
    errs() << "marmot-pcln: " << output << ": " << ec.message() << "\n";
    return 1;
  }
  os << table;
  errs() << "marmot-pcln: " << funcs.size() << " functions, " << files.size() << " files, "
         << table.size() << " bytes\n";
  return 0;
}
