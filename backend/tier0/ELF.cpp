// ELF.cpp: the ELF64 relocatable form of the object (x86-64), from the same
// symbols, sections and relocations as the Mach-O form (see MachO.h).
#include "MachO.h"

#include <cstring>

namespace marmot {
namespace tier0 {

namespace {

constexpr uint16_t ET_REL = 1, EM_X86_64 = 62;
constexpr uint32_t SHT_PROGBITS = 1, SHT_SYMTAB = 2, SHT_STRTAB = 3, SHT_RELA = 4, SHT_NOBITS = 8;
constexpr uint64_t SHF_WRITE = 1, SHF_ALLOC = 2, SHF_EXECINSTR = 4, SHF_INFO_LINK = 0x40;
constexpr uint8_t STB_LOCAL = 0, STB_GLOBAL = 1, STB_WEAK = 2;
constexpr uint8_t STT_NOTYPE = 0, STT_OBJECT = 1, STT_FUNC = 2;
constexpr uint8_t STV_HIDDEN = 2;
constexpr uint16_t SHN_COMMON = 0xfff2;
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
  uint32_t add(const std::string &n) {
    uint32_t at = uint32_t(s.size());
    s += n;
    s.push_back('\0');
    return at;
  }
};

const char *elfSectName(unsigned i) {
  static const char *n[] = {".text", ".data", ".rodata", ".bss"};
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
  unsigned secIndex[4];
  static const uint64_t flags[4] = {SHF_ALLOC | SHF_EXECINSTR, SHF_ALLOC | SHF_WRITE, SHF_ALLOC,
                                    SHF_ALLOC | SHF_WRITE};
  for (unsigned i = 0; i < 4; i++) {
    const Section &x = sec_[i];
    Shdr h{shstr.add(elfSectName(i)), i == 3 ? SHT_NOBITS : SHT_PROGBITS, flags[i]};
    h.align = uint64_t(1) << x.align;
    if (i == 3)
      h.size = x.bssSize;
    else
      h.data.assign(x.data.begin(), x.data.end());
    secIndex[i] = unsigned(sh.size());
    sh.push_back(std::move(h));
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
      symtab.u16(uint16_t(secIndex[unsigned(s.sect)]));
      symtab.u64(s.offset);
      symtab.u64(0);
    } else {
      symtab.u16(0);
      symtab.u64(0);
      symtab.u64(0);
    }
  }

  // Relocations with explicit addends. PC-relative fields end their
  // instruction, so the addend is relative to the end of the 4-byte field.
  unsigned symtabIndex = unsigned(sh.size()) + 0;  // fixed below
  std::vector<Shdr> relas;
  for (unsigned i = 0; i < 3; i++) {
    if (sec_[i].rels.empty()) continue;
    Buf r;
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
      r.u64(x.offset);
      r.u64(uint64_t(finalIdx[x.sym]) << 32 | type);
      r.u64(uint64_t(add));
    }
    Shdr h{shstr.add(std::string(".rela") + elfSectName(i)), SHT_RELA, SHF_INFO_LINK};
    h.info = secIndex[i];
    h.align = 8;
    h.entsize = 24;
    h.data = std::move(r.b);
    relas.push_back(std::move(h));
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
  hd.u16(uint16_t(sh.size()));
  hd.u16(uint16_t(sh.size() - 1));  // .shstrtab is last
  std::memcpy(&o.b[0], hd.b.data(), 64);
  return o.b;
}

} // namespace tier0
} // namespace marmot
