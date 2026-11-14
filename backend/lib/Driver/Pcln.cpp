// Pcln.cpp: see Pcln.h.
#include "marmot/Driver/Pcln.h"

#include "llvm/ADT/StringMap.h"
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
#ifdef __APPLE__
#include <fcntl.h>
#include <sys/clonefile.h>
#include <unistd.h>
#endif

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
               const std::function<uint32_t(const std::string &)> &fileId, bool useSections,
               std::vector<std::tuple<uint64_t, uint32_t, uint32_t>> *orphans = nullptr) {
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
      Raw *r = nullptr;
      if (it != byStart.begin()) {
        --it;
        Raw &c = raws[it->second];
        if (it->first.first == sec && row.Address.Address < c.hi)
          r = &c;
      }
      if (!r && !orphans)
        continue;
      std::string fname;
      lt->getFileNameByIndex(row.File, cu->getCompilationDir(),
                             DILineInfoSpecifier::FileLineInfoKind::AbsoluteFilePath, fname);
      if (r)
        r->f.rows.push_back({row.Address.Address, row.Line, fileId(fname)});
      else
        orphans->push_back({row.Address.Address, row.Line, fileId(fname)});
    }
  }
}

} // namespace

void readDwarf(const ObjectFile &obj, unsigned version, Table &t) {
  std::unique_ptr<DWARFContext> dwarf = DWARFContext::create(obj);
  std::vector<Raw> raws;
  readUnits(
      *dwarf, version, raws, [&](const std::string &f) { return t.fileId(f); }, false,
      &t.orphans);
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
  addTextSymbols(syms, t);
}

void addTextSymbols(std::map<uint64_t, std::pair<std::string, uint64_t>> &syms, Table &t) {
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
  // Rows without a DWARF subprogram belong to the function symbol before them.
  for (auto &row : t.orphans) {
    auto it = t.funcs.upper_bound(std::get<0>(row));
    if (it == t.funcs.begin())
      continue;
    --it;
    if (std::get<0>(row) < it->second.hi)
      it->second.rows.push_back(row);
  }
  t.orphans.clear();
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

bool parseFragment(StringRef fragment, Fragment &out, std::string &err) {
  Fragment::Fn *cur = nullptr;
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
      out.files.push_back(body.str());
      break;
    case 'F': {
      auto [size, name] = body.split('\t');
      cur = &out.funcs.emplace_back();
      cur->name = name.str();
      cur->size = num(size);
      break;
    }
    case 'R':
      if (!cur)
        break;
      body.split(f, '\t');
      if (f.size() != 3 || num(f[2]) >= out.files.size()) {
        err = "bad fragment row";
        return false;
      }
      cur->rows.push_back({num(f[0]), uint32_t(num(f[1])), uint32_t(num(f[2]))});
      break;
    case 'N': {
      if (!cur)
        break;
      body.split(f, '\t', 3);
      if (f.size() != 4 || num(f[1]) >= std::max<size_t>(out.files.size(), 1)) {
        err = "bad fragment inline record";
        return false;
      }
      Inline in;
      in.parent = int(num(f[0])) - 1;
      if (in.parent >= int(cur->inl.size())) {
        err = "bad fragment inline record";
        return false;
      }
      in.depth = in.parent < 0 ? 1 : cur->inl[in.parent].depth + 1;
      in.callFile = uint32_t(num(f[1]));
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
      cur->inl.back().ranges.push_back({num(f[0]), num(f[1])});
      break;
    default:
      err = "bad fragment record";
      return false;
    }
  }
  return true;
}

void addParsed(Fragment &frag, const std::function<uint64_t(StringRef)> &addr, Table &t) {
  std::vector<uint32_t> fileMap;
  fileMap.reserve(frag.files.size());
  for (const std::string &f : frag.files)
    fileMap.push_back(t.fileId(f));
  for (Fragment::Fn &fn : frag.funcs) {
    uint64_t a = addr(fn.name);
    if (!a || t.funcs.count(a))
      continue; // dead stripped (or folded into another function)
    Func &cur = t.funcs[a];
    cur.lo = a;
    cur.hi = a + fn.size;
    cur.name = std::move(fn.name);
    cur.rows.reserve(fn.rows.size());
    for (auto &[off, line, file] : fn.rows)
      cur.rows.push_back({a + off, line, fileMap[file]});
    for (Inline &in : fn.inl) {
      in.callFile = fileMap.empty() ? 0 : fileMap[in.callFile];
      for (auto &[lo, hi] : in.ranges)
        lo += a, hi += a;
    }
    cur.inl = std::move(fn.inl);
  }
}

bool addFragment(StringRef fragment, const std::function<uint64_t(StringRef)> &addr, Table &t,
                 std::string &err) {
  Fragment f;
  if (!parseFragment(fragment, f, err))
    return false;
  addParsed(f, addr, t);
  return true;
}

std::string encode(Table &t, uint64_t base, uint32_t quantum, unsigned version,
                   const std::function<std::string(const std::string &)> &rename) {
  auto nm = [&](const std::string &n) { return rename ? rename(n) : n; };
  // Hash maps: offsets depend only on the order of first use (a std::map of
  // 45000 names cost a large part of encode).
  std::string strs;
  llvm::StringMap<uint32_t> strIndex;
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
  llvm::StringMap<uint32_t> dirIdx, pkgIdx;
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
void putBe32(uint8_t *p, uint32_t v) {
  p[0] = uint8_t(v >> 24);
  p[1] = uint8_t(v >> 16);
  p[2] = uint8_t(v >> 8);
  p[3] = uint8_t(v);
}
template <class T> T readAt(StringRef data, uint64_t off) {
  T v;
  std::memcpy(&v, data.data() + off, sizeof v);
  return v;
}
template <class T> void writeAt(std::string &s, uint64_t off, const T &v) {
  std::memcpy(&s[off], &v, sizeof v);
}

// A replacement of the bytes at `at` in the output file.
struct Edit {
  uint64_t at;
  std::string bytes;
};

// The bytes [lo, hi) of the output: the input with the edits applied.
void patchedRange(StringRef data, const std::vector<Edit> &edits, uint64_t lo, uint64_t hi,
                  std::string &out) {
  out.assign(hi - lo, '\0');
  if (lo < data.size())
    std::memcpy(out.data(), data.data() + lo, std::min<uint64_t>(hi, data.size()) - lo);
  for (const Edit &e : edits) {
    uint64_t a = std::max(lo, e.at), z = std::min(hi, e.at + e.bytes.size());
    if (a < z)
      std::memcpy(&out[a - lo], e.bytes.data() + (a - e.at), z - a);
  }
}

// The tail of __LINKEDIT without the local symbols: the symbol table (globals
// and undefined symbols only), the indirect symbol table renumbered, a new
// string table, then the code signature (written by the caller at
// sigOff). Only for the layout ld64 writes: symbol table, indirect symbols,
// strings and signature last in the file, locals first in the table.
// Edits the load commands in hdr. False (and nothing changed) otherwise.
bool dropLocals(const MachOObjectFile &m, StringRef data, std::string &hdr, uint64_t &newSigOff,
                std::string &tail, uint32_t &dropped) {
  uint64_t symtabCmd = 0, dysymCmd = 0, linkeditCmd = 0;
  MachO::symtab_command st{};
  MachO::dysymtab_command dy{};
  uint64_t sigOff = 0;
  std::vector<std::pair<uint64_t, uint64_t>> other; // what must end before the symbols
  for (const auto &lc : m.load_commands()) {
    uint64_t at = uint64_t(lc.Ptr - data.data());
    switch (lc.C.cmd) {
    case MachO::LC_SYMTAB:
      symtabCmd = at;
      st = m.getSymtabLoadCommand();
      break;
    case MachO::LC_DYSYMTAB:
      dysymCmd = at;
      dy = m.getDysymtabLoadCommand();
      break;
    case MachO::LC_CODE_SIGNATURE:
      sigOff = m.getLinkeditDataLoadCommand(lc).dataoff;
      break;
    case MachO::LC_SEGMENT_64:
    {
      auto seg = m.getSegment64LoadCommand(lc);
      if (StringRef(seg.segname, strnlen(seg.segname, 16)) == "__LINKEDIT")
        linkeditCmd = at;
      else if (seg.filesize)
        other.push_back({seg.fileoff, seg.fileoff + seg.filesize});
      break;
    }
    case MachO::LC_FUNCTION_STARTS:
    case MachO::LC_DATA_IN_CODE:
    case MachO::LC_DYLD_CHAINED_FIXUPS:
    case MachO::LC_DYLD_EXPORTS_TRIE:
    case MachO::LC_LINKER_OPTIMIZATION_HINT:
    case MachO::LC_DYLIB_CODE_SIGN_DRS: {
      auto d = m.getLinkeditDataLoadCommand(lc);
      if (d.datasize)
        other.push_back({d.dataoff, d.dataoff + d.datasize});
      break;
    }
    case MachO::LC_DYLD_INFO:
    case MachO::LC_DYLD_INFO_ONLY: {
      auto d = m.getDyldInfoLoadCommand(lc);
      for (auto [o, s] : {std::pair{d.rebase_off, d.rebase_size}, {d.bind_off, d.bind_size},
                          {d.weak_bind_off, d.weak_bind_size}, {d.lazy_bind_off, d.lazy_bind_size},
                          {d.export_off, d.export_size}})
        if (s)
          other.push_back({o, uint64_t(o) + s});
      break;
    }
    default:
      break;
    }
  }
  if (!symtabCmd || !dysymCmd || !linkeditCmd || !sigOff)
    return false;
  uint32_t nloc = dy.nlocalsym;
  if (nloc == 0 || dy.ilocalsym != 0 || dy.iextdefsym != nloc ||
      dy.iundefsym != dy.iextdefsym + dy.nextdefsym || dy.iundefsym + dy.nundefsym != st.nsyms ||
      dy.ntoc || dy.nmodtab || dy.nextrefsyms || dy.nextrel || dy.nlocrel)
    return false;
  uint64_t symEnd = uint64_t(st.symoff) + uint64_t(st.nsyms) * sizeof(MachO::nlist_64);
  uint64_t indEnd = uint64_t(dy.indirectsymoff) + uint64_t(dy.nindirectsyms) * 4;
  if (dy.nindirectsyms && (dy.indirectsymoff < symEnd || indEnd > st.stroff))
    return false;
  if (symEnd > st.stroff || uint64_t(st.stroff) + st.strsize > sigOff || sigOff > data.size())
    return false;
  for (auto [lo, hi] : other)
    if (hi > st.symoff)
      return false;
  auto seg = readAt<MachO::segment_command_64>(StringRef(hdr), linkeditCmd);
  if (seg.fileoff + seg.filesize != data.size())
    return false;

  StringRef strtab = data.substr(st.stroff, st.strsize);
  std::string strs;
  size_t first = strtab.find('\0');
  strs = first == StringRef::npos ? std::string(1, '\0') : strtab.substr(0, first + 1).str();
  tail.clear();
  for (uint32_t k = nloc; k < st.nsyms; ++k) {
    auto n = readAt<MachO::nlist_64>(data, st.symoff + uint64_t(k) * sizeof(MachO::nlist_64));
    if (n.n_strx) {
      if (n.n_strx >= st.strsize)
        return false;
      StringRef name(strtab.data() + n.n_strx, strnlen(strtab.data() + n.n_strx, st.strsize - n.n_strx));
      n.n_strx = uint32_t(strs.size());
      strs += name;
      strs += '\0';
    }
    tail.append(reinterpret_cast<const char *>(&n), sizeof n);
  }
  uint64_t indOff = st.symoff + tail.size();
  for (uint32_t k = 0; k < dy.nindirectsyms; ++k) {
    uint32_t e = readAt<uint32_t>(data, dy.indirectsymoff + uint64_t(k) * 4);
    if (!(e & (MachO::INDIRECT_SYMBOL_LOCAL | MachO::INDIRECT_SYMBOL_ABS))) {
      if (e < nloc)
        return false;
      e -= nloc;
    }
    tail.append(reinterpret_cast<const char *>(&e), 4);
  }
  uint64_t strOff = st.symoff + tail.size();
  while ((strOff + strs.size()) % 16)
    strs += '\0';
  tail += strs;
  newSigOff = st.symoff + tail.size();

  auto sym = readAt<MachO::symtab_command>(StringRef(hdr), symtabCmd);
  sym.nsyms = st.nsyms - nloc;
  sym.stroff = uint32_t(strOff);
  sym.strsize = uint32_t(strs.size());
  writeAt(hdr, symtabCmd, sym);
  auto dys = readAt<MachO::dysymtab_command>(StringRef(hdr), dysymCmd);
  dys.ilocalsym = 0;
  dys.nlocalsym = 0;
  dys.iextdefsym = 0;
  dys.iundefsym = dys.nextdefsym;
  if (dys.nindirectsyms)
    dys.indirectsymoff = uint32_t(indOff);
  writeAt(hdr, dysymCmd, dys);
  dropped = nloc;
  return true;
}

// The ad hoc code signature sig (a superblob) for an output whose signed
// part ends at codeLimit: each code directory gets the hashes of the pages
// edits touched, or that end at a new limit; the others keep theirs.
bool resign(StringRef data, const std::vector<Edit> &edits, StringRef sig, uint64_t oldLimit,
            uint64_t codeLimit, std::string &out, std::string &err, bool hashPages = true) {
  auto *sb = reinterpret_cast<const uint8_t *>(sig.data());
  if (sig.size() < 12 || be32(sb) != 0xfade0cc0) {
    err = "unknown code signature";
    return false;
  }
  uint32_t count = be32(sb + 8);
  if (12 + uint64_t(count) * 8 > sig.size()) {
    err = "bad code signature";
    return false;
  }
  std::vector<std::pair<uint32_t, std::string>> blobs;
  std::string page;
  for (uint32_t b = 0; b < count; ++b) {
    uint32_t type = be32(sb + 12 + 8 * b), off = be32(sb + 12 + 8 * b + 4);
    if (uint64_t(off) + 8 > sig.size()) {
      err = "bad code signature";
      return false;
    }
    uint32_t magic = be32(sb + off), len = be32(sb + off + 4);
    if (uint64_t(off) + len > sig.size()) {
      err = "bad code signature";
      return false;
    }
    std::string blob(sig.substr(off, len));
    if (magic == 0xfade0c02) {
      auto *cd = reinterpret_cast<uint8_t *>(blob.data());
      uint32_t version = be32(cd + 8), hashOffset = be32(cd + 16), identOffset = be32(cd + 20);
      uint32_t nCodeSlots = be32(cd + 28);
      uint8_t hashSize = cd[36], hashType = cd[37], pageShift = cd[39];
      uint32_t scatter = version >= 0x20100 ? be32(cd + 44) : 0;
      uint32_t team = version >= 0x20200 ? be32(cd + 48) : 0;
      uint64_t limit64 = 0;
      if (version >= 0x20300)
        std::memcpy(&limit64, cd + 56, 8);
      if ((hashType != 1 && hashType != 2) || scatter || limit64 || identOffset > hashOffset ||
          team > hashOffset || hashOffset > len) {
        err = "unsupported code signature";
        return false;
      }
      uint64_t ps = pageShift ? uint64_t(1) << pageShift : 4096;
      uint64_t slots = (codeLimit + ps - 1) / ps;
      std::string ncd = blob.substr(0, hashOffset);
      ncd.resize(hashOffset + slots * hashSize);
      auto *n = reinterpret_cast<uint8_t *>(ncd.data());
      putBe32(n + 4, uint32_t(ncd.size()));
      putBe32(n + 28, uint32_t(slots));
      putBe32(n + 32, uint32_t(codeLimit));
      for (uint64_t p = 0; p < slots; ++p) {
        uint64_t lo = p * ps, hi = std::min(lo + ps, codeLimit);
        bool same = p < nCodeSlots && hi == std::min(lo + ps, oldLimit);
        for (const Edit &e : edits)
          if (e.at < hi && e.at + e.bytes.size() > lo)
            same = false;
        uint8_t *slot = n + hashOffset + p * hashSize;
        if (!hashPages)
          continue;
        if (same) {
          std::memcpy(slot, cd + hashOffset + p * hashSize, hashSize);
          continue;
        }
        patchedRange(data, edits, lo, hi, page);
        ArrayRef<uint8_t> bytes(reinterpret_cast<const uint8_t *>(page.data()), page.size());
        if (hashType == 2) {
          auto h = SHA256::hash(bytes);
          std::memcpy(slot, h.data(), std::min<size_t>(hashSize, h.size()));
        } else {
          auto h = SHA1::hash(bytes);
          std::memcpy(slot, h.data(), std::min<size_t>(hashSize, h.size()));
        }
      }
      blob = std::move(ncd);
    } else if (magic != 0xfade0c01) {
      // Only ad hoc signatures (a code directory and requirements).
      err = "not an ad hoc code signature";
      return false;
    }
    blobs.push_back({type, std::move(blob)});
  }
  out.assign(12 + blobs.size() * 8, '\0');
  auto *o = reinterpret_cast<uint8_t *>(out.data());
  putBe32(o, 0xfade0cc0);
  putBe32(o + 8, uint32_t(blobs.size()));
  for (size_t b = 0; b < blobs.size(); ++b) {
    putBe32(reinterpret_cast<uint8_t *>(&out[12 + 8 * b]), blobs[b].first);
    putBe32(reinterpret_cast<uint8_t *>(&out[12 + 8 * b + 4]), uint32_t(out.size()));
    out += blobs[b].second;
  }
  putBe32(reinterpret_cast<uint8_t *>(out.data()) + 4, uint32_t(out.size()));
  return true;
}
} // namespace

bool patchMachO(const std::string &binary, StringRef table, std::string &err, bool stripLocals,
                uint32_t *droppedLocals) {
  if (droppedLocals)
    *droppedLocals = 0;
  auto bufOr = MemoryBuffer::getFile(binary, /*IsText=*/false, /*RequiresNullTerminator=*/false);
  if (!bufOr) {
    err = binary + ": " + bufOr.getError().message();
    return false;
  }
  // The file stays mapped; the edits (the section, and with stripLocals the
  // load commands and the end of __LINKEDIT) and the signature are built
  // apart (a copy of the whole file cost more than the rest).
  StringRef data = (*bufOr)->getBuffer();
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
  uint64_t sigCmd = 0;
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
      sigCmd = uint64_t(lc.Ptr - data.data());
    }
  }
  if (!found) {
    err = binary + ": no __TEXT,__marmot_pcln section";
    return false;
  }
  if (table.size() > size) {
    err = binary + ": the table needs " + std::to_string(table.size()) +
          " bytes, the __marmot_pcln section has " + std::to_string(size);
    return false;
  }
  if (off + size > data.size() || (sigSize && uint64_t(sigOff) + sigSize > data.size())) {
    err = binary + ": bad section or code signature offset";
    return false;
  }
  std::vector<Edit> edits;
  std::string sect(size, '\0');
  std::memcpy(sect.data(), table.data(), table.size());
  edits.push_back({off, std::move(sect)});
  uint64_t end = data.size(), codeLimit = sigOff;
  bool dropping = false;
  // Local symbols: the link kept them for this tool; without them the
  // binary keeps the global and undefined ones, as a link with -x would.
  // Needs the signature to be rebuilt, so only with one.
  if (stripLocals && sigSize) {
    uint32_t sizeofcmds = m->getHeader64().sizeofcmds;
    std::string hdr(data.substr(0, sizeof(MachO::mach_header_64) + sizeofcmds));
    std::string tail;
    uint64_t newSigOff = 0;
    uint32_t dropped = 0;
    if (dropLocals(*m, data, hdr, newSigOff, tail, dropped)) {
      uint64_t symoff = newSigOff - tail.size();
      edits.push_back({0, std::move(hdr)});
      edits.push_back({symoff, std::move(tail)});
      codeLimit = newSigOff;
      dropping = true;
      if (droppedLocals)
        *droppedLocals = dropped;
    }
  }
  objOr->reset();

  // Ad hoc code signature: the pages the edits changed get new hashes.
  std::string sig;
  if (sigSize) {
    std::string newSig;
    // With dropping, first only the new size (the load commands say it).
    if (!resign(data, edits, data.substr(sigOff, sigSize), sigOff, codeLimit, newSig, err,
                /*hashPages=*/!dropping)) {
      err = binary + ": " + err;
      return false;
    }
    if (dropping) {
      // The signature moves to the new end of __LINKEDIT, padded as ld64
      // does; the load commands say where and how large.
      while (newSig.size() % 16)
        newSig += '\0';
      std::string &hdr = edits[1].bytes;
      auto cs = readAt<MachO::linkedit_data_command>(StringRef(hdr), sigCmd);
      cs.dataoff = uint32_t(codeLimit);
      cs.datasize = uint32_t(newSig.size());
      writeAt(hdr, sigCmd, cs);
      end = codeLimit + newSig.size();
      // __LINKEDIT ends with the file.
      uint64_t at = sizeof(MachO::mach_header_64);
      uint32_t ncmds = readAt<MachO::mach_header_64>(StringRef(hdr), 0).ncmds;
      for (uint32_t c = 0; c < ncmds; ++c) {
        auto lc = readAt<MachO::load_command>(StringRef(hdr), at);
        if (lc.cmd == MachO::LC_SEGMENT_64) {
          auto seg = readAt<MachO::segment_command_64>(StringRef(hdr), at);
          if (StringRef(seg.segname, strnlen(seg.segname, 16)) == "__LINKEDIT") {
            seg.filesize = end - seg.fileoff;
            seg.vmsize = alignTo(seg.filesize, 0x4000);
            writeAt(hdr, at, seg);
          }
        }
        at += lc.cmdsize;
      }
      // Now the hashes, of the final header too.
      if (!resign(data, edits, data.substr(sigOff, sigSize), sigOff, codeLimit, newSig, err)) {
        err = binary + ": " + err;
        return false;
      }
      while (newSig.size() % 16)
        newSig += '\0';
      if (newSig.size() != cs.datasize) {
        err = binary + ": code signature size changed";
        return false;
      }
    } else {
      // In place: the signature keeps its size.
      if (newSig.size() > sigSize) {
        err = binary + ": code signature grew";
        return false;
      }
      newSig.resize(sigSize, '\0');
    }
    sig = std::move(newSig);
    edits.push_back({codeLimit, std::move(sig)});
  }

  // A new file (not a write in place): the kernel caches code signatures per
  // file.
  sys::fs::file_status st;
  sys::fs::status(binary, st);
  std::string tmp = binary + ".pcln.tmp";
#ifdef __APPLE__
  // A clone shares the blocks of the binary: only the edits are written.
  sys::fs::remove(tmp);
  if (clonefile(binary.c_str(), tmp.c_str(), 0) == 0) {
    int fd = ::open(tmp.c_str(), O_WRONLY);
    bool ok = fd >= 0;
    for (const Edit &e : edits)
      ok = ok && ::pwrite(fd, e.bytes.data(), e.bytes.size(), off_t(e.at)) == ssize_t(e.bytes.size());
    ok = ok && (end == data.size() || ::ftruncate(fd, off_t(end)) == 0);
    if (fd >= 0)
      ok = ::close(fd) == 0 && ok;
    if (ok && !sys::fs::rename(tmp, binary))
      return true;
    sys::fs::remove(tmp);
  }
#endif
  {
    std::error_code ec;
    raw_fd_ostream os(tmp, ec);
    if (ec) {
      err = tmp + ": " + ec.message();
      return false;
    }
    // In chunks, the edits applied.
    std::string chunk;
    for (uint64_t lo = 0; lo < end; lo += 1 << 20) {
      patchedRange(data, edits, lo, std::min<uint64_t>(end, lo + (1 << 20)), chunk);
      os << chunk;
    }
  }
  sys::fs::setPermissions(tmp, st.permissions());
  if (std::error_code ec = sys::fs::rename(tmp, binary)) {
    err = binary + ": " + ec.message();
    return false;
  }
  return true;
}

} // namespace marmot::pcln
