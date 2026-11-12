// MachO.cpp: arm64 Mach-O relocatable object writer (see MachO.h).
#include "MachO.h"

#include <cstdio>
#include <cstring>

namespace marmot {
namespace tier0 {

namespace {

// Mach-O constants (from <mach-o/loader.h>, <mach-o/nlist.h>,
// <mach-o/arm64/reloc.h>); spelled out so the writer builds anywhere.
constexpr uint32_t MH_MAGIC_64 = 0xfeedfacf, CPU_TYPE_ARM64 = 0x0100000c, MH_OBJECT = 1,
                   MH_SUBSECTIONS_VIA_SYMBOLS = 0x2000;
constexpr uint32_t LC_SEGMENT_64 = 0x19, LC_SYMTAB = 0x2, LC_DYSYMTAB = 0xb, LC_BUILD_VERSION = 0x32;
constexpr uint32_t S_REGULAR = 0, S_ZEROFILL = 1, S_ATTR_PURE_INSTRUCTIONS = 0x80000000,
                   S_ATTR_SOME_INSTRUCTIONS = 0x400;
constexpr uint8_t N_UNDF = 0, N_EXT = 1, N_SECT = 0xe;
enum : uint32_t {
  ARM64_RELOC_UNSIGNED = 0,
  ARM64_RELOC_BRANCH26 = 2,
  ARM64_RELOC_PAGE21 = 3,
  ARM64_RELOC_PAGEOFF12 = 4,
  ARM64_RELOC_GOT_LOAD_PAGE21 = 5,
  ARM64_RELOC_GOT_LOAD_PAGEOFF12 = 6,
  ARM64_RELOC_ADDEND = 10,
};

struct Out {
  std::string b;
  void u8(uint8_t v) { b.push_back(char(v)); }
  void u16(uint16_t v) { b.append(reinterpret_cast<const char *>(&v), 2); }
  void u32(uint32_t v) { b.append(reinterpret_cast<const char *>(&v), 4); }
  void u64(uint64_t v) { b.append(reinterpret_cast<const char *>(&v), 8); }
  void name16(const char *s) {
    char n[16] = {};
    std::strncpy(n, s, 16);
    b.append(n, 16);
  }
  void pad(size_t to) {
    if (b.size() < to) b.append(to - b.size(), '\0');
  }
};

const char *sectName(unsigned i) {
  static const char *n[] = {"__text", "__data", "__const", "__bss"};
  return n[i];
}
const char *segName(unsigned i) { return i == 0 || i == 2 ? "__TEXT" : "__DATA"; }

} // namespace

unsigned MachOWriter::symbol(const std::string &name) {
  auto it = index_.find(name);
  if (it != index_.end()) return it->second;
  unsigned i = unsigned(syms_.size());
  syms_.push_back(Sym{name});
  index_.emplace(name, i);
  return i;
}

void MachOWriter::define(const std::string &name, Sect sect, uint64_t offset, bool global, bool weak,
                         bool hidden) {
  Sym &s = syms_[symbol(name)];
  s.defined = true;
  s.global = global || weak || hidden;
  s.weak = weak;
  s.hidden = hidden;
  s.sect = sect;
  s.offset = offset;
}

void MachOWriter::common(const std::string &name, uint64_t size, unsigned log2Align) {
  Sym &s = syms_[symbol(name)];
  s.commonSize = size ? size : 1;
  s.commonAlign = log2Align;
}

void MachOWriter::renameShared(const std::string &from, const std::string &to) {
  auto it = index_.find(from);
  if (it == index_.end()) return;
  unsigned i = it->second;
  index_.erase(it);
  index_[to] = i;
  syms_[i].name = to;
  if (syms_[i].defined) syms_[i].global = syms_[i].hidden = true;
}

uint64_t MachOWriter::size(Sect s) const {
  const Section &x = sec_[unsigned(s)];
  return s == Sect::Bss ? x.bssSize : x.data.size();
}

void MachOWriter::align(Sect s, unsigned a) {
  Section &x = sec_[unsigned(s)];
  unsigned log2 = 0;
  while ((1u << log2) < a) log2++;
  setAlign(s, log2);
  if (s == Sect::Bss) {
    x.bssSize = (x.bssSize + a - 1) & ~uint64_t(a - 1);
    return;
  }
  while (x.data.size() % a) x.data.push_back(s == Sect::Text ? 0 : 0);
}

void MachOWriter::reserveBss(uint64_t n, unsigned a) {
  align(Sect::Bss, a);
  sec_[unsigned(Sect::Bss)].bssSize += n;
}

uint64_t MachOWriter::emit32(uint32_t w) {
  auto &d = sec_[0].data;
  uint64_t off = d.size();
  d.insert(d.end(), reinterpret_cast<uint8_t *>(&w), reinterpret_cast<uint8_t *>(&w) + 4);
  setAlign(Sect::Text, 2);
  return off;
}

void MachOWriter::emitData(Sect s, const void *p, size_t n) {
  auto &d = sec_[unsigned(s)].data;
  d.insert(d.end(), static_cast<const uint8_t *>(p), static_cast<const uint8_t *>(p) + n);
}

void MachOWriter::reloc(Sect s, uint64_t offset, Reloc kind, unsigned sym, int64_t addend) {
  sec_[unsigned(s)].rels.push_back(Rel{offset, kind, sym, addend});
}

std::string MachOWriter::serialize() const {
  const unsigned nsect = 4;
  const uint32_t segCmdSize = 72 + 80 * nsect, buildSize = 24, symtabSize = 24, dysymSize = 80;
  const uint32_t sizeofcmds = segCmdSize + buildSize + symtabSize + dysymSize;
  const uint64_t headerEnd = 32 + sizeofcmds;

  // Section addresses (in one segment) and file offsets.
  uint64_t addr[nsect], fileoff[nsect], vmsize = 0, filePos = headerEnd, fileEnd = headerEnd;
  for (unsigned i = 0; i < nsect; i++) {
    uint64_t a = uint64_t(1) << sec_[i].align;
    vmsize = (vmsize + a - 1) & ~(a - 1);
    addr[i] = vmsize;
    vmsize += i == 3 ? sec_[i].bssSize : sec_[i].data.size();
    if (i != 3) {
      filePos = (filePos + a - 1) & ~(a - 1);
      fileoff[i] = filePos;
      filePos += sec_[i].data.size();
      fileEnd = filePos;
    } else {
      fileoff[i] = 0;
    }
  }

  // Symbol order: locals, external definitions, undefined. Final indices
  // are needed by relocations.
  std::vector<unsigned> order;
  for (unsigned pass = 0; pass < 3; pass++)
    for (unsigned i = 0; i < syms_.size(); i++) {
      const Sym &s = syms_[i];
      unsigned kind = !s.defined ? 2 : s.global ? 1 : 0;
      if (kind == pass) order.push_back(i);
    }
  std::vector<unsigned> finalIdx(syms_.size());
  unsigned nlocal = 0, nextdef = 0, nundef = 0;
  for (unsigned k = 0; k < order.size(); k++) {
    finalIdx[order[k]] = k;
    const Sym &s = syms_[order[k]];
    if (!s.defined)
      nundef++;
    else if (s.global)
      nextdef++;
    else
      nlocal++;
  }

  // Relocations, then symbols and strings, after the section data.
  uint64_t relPos = (fileEnd + 7) & ~uint64_t(7);
  uint64_t reloff[nsect];
  uint32_t nreloc[nsect];
  for (unsigned i = 0; i < nsect; i++) {
    uint32_t n = 0;
    for (const Rel &r : sec_[i].rels) {
      bool needAddend = r.addend != 0 && r.kind != Reloc::Abs64;
      n += needAddend ? 2 : 1;
    }
    reloff[i] = n ? relPos : 0;
    nreloc[i] = n;
    relPos += uint64_t(n) * 8;
  }
  uint64_t symoff = (relPos + 7) & ~uint64_t(7);
  std::string strtab(1, '\0');
  std::vector<uint32_t> strx(syms_.size());
  for (unsigned i : order) {
    strx[i] = uint32_t(strtab.size());
    strtab += syms_[i].name;
    strtab.push_back('\0');
  }
  while (strtab.size() % 8) strtab.push_back('\0');
  uint64_t stroff = symoff + uint64_t(order.size()) * 16;

  Out o;
  // mach_header_64
  o.u32(MH_MAGIC_64);
  o.u32(CPU_TYPE_ARM64);
  o.u32(0);
  o.u32(MH_OBJECT);
  o.u32(4);
  o.u32(sizeofcmds);
  o.u32(MH_SUBSECTIONS_VIA_SYMBOLS);
  o.u32(0);
  // LC_SEGMENT_64 (unnamed)
  o.u32(LC_SEGMENT_64);
  o.u32(segCmdSize);
  o.name16("");
  o.u64(0);
  o.u64(vmsize);
  o.u64(headerEnd);
  o.u64(fileEnd - headerEnd);
  o.u32(7);
  o.u32(7);
  o.u32(nsect);
  o.u32(0);
  for (unsigned i = 0; i < nsect; i++) {
    o.name16(sectName(i));
    o.name16(segName(i));
    o.u64(addr[i]);
    o.u64(i == 3 ? sec_[i].bssSize : sec_[i].data.size());
    o.u32(uint32_t(fileoff[i]));
    o.u32(sec_[i].align);
    o.u32(uint32_t(reloff[i]));
    o.u32(nreloc[i]);
    uint32_t flags = i == 0 ? S_REGULAR | S_ATTR_PURE_INSTRUCTIONS | S_ATTR_SOME_INSTRUCTIONS
                     : i == 3 ? S_ZEROFILL
                              : S_REGULAR;
    o.u32(flags);
    o.u32(0);
    o.u32(0);
    o.u32(0);
  }
  // LC_BUILD_VERSION: macOS 11.0, no tools.
  o.u32(LC_BUILD_VERSION);
  o.u32(buildSize);
  o.u32(1);
  o.u32(0x000b0000);
  o.u32(0);
  o.u32(0);
  // LC_SYMTAB
  o.u32(LC_SYMTAB);
  o.u32(symtabSize);
  o.u32(uint32_t(symoff));
  o.u32(uint32_t(order.size()));
  o.u32(uint32_t(stroff));
  o.u32(uint32_t(strtab.size()));
  // LC_DYSYMTAB
  o.u32(LC_DYSYMTAB);
  o.u32(dysymSize);
  o.u32(0);
  o.u32(nlocal);
  o.u32(nlocal);
  o.u32(nextdef);
  o.u32(nlocal + nextdef);
  o.u32(nundef);
  for (int i = 0; i < 12; i++) o.u32(0);

  // Section data.
  for (unsigned i = 0; i < 3; i++) {
    o.pad(fileoff[i]);
    std::vector<uint8_t> d = sec_[i].data;
    // Abs64 addends live in the data.
    for (const Rel &r : sec_[i].rels)
      if (r.kind == Reloc::Abs64) std::memcpy(&d[r.offset], &r.addend, 8);
    o.b.append(reinterpret_cast<const char *>(d.data()), d.size());
  }

  // Relocations.
  for (unsigned i = 0; i < nsect; i++) {
    if (!nreloc[i]) continue;
    o.pad(reloff[i]);
    for (const Rel &r : sec_[i].rels) {
      uint32_t type = 0, pcrel = 0, len = 2;
      switch (r.kind) {
      case Reloc::Branch26: type = ARM64_RELOC_BRANCH26; pcrel = 1; break;
      case Reloc::Page21: type = ARM64_RELOC_PAGE21; pcrel = 1; break;
      case Reloc::PageOff12: type = ARM64_RELOC_PAGEOFF12; break;
      case Reloc::GotPage21: type = ARM64_RELOC_GOT_LOAD_PAGE21; pcrel = 1; break;
      case Reloc::GotPageOff12: type = ARM64_RELOC_GOT_LOAD_PAGEOFF12; break;
      case Reloc::Abs64: type = ARM64_RELOC_UNSIGNED; len = 3; break;
      default: break;  // x86-64 kinds never reach a Mach-O arm64 object
      }
      if (r.addend != 0 && r.kind != Reloc::Abs64) {
        o.u32(uint32_t(r.offset));
        o.u32((uint32_t(r.addend) & 0xFFFFFF) | (0u << 24) | (2u << 25) | (0u << 27) |
              (ARM64_RELOC_ADDEND << 28));
      }
      o.u32(uint32_t(r.offset));
      o.u32((finalIdx[r.sym] & 0xFFFFFF) | (pcrel << 24) | (len << 25) | (1u << 27) | (type << 28));
    }
  }
  // Symbols.
  o.pad(symoff);
  for (unsigned i : order) {
    const Sym &s = syms_[i];
    o.u32(strx[i]);
    if (!s.defined) {
      o.u8(N_UNDF | N_EXT);
      o.u8(0);
      o.u16(uint16_t((s.commonAlign & 0xF) << 8));  // SET_COMM_ALIGN
      o.u64(s.commonSize);
    } else {
      o.u8(N_SECT | (s.global ? N_EXT : 0) | (s.hidden ? 0x10 : 0));  // N_PEXT
      o.u8(uint8_t(unsigned(s.sect) + 1));
      o.u16(s.weak ? 0x80 : 0);  // N_WEAK_DEF
      o.u64(addr[unsigned(s.sect)] + s.offset);
    }
  }
  o.b += strtab;
  return o.b;
}

bool MachOWriter::write(const std::string &path, std::string &err) const {
  std::string bytes = elf_ ? serializeELF() : serialize();
  FILE *f = std::fopen(path.c_str(), "wb");
  if (!f) {
    err = "cannot create " + path;
    return false;
  }
  bool ok = std::fwrite(bytes.data(), 1, bytes.size(), f) == bytes.size();
  ok = (std::fclose(f) == 0) && ok;
  if (!ok) err = "cannot write " + path;
  return ok;
}

} // namespace tier0
} // namespace marmot
