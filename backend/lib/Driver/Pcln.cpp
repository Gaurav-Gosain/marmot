// Pcln.cpp: see Pcln.h and docs/pcln.md.
#include "marmot/Driver/Pcln.h"

#include "llvm/DebugInfo/DWARF/DWARFContext.h"
#include "llvm/Object/MachO.h"
#include "llvm/Object/ObjectFile.h"
#include "llvm/Support/FileSystem.h"
#include "llvm/Support/MemoryBuffer.h"
#include "llvm/Support/Path.h"
#include "llvm/Support/SHA1.h"
#include "llvm/Support/SHA256.h"
#include "llvm/Support/raw_ostream.h"

#include <algorithm>
#include <cstring>

using namespace llvm;
using namespace llvm::object;

namespace marmot::pcln {

uint32_t Table::fileId(const std::string &f) {
  auto [it, fresh] = fileIndex.try_emplace(f, files.size());
  if (fresh)
    files.push_back(f);
  return it->second;
}

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

namespace {

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

// A function under construction while reading DWARF: its section (ELF
// objects have one per function and all start at 0) and its file indices
// local to the reader.
struct Raw {
  uint64_t sec, lo, hi;
  Func f;
};

const char *dieName(const DWARFDie &d) {
  const char *n = d.getName(DINameKind::LinkageName);
  return n ? n : d.getName(DINameKind::ShortName);
}

// Reads functions, rows and inlined calls of every compile unit. Section
// indices only matter in relocatable ELF objects (a section per function, all
// at address 0); elsewhere addresses are unique and section indices of rows
// and functions need not agree.
void readUnits(DWARFContext &dwarf, unsigned version, std::vector<Raw> &raws,
               const std::function<uint32_t(const std::string &)> &fileId, bool useSections) {
  auto normSec = [&](uint64_t s) {
    return !useSections || s == object::SectionedAddress::UndefSection ? 0 : s;
  };
  for (const auto &cu : dwarf.compile_units()) {
    const DWARFDebugLine::LineTable *lt = dwarf.getLineTableForUnit(cu.get());
    if (!lt)
      continue;
    size_t first = raws.size();
    for (const auto &entry : cu->dies()) {
      DWARFDie die(cu.get(), &entry);
      if (die.getTag() != dwarf::DW_TAG_subprogram)
        continue;
      uint64_t lo, hi, sec;
      if (!die.getLowAndHighPC(lo, hi, sec) || hi <= lo)
        continue;
      Raw r{normSec(sec), lo, hi, {}};
      r.f.lo = lo;
      r.f.hi = hi;
      const char *name = dieName(die);
      r.f.name = name ? name : "?";
      std::function<void(DWARFDie, int, unsigned)> walk = [&](DWARFDie d, int parent,
                                                                unsigned depth) {
        for (DWARFDie c : d.children()) {
          int here = parent;
          unsigned cdepth = depth;
          if (c.getTag() == dwarf::DW_TAG_inlined_subroutine) {
            Inline in;
            in.parent = parent;
            in.depth = cdepth = depth + 1;
            const char *n = dieName(c);
            in.name = n ? n : "?";
            uint32_t cf = 0, cl = 0, cc = 0, cd = 0;
            c.getCallerFrame(cf, cl, cc, cd);
            std::string fname;
            in.callFile = cf && lt->getFileNameByIndex(
                                    cf, cu->getCompilationDir(),
                                    DILineInfoSpecifier::FileLineInfoKind::AbsoluteFilePath, fname)
                              ? fileId(fname)
                              : 0;
            in.callLine = cl;
            if (auto rs = c.getAddressRanges()) {
              for (auto &rg : *rs)
                if (rg.HighPC > rg.LowPC)
                  in.ranges.push_back({rg.LowPC, rg.HighPC});
            } else {
              consumeError(rs.takeError());
            }
            here = r.f.inl.size();
            r.f.inl.push_back(std::move(in));
          }
          walk(c, here, cdepth);
        }
      };
      if (version >= 3)
        walk(die, -1, 0);
      raws.push_back(std::move(r));
    }
    // Rows go to the function of this unit whose range holds them.
    std::map<std::pair<uint64_t, uint64_t>, size_t> byStart;
    for (size_t i = first; i < raws.size(); ++i)
      byStart[{raws[i].sec, raws[i].lo}] = i;
    for (const auto &row : lt->Rows) {
      if (row.EndSequence || row.Line == 0)
        continue;
      uint64_t sec = normSec(row.Address.SectionIndex);
      auto it = byStart.upper_bound({sec, row.Address.Address});
      if (it == byStart.begin())
        continue;
      --it;
      Raw &r = raws[it->second];
      if (it->first.first != sec || row.Address.Address >= r.hi)
        continue;
      std::string fname;
      lt->getFileNameByIndex(row.File, cu->getCompilationDir(),
                             DILineInfoSpecifier::FileLineInfoKind::AbsoluteFilePath, fname);
      r.f.rows.push_back({row.Address.Address, row.Line, fileId(fname)});
    }
  }
}

} // namespace

void readDwarf(const ObjectFile &obj, unsigned version, Table &t) {
  std::unique_ptr<DWARFContext> dwarf = DWARFContext::create(obj);
  std::vector<Raw> raws;
  readUnits(*dwarf, version, raws, [&](const std::string &f) { return t.fileId(f); }, false);
  for (Raw &r : raws)
    t.funcs[r.lo] = std::move(r.f);
}

void addTextSymbols(const ObjectFile &obj, Table &t) {
  bool macho = isa<MachOObjectFile>(&obj);
  std::map<uint64_t, std::pair<std::string, uint64_t>> syms; // addr -> name, section end
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
    syms.try_emplace(*addr, n.str(), (*sec)->getAddress() + (*sec)->getSize());
  }
  std::vector<uint64_t> add;
  for (auto &[addr, sym] : syms) {
    auto it = t.funcs.upper_bound(addr);
    if (it == t.funcs.begin() || std::prev(it)->second.hi <= addr)
      add.push_back(addr);
  }
  for (uint64_t addr : add) {
    Func &f = t.funcs[addr];
    f.lo = addr;
    f.hi = syms[addr].second; // only for the end of the table; lookups stop at the next start
    f.name = syms[addr].first;
  }
}

// Fragment text, one record per line, fields separated by tabs, names last:
//   P <path>                         file (index = order of P lines)
//   F <size> <name>                  function, then its records:
//   R <offset> <line> <file>         row (only where line or file changes)
//   N <parent+1> <file> <line> <name>  inlined call
//   G <offset> <end>                 a range of the last N
bool makeFragment(MemoryBufferRef object, unsigned version, std::string &out,
                  std::string &err) {
  auto objOr = ObjectFile::createObjectFile(object);
  if (!objOr) {
    err = toString(objOr.takeError());
    return false;
  }
  std::unique_ptr<DWARFContext> dwarf = DWARFContext::create(**objOr);
  std::vector<std::string> files;
  std::map<std::string, uint32_t> fileIndex;
  std::vector<Raw> raws;
  bool elfObject = (*objOr)->isELF() && (*objOr)->isRelocatableObject();
  readUnits(
      *dwarf, version, raws,
      [&](const std::string &f) {
        auto [it, fresh] = fileIndex.try_emplace(f, files.size());
        if (fresh)
          files.push_back(f);
        return it->second;
      },
      elfObject);
  raw_string_ostream os(out);
  for (const std::string &f : files)
    os << "P\t" << f << "\n";
  for (Raw &r : raws) {
    Func &f = r.f;
    os << "F\t" << (r.hi - r.lo) << "\t" << f.name << "\n";
    std::stable_sort(f.rows.begin(), f.rows.end());
    uint32_t line = 0, file = UINT32_MAX;
    for (auto &[pc, l, fi] : f.rows) {
      if (l == line && fi == file)
        continue;
      os << "R\t" << (pc - r.lo) << "\t" << l << "\t" << fi << "\n";
      line = l;
      file = fi;
    }
    for (Inline &in : f.inl) {
      os << "N\t" << (in.parent + 1) << "\t" << in.callFile << "\t" << in.callLine << "\t"
         << in.name << "\n";
      for (auto &[a, b] : in.ranges)
        os << "G\t" << (a - r.lo) << "\t" << (b - r.lo) << "\n";
    }
  }
  return true;
}

bool addFragment(StringRef fragment, const std::function<uint64_t(StringRef)> &addr, Table &t,
                 std::string &err) {
  std::vector<uint32_t> fileMap;
  Func *cur = nullptr;
  uint64_t base = 0;
  auto num = [&](StringRef s) {
    uint64_t v = 0;
    s.getAsInteger(10, v);
    return v;
  };
  while (!fragment.empty()) {
    auto [line, rest] = fragment.split('\n');
    fragment = rest;
    if (line.empty())
      continue;
    char tag = line[0];
    StringRef body = line.size() > 2 ? line.drop_front(2) : StringRef();
    SmallVector<StringRef, 5> f;
    switch (tag) {
    case 'P':
      fileMap.push_back(t.fileId(body.str()));
      break;
    case 'F': {
      auto [size, name] = body.split('\t');
      cur = nullptr;
      uint64_t a = addr(name);
      if (!a || t.funcs.count(a))
        break; // dead stripped (or folded into another function)
      base = a;
      cur = &t.funcs[a];
      cur->lo = a;
      cur->hi = a + num(size);
      cur->name = name.str();
      break;
    }
    case 'R':
      if (!cur)
        break;
      body.split(f, '\t');
      if (f.size() != 3 || num(f[2]) >= fileMap.size()) {
        err = "bad fragment row";
        return false;
      }
      cur->rows.push_back({base + num(f[0]), uint32_t(num(f[1])), fileMap[num(f[2])]});
      break;
    case 'N': {
      if (!cur)
        break;
      body.split(f, '\t', 3);
      if (f.size() != 4 || num(f[1]) >= std::max<size_t>(fileMap.size(), 1)) {
        err = "bad fragment inline record";
        return false;
      }
      Inline in;
      in.parent = int(num(f[0])) - 1;
      in.depth = in.parent < 0 ? 1 : cur->inl[in.parent].depth + 1;
      in.callFile = fileMap.empty() ? 0 : fileMap[num(f[1])];
      in.callLine = num(f[2]);
      in.name = f[3].str();
      cur->inl.push_back(std::move(in));
      break;
    }
    case 'G':
      if (!cur || cur->inl.empty())
        break;
      body.split(f, '\t');
      if (f.size() != 2) {
        err = "bad fragment range";
        return false;
      }
      cur->inl.back().ranges.push_back({base + num(f[0]), base + num(f[1])});
      break;
    default:
      err = "bad fragment record";
      return false;
    }
  }
  return true;
}

std::string encode(Table &t, uint64_t base, uint32_t quantum, unsigned version,
                   const std::function<std::string(const std::string &)> &rename) {
  auto nm = [&](const std::string &n) { return rename ? rename(n) : n; };
  std::string strs;
  std::map<std::string, uint32_t> strIndex;
  auto addStr = [&](const std::string &s) {
    auto [it, fresh] = strIndex.try_emplace(s, strs.size());
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
  for (const std::string &f : t.files)
    fileRec.push_back(prefixed(f, false));

  std::string ftab, lines, inlIdx, inl;
  uint64_t textEnd = 0;
  uint32_t ninl = 0, funcIndex = 0;
  for (auto &[lo, f] : t.funcs) {
    std::stable_sort(f.rows.begin(), f.rows.end());
    // Inline tree (version 3): nodes, then pc ranges mapped to the innermost
    // node that covers them.
    if (version >= 3 && !f.inl.empty()) {
      std::vector<std::tuple<uint64_t, uint64_t, unsigned>> segs; // [lo, hi) -> node + 1
      std::vector<uint64_t> cuts;
      for (auto &in : f.inl)
        for (auto &[a, b] : in.ranges)
          cuts.push_back(a), cuts.push_back(b);
      llvm::sort(cuts);
      cuts.erase(std::unique(cuts.begin(), cuts.end()), cuts.end());
      for (size_t c = 0; c + 1 < cuts.size(); ++c) {
        uint64_t a = cuts[c], b = cuts[c + 1];
        int best = -1;
        for (size_t k = 0; k < f.inl.size(); ++k)
          for (auto &[ra, rb] : f.inl[k].ranges)
            if (ra <= a && b <= rb && (best < 0 || f.inl[k].depth > f.inl[best].depth))
              best = k;
        if (best < 0)
          continue;
        if (!segs.empty() && std::get<1>(segs.back()) == a &&
            std::get<2>(segs.back()) == unsigned(best + 1))
          std::get<1>(segs.back()) = b;
        else
          segs.push_back({a, b, best + 1});
      }
      // A call site without DW_AT_call_line (LLVM merged call locations):
      // use the last line of the parent before the inlined code starts.
      auto nodeAt = [&](uint64_t pc) {
        for (auto &[a, b, node] : segs)
          if (a <= pc && pc < b)
            return int(node) - 1;
        return -1;
      };
      for (Inline &in : f.inl) {
        if (in.callLine || in.ranges.empty())
          continue;
        uint64_t start = std::min_element(in.ranges.begin(), in.ranges.end())->first;
        for (auto r = f.rows.rbegin(); r != f.rows.rend(); ++r)
          if (std::get<0>(*r) < start && nodeAt(std::get<0>(*r)) == in.parent) {
            in.callLine = std::get<1>(*r);
            in.callFile = std::get<2>(*r);
            break;
          }
      }
      u32(inlIdx, funcIndex);
      u32(inlIdx, inl.size());
      ++ninl;
      uvarint(inl, f.inl.size());
      for (auto &in : f.inl) {
        uvarint(inl, in.parent + 1);
        uvarint(inl, prefixed(nm(in.name), true));
        uvarint(inl, in.callFile);
        uvarint(inl, in.callLine);
      }
      uvarint(inl, segs.size());
      uint64_t at = f.lo;
      for (auto &[a, b, node] : segs) {
        uvarint(inl, (a - at) / quantum);
        uvarint(inl, (b - a) / quantum);
        uvarint(inl, node);
        at = b;
      }
    }
    ++funcIndex;
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
    u32(ftab, prefixed(nm(f.name), true));
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

  const uint32_t hdr = 8 + 4 * (version >= 3 ? 16 : 13);
  uint32_t funcOff = hdr, fileOff = funcOff + ftab.size(), dirOff = fileOff + fileTab.size(),
           pkgOff = dirOff + dirTab.size(), lineOff = pkgOff + pkgTab.size(),
           inlIdxOff = lineOff + lines.size(), inlOff = inlIdxOff + inlIdx.size(),
           strOff = inlOff + inl.size();
  std::string table = "MRMTPCLN";
  u32(table, version >= 3 ? 3 : 2);
  u32(table, t.funcs.size());
  u32(table, t.files.size());
  u32(table, dirs.size());
  u32(table, pkgs.size());
  u32(table, quantum);
  u32(table, uint32_t(textEnd));
  u32(table, funcOff);
  u32(table, fileOff);
  u32(table, dirOff);
  u32(table, pkgOff);
  u32(table, lineOff);
  u32(table, strOff);
  if (version >= 3) {
    u32(table, ninl);
    u32(table, inlIdxOff);
    u32(table, inlOff);
  }
  table += ftab + fileTab + dirTab + pkgTab + lines + inlIdx + inl + strs;
  return table;
}

namespace {
uint32_t be32(const uint8_t *p) {
  return uint32_t(p[0]) << 24 | uint32_t(p[1]) << 16 | uint32_t(p[2]) << 8 | p[3];
}
} // namespace

bool patchMachO(const std::string &binary, StringRef table, std::string &err) {
  auto bufOr = MemoryBuffer::getFile(binary, /*IsText=*/false, /*RequiresNullTerminator=*/false);
  if (!bufOr) {
    err = binary + ": " + bufOr.getError().message();
    return false;
  }
  std::string data = (*bufOr)->getBuffer().str();
  auto objOr = ObjectFile::createObjectFile(MemoryBufferRef(data, binary));
  if (!objOr) {
    err = toString(objOr.takeError());
    return false;
  }
  auto *m = dyn_cast<MachOObjectFile>(objOr->get());
  if (!m || !m->is64Bit()) {
    err = binary + ": not a 64-bit Mach-O binary";
    return false;
  }
  uint64_t off = 0, size = 0;
  bool found = false;
  uint32_t sigOff = 0, sigSize = 0;
  for (const auto &lc : m->load_commands()) {
    if (lc.C.cmd == MachO::LC_SEGMENT_64) {
      auto seg = m->getSegment64LoadCommand(lc);
      for (unsigned s = 0; s < seg.nsects; ++s) {
        MachO::section_64 sec = m->getSection64(lc, s);
        if (StringRef(sec.segname, strnlen(sec.segname, 16)) == "__TEXT" &&
            StringRef(sec.sectname, strnlen(sec.sectname, 16)) == "__marmot_pcln") {
          off = sec.offset;
          size = sec.size;
          found = true;
        }
      }
    } else if (lc.C.cmd == MachO::LC_CODE_SIGNATURE) {
      auto cs = m->getLinkeditDataLoadCommand(lc);
      sigOff = cs.dataoff;
      sigSize = cs.datasize;
    }
  }
  if (!found) {
    err = binary + ": no __TEXT,__marmot_pcln section";
    return false;
  }
  if (table.size() > size) {
    err = binary + ": table of " + std::to_string(table.size()) + " bytes, section of " +
          std::to_string(size);
    return false;
  }
  objOr->reset(); // data is changed below
  std::memcpy(&data[off], table.data(), table.size());
  std::memset(&data[off + table.size()], 0, size - table.size());

  // Ad hoc code signature: rehash the pages that hold the section in every
  // code directory.
  if (sigSize) {
    auto *sb = reinterpret_cast<const uint8_t *>(data.data()) + sigOff;
    if (be32(sb) != 0xfade0cc0) {
      err = binary + ": unknown code signature";
      return false;
    }
    uint32_t count = be32(sb + 8);
    for (uint32_t b = 0; b < count; ++b) {
      uint32_t blobOff = be32(sb + 12 + 8 * b + 4);
      auto *cd = reinterpret_cast<uint8_t *>(&data[sigOff + blobOff]);
      if (be32(cd) != 0xfade0c02)
        continue;
      uint32_t hashOffset = be32(cd + 16), nCodeSlots = be32(cd + 28), codeLimit = be32(cd + 32);
      uint8_t hashSize = cd[36], hashType = cd[37], pageShift = cd[39];
      uint64_t page = pageShift ? uint64_t(1) << pageShift : 4096;
      if (hashType != 1 && hashType != 2) {
        err = binary + ": unsupported code signature hash";
        return false;
      }
      for (uint64_t p = off / page; p <= (off + size - 1) / page && p < nCodeSlots; ++p) {
        uint64_t lo = p * page, hi = std::min<uint64_t>(lo + page, codeLimit);
        ArrayRef<uint8_t> bytes(reinterpret_cast<const uint8_t *>(data.data()) + lo, hi - lo);
        uint8_t *slot = cd + hashOffset + p * hashSize;
        if (hashType == 2) {
          auto h = SHA256::hash(bytes);
          std::memcpy(slot, h.data(), std::min<size_t>(hashSize, h.size()));
        } else {
          auto h = SHA1::hash(bytes);
          std::memcpy(slot, h.data(), std::min<size_t>(hashSize, h.size()));
        }
      }
    }
  }

  // A new file (not a write in place): the kernel caches code signatures per
  // file.
  sys::fs::file_status st;
  sys::fs::status(binary, st);
  std::string tmp = binary + ".pcln.tmp";
  {
    std::error_code ec;
    raw_fd_ostream os(tmp, ec);
    if (ec) {
      err = tmp + ": " + ec.message();
      return false;
    }
    os << data;
  }
  sys::fs::setPermissions(tmp, st.permissions());
  if (std::error_code ec = sys::fs::rename(tmp, binary)) {
    err = binary + ": " + ec.message();
    return false;
  }
  return true;
}

} // namespace marmot::pcln
