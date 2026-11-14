// ELF.cpp: the ELF64 relocatable form of the object (x86-64), from the same
// symbols, sections and relocations as the Mach-O form (see MachO.h).
#include "MachO.h"

#include <algorithm>
#include <cstring>
#include <unordered_map>

namespace marmot {
namespace tier0 {

namespace {

constexpr uint16_t ET_REL = 1, EM_X86_64 = 62;
constexpr uint32_t SHT_PROGBITS = 1, SHT_SYMTAB = 2, SHT_STRTAB = 3, SHT_RELA = 4, SHT_NOBITS = 8;
constexpr uint64_t SHF_WRITE = 1, SHF_ALLOC = 2, SHF_EXECINSTR = 4, SHF_INFO_LINK = 0x40, SHF_LINK_ORDER = 0x80,
                   SHF_GNU_RETAIN = 0x200000;
constexpr uint8_t STB_LOCAL = 0, STB_GLOBAL = 1, STB_WEAK = 2;
constexpr uint8_t STT_NOTYPE = 0, STT_OBJECT = 1, STT_FUNC = 2;
constexpr uint8_t STV_HIDDEN = 2;
constexpr uint16_t SHN_COMMON = 0xfff2;
// Extended section numbering (one section per function can pass 65280):
// indices from SHN_LORESERVE on go to SHT_SYMTAB_SHNDX (symbols) and to the
// null section header (count, string table index).
constexpr uint32_t SHN_LORESERVE = 0xff00, SHN_XINDEX = 0xffff, SHT_SYMTAB_SHNDX = 18;
enum : uint32_t {
  R_X86_64_64 = 1,
  R_X86_64_PC32 = 2,
  R_X86_64_PLT32 = 4,
  R_X86_64_REX_GOTPCRELX = 42,
};

struct Buf {
  std::string b;
  void u8(uint8_t v) { b.push_back(char(v)); }
  void u16(uint16_t v) { b.append(reinterpret_cast<const char *>(&v), 2); }
  void u32(uint32_t v) { b.append(reinterpret_cast<const char *>(&v), 4); }
  void u64(uint64_t v) { b.append(reinterpret_cast<const char *>(&v), 8); }
  void align(size_t a) {
    while (b.size() % a) b.push_back('\0');
  }
};

struct Strtab {
  std::string s = std::string(1, '\0');
  std::unordered_map<std::string, uint32_t> seen;  // section names repeat
  uint32_t add(const std::string &n, bool share = false) {
    if (share) {
      auto it = seen.find(n);
      if (it != seen.end()) return it->second;
    }
    uint32_t at = uint32_t(s.size());
    s += n;
    s.push_back('\0');
    if (share) seen.emplace(n, at);
    return at;
  }
};

const char *elfSectName(unsigned i) {
  static const char *n[] = {".text", ".data", ".rodata", "marmot_tlinks", ".bss"};
  return n[i];
}

} // namespace

std::string MachOWriter::serializeELF() const {
  // Section header table entries, in order.
  struct Shdr {
    uint32_t name;
    uint32_t type;
    uint64_t flags, offset = 0, size = 0;
    uint32_t link = 0, info = 0;
    uint64_t align = 1, entsize = 0;
    std::string data;
  };
  Strtab shstr, str;
  std::vector<Shdr> sh;
  sh.push_back(Shdr{0, 0, 0});
  // One ELF section per defined symbol (as -ffunction-sections and
  // -fdata-sections), so --gc-sections drops dead code and the undefined
  // symbols only dead code refers to.
  const unsigned B = unsigned(Sect::Bss), TL = unsigned(Sect::TLinks);
  static const uint64_t flags[kNumSect] = {SHF_ALLOC | SHF_EXECINSTR, SHF_ALLOC | SHF_WRITE, SHF_ALLOC,
                                           SHF_ALLOC | SHF_WRITE, SHF_ALLOC | SHF_WRITE};
  struct Chunk {
    uint64_t lo, hi;
    unsigned shndx;
  };
  std::vector<Chunk> chunks[kNumSect];
  for (unsigned i = 0; i < kNumSect; i++) {
    const Section &x = sec_[i];
    uint64_t total = i == B ? x.bssSize : x.data.size();
    if (i == TL && total == 0) continue;
    std::vector<uint64_t> starts, kept;
    for (const Sym &y : syms_)
      if (y.defined && unsigned(y.sect) == i) {
        starts.push_back(y.offset);
        if (y.retain) kept.push_back(y.offset);
      }
    std::sort(starts.begin(), starts.end());
    starts.erase(std::unique(starts.begin(), starts.end()), starts.end());
    if (starts.empty() || starts[0] != 0) starts.insert(starts.begin(), 0);
    for (size_t k = 0; k < starts.size(); k++) {
      uint64_t lo = starts[k], hi = k + 1 < starts.size() ? starts[k + 1] : total;
      if (hi <= lo && !(k == 0 && starts.size() == 1)) continue;
      Shdr h{shstr.add(elfSectName(i), true), i == B ? SHT_NOBITS : SHT_PROGBITS, flags[i]};
      h.align = uint64_t(1) << x.align;
      if (std::find(kept.begin(), kept.end(), lo) != kept.end()) h.flags |= SHF_GNU_RETAIN;
      if (i == B)
        h.size = hi - lo;
      else
        h.data.assign(x.data.begin() + lo, x.data.begin() + hi);
      chunks[i].push_back({lo, hi, unsigned(sh.size())});
      sh.push_back(std::move(h));
    }
  }
  // The chunk holding an offset (a symbol at the end of a section, of size
  // zero, belongs to the last chunk).
  auto chunkOf = [&](unsigned i, uint64_t off) -> const Chunk & {
    const std::vector<Chunk> &c = chunks[i];
    size_t lo = 0, hi = c.size();
    while (hi - lo > 1) {
      size_t mid = (lo + hi) / 2;
      if (c[mid].lo <= off)
        lo = mid;
      else
        hi = mid;
    }
    return c[lo];
  };

  // A type link entry (one pointer in its own chunk) is linked to the
  // section of the descriptor it points to, so --gc-sections keeps it only
  // while that descriptor is live. Anything else in the table is retained.
  {
    std::vector<int> target(chunks[TL].size(), -1);  // -1: none yet, -2: not one defined target
    for (const Rel &x : sec_[TL].rels) {
      size_t k = size_t(&chunkOf(TL, x.offset) - chunks[TL].data());
      const Sym &y = syms_[x.sym];
      int t = y.defined && !y.commonSize && unsigned(y.sect) != TL
                  ? int(chunkOf(unsigned(y.sect), y.offset).shndx)
                  : -2;
      target[k] = target[k] == -1 ? t : -2;
    }
    for (size_t k = 0; k < target.size(); k++) {
      Shdr &h = sh[chunks[TL][k].shndx];
      if (target[k] >= 0 && chunks[TL][k].hi - chunks[TL][k].lo == 8) {
        h.flags = (h.flags | SHF_LINK_ORDER) & ~SHF_GNU_RETAIN;
        h.link = uint32_t(target[k]);
      } else {
        h.flags |= SHF_GNU_RETAIN;
      }
    }
  }

  // Symbols: the null symbol, locals, then globals (sh_info = first global).
  std::vector<unsigned> order;
  for (unsigned i = 0; i < syms_.size(); i++)
    if (syms_[i].defined && !syms_[i].global) order.push_back(i);
  unsigned firstGlobal = unsigned(order.size()) + 1;
  for (unsigned i = 0; i < syms_.size(); i++)
    if (!(syms_[i].defined && !syms_[i].global)) order.push_back(i);
  std::vector<uint32_t> finalIdx(syms_.size());
  Buf symtab;
  symtab.b.append(24, '\0');
  std::vector<uint32_t> xindex(1, 0); // per symbol: its section index when it does not fit st_shndx
  bool needXindex = false;
  for (unsigned k = 0; k < order.size(); k++) {
    const Sym &s = syms_[order[k]];
    finalIdx[order[k]] = k + 1;
    uint8_t bind = !s.defined ? STB_GLOBAL : !s.global ? STB_LOCAL : s.weak ? STB_WEAK : STB_GLOBAL;
    uint8_t type = s.commonSize ? STT_OBJECT : !s.defined ? STT_NOTYPE : s.sect == Sect::Text ? STT_FUNC : STT_OBJECT;
    symtab.u32(str.add(s.name));
    symtab.u8(uint8_t(bind << 4 | type));
    symtab.u8(s.hidden ? STV_HIDDEN : 0);
    if (s.commonSize) {
      symtab.u16(SHN_COMMON);
      symtab.u64(uint64_t(1) << s.commonAlign);
      symtab.u64(s.commonSize);
    } else if (s.defined) {
      const Chunk &c = chunkOf(unsigned(s.sect), s.offset);
      if (c.shndx >= SHN_LORESERVE) {
        symtab.u16(uint16_t(SHN_XINDEX));
        needXindex = true;
      } else {
        symtab.u16(uint16_t(c.shndx));
      }
      symtab.u64(s.offset - c.lo);
      symtab.u64(c.hi - c.lo);
    } else {
      symtab.u16(0);
      symtab.u64(0);
      symtab.u64(0);
    }
    xindex.push_back(!s.commonSize && s.defined ? uint32_t(chunkOf(unsigned(s.sect), s.offset).shndx) : 0);
  }

  // Relocations with explicit addends, one table per chunk. PC-relative
  // fields end their instruction, so the addend is relative to the end of
  // the 4-byte field.
  unsigned symtabIndex = 0;
  std::vector<Shdr> relas;
  for (unsigned i = 0; i < kNumSect; i++) {
    if (i == B) continue;
    std::vector<Buf> per(chunks[i].size());
    for (const Rel &x : sec_[i].rels) {
      uint32_t type = R_X86_64_64;
      int64_t add = x.addend;
      switch (x.kind) {
      case Reloc::Call32: type = R_X86_64_PLT32; add -= 4; break;
      case Reloc::Pc32: type = R_X86_64_PC32; add -= 4; break;
      case Reloc::GotPc32: type = R_X86_64_REX_GOTPCRELX; add -= 4; break;
      case Reloc::Abs64: type = R_X86_64_64; break;
      default: break;  // arm64 kinds never reach an x86-64 object
      }
      size_t k = size_t(&chunkOf(i, x.offset) - chunks[i].data());
      Buf &r = per[k];
      r.u64(x.offset - chunks[i][k].lo);
      r.u64(uint64_t(finalIdx[x.sym]) << 32 | type);
      r.u64(uint64_t(add));
    }
    for (size_t k = 0; k < per.size(); k++) {
      if (per[k].b.empty()) continue;
      Shdr h{shstr.add(std::string(".rela") + elfSectName(i), true), SHT_RELA, SHF_INFO_LINK};
      h.info = chunks[i][k].shndx;
      h.align = 8;
      h.entsize = 24;
      h.data = std::move(per[k].b);
      relas.push_back(std::move(h));
    }
  }
  symtabIndex = unsigned(sh.size() + relas.size());
  for (Shdr &h : relas) {
    h.link = symtabIndex;
    sh.push_back(std::move(h));
  }
  Shdr st{shstr.add(".symtab"), SHT_SYMTAB, 0};
  st.link = symtabIndex + 1;
  st.info = firstGlobal;
  st.align = 8;
  st.entsize = 24;
  st.data = std::move(symtab.b);
  sh.push_back(std::move(st));
  Shdr strh{shstr.add(".strtab"), SHT_STRTAB, 0};
  strh.data = str.s;
  sh.push_back(std::move(strh));
  if (needXindex) {
    Shdr xh{shstr.add(".symtab_shndx"), SHT_SYMTAB_SHNDX, 0};
    xh.link = symtabIndex;
    xh.align = 4;
    xh.entsize = 4;
    Buf xb;
    for (uint32_t v : xindex)
      xb.u32(v >= SHN_LORESERVE ? v : 0);
    xh.data = std::move(xb.b);
    sh.push_back(std::move(xh));
  }
  sh.push_back(Shdr{shstr.add(".note.GNU-stack"), SHT_PROGBITS, 0});
  uint32_t shstrName = shstr.add(".shstrtab");
  Shdr shs{shstrName, SHT_STRTAB, 0};
  shs.data = shstr.s;
  sh.push_back(std::move(shs));

  // Layout: header, section contents, section header table.
  Buf o;
  o.b.append(64, '\0');
  for (Shdr &h : sh) {
    if (h.data.empty()) continue;
    o.align(std::max<uint64_t>(h.align, 1));
    h.offset = o.b.size();
    h.size = h.data.size();
    o.b += h.data;
  }
  o.align(8);
  uint64_t shoff = o.b.size();
  // Counts that do not fit the header's 16 bits: in the null section.
  uint64_t nsect = sh.size(), shstrIdx = sh.size() - 1;  // .shstrtab is last
  if (nsect >= SHN_LORESERVE)
    sh[0].size = nsect;
  if (shstrIdx >= SHN_LORESERVE)
    sh[0].link = uint32_t(shstrIdx);
  for (const Shdr &h : sh) {
    o.u32(h.name);
    o.u32(h.type);
    o.u64(h.flags);
    o.u64(0);  // address
    o.u64(h.offset);
    o.u64(h.size);
    o.u32(h.link);
    o.u32(h.info);
    o.u64(h.align);
    o.u64(h.entsize);
  }
  Buf hd;
  const unsigned char ident[16] = {0x7f, 'E', 'L', 'F', 2, 1, 1, 0};
  hd.b.append(reinterpret_cast<const char *>(ident), 16);
  hd.u16(ET_REL);
  hd.u16(EM_X86_64);
  hd.u32(1);
  hd.u64(0);  // entry
  hd.u64(0);  // phoff
  hd.u64(shoff);
  hd.u32(0);  // flags
  hd.u16(64);
  hd.u16(0);
  hd.u16(0);
  hd.u16(64);
  hd.u16(nsect >= SHN_LORESERVE ? 0 : uint16_t(nsect));
  hd.u16(shstrIdx >= SHN_LORESERVE ? uint16_t(SHN_XINDEX) : uint16_t(shstrIdx));
  std::memcpy(&o.b[0], hd.b.data(), 64);
  return o.b;
}

} // namespace tier0
} // namespace marmot
