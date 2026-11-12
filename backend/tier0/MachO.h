// MachO.h: a minimal writer for arm64 Mach-O relocatable objects
// (MH_OBJECT), enough for tier0: code, data, read-only data and zero fill
// sections, local / external / undefined symbols, and the relocations the
// code generator needs.
#ifndef MARMOT_TIER0_MACHO_H
#define MARMOT_TIER0_MACHO_H

#include <cstdint>
#include <string>
#include <unordered_map>
#include <vector>

namespace marmot {
namespace tier0 {

enum class Sect : uint8_t { Text, Data, Const, Bss };

enum class Reloc : uint8_t {
  Branch26,       // BL/B to a symbol
  Page21,         // ADRP of a symbol's page
  PageOff12,      // ADD/LDR/STR low 12 bits of a symbol's address (scaled by the access size)
  GotPage21,      // ADRP of the GOT slot's page
  GotPageOff12,   // LDR of the GOT slot
  Abs64,          // 64-bit pointer in data
};

class MachOWriter {
public:
  // Symbols. A name is defined at most once; undefined names are imports.
  // Returns the symbol index.
  unsigned symbol(const std::string &name);
  // Defines name at offset in sect. global: visible to other objects.
  // weak: may be defined in several objects (linkonce_odr / weak_odr), the
  // linker keeps one.
  // hidden: global but not exported from the linked image (private extern).
  void define(const std::string &name, Sect sect, uint64_t offset, bool global, bool weak = false,
              bool hidden = false);

  // A common symbol (tentative definition): the linker merges it with a real
  // definition elsewhere, or allocates size zero bytes.
  void common(const std::string &name, uint64_t size, unsigned log2Align);

  // Renames a symbol; a defined one becomes a hidden global, so another object
  // of the same image can reach it.
  void renameShared(const std::string &from, const std::string &to);
  const std::string &name(unsigned sym) const { return syms_[sym].name; }

  // Section contents. Code is appended as instruction words.
  std::vector<uint8_t> &bytes(Sect s) { return sec_[unsigned(s)].data; }
  uint64_t size(Sect s) const;
  void reserveBss(uint64_t n, unsigned align);
  void align(Sect s, unsigned align);
  void setAlign(Sect s, unsigned log2) {
    if (log2 > sec_[unsigned(s)].align) sec_[unsigned(s)].align = log2;
  }
  uint64_t emit32(uint32_t word);  // appends to text, returns its offset
  void emitData(Sect s, const void *p, size_t n);

  // A relocation at offset in sect against symbol sym with addend.
  void reloc(Sect s, uint64_t offset, Reloc kind, unsigned sym, int64_t addend = 0);

  // Text size and relocation count, to drop a function that turned out
  // unsupported (symbols it referenced stay, undefined and unused).
  struct Checkpoint {
    uint64_t text;
    size_t rels;
  };
  Checkpoint checkpoint() const { return {sec_[0].data.size(), sec_[0].rels.size()}; }
  void rollback(Checkpoint c) {
    sec_[0].data.resize(c.text);
    sec_[0].rels.resize(c.rels);
  }

  // Writes the object. False with err set on failure.
  bool write(const std::string &path, std::string &err) const;
  std::string serialize() const;

private:
  struct Sym {
    std::string name;
    bool defined = false, global = false, weak = false, hidden = false;
    uint64_t commonSize = 0;  // nonzero for a common symbol (left undefined)
    unsigned commonAlign = 0;
    Sect sect = Sect::Text;
    uint64_t offset = 0;
  };
  struct Rel {
    uint64_t offset;
    Reloc kind;
    unsigned sym;
    int64_t addend;
  };
  struct Section {
    std::vector<uint8_t> data;  // empty for Bss
    uint64_t bssSize = 0;
    unsigned align = 0;         // log2
    std::vector<Rel> rels;
  };
  std::vector<Sym> syms_;
  std::unordered_map<std::string, unsigned> index_;
  Section sec_[4];
};

} // namespace tier0
} // namespace marmot

#endif
