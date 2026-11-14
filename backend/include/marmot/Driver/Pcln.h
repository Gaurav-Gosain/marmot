// Pcln.h: the compact pc table. Reading DWARF line tables and
// inlined calls, per-object fragments, encoding, and writing the table into a
// linked binary.
#ifndef MARMOT_DRIVER_PCLN_H
#define MARMOT_DRIVER_PCLN_H

#include "llvm/ADT/StringRef.h"
#include "llvm/Support/MemoryBufferRef.h"

#include <cstdint>
#include <functional>
#include <map>
#include <string>
#include <tuple>
#include <vector>

namespace llvm::object {
class ObjectFile;
}

namespace marmot::pcln {

// An inlined call: the callee's name, the call site, and the code ranges of
// the inlined body (DW_TAG_inlined_subroutine).
struct Inline {
  int parent; // -1: the function itself
  unsigned depth;
  std::string name;
  uint32_t callFile, callLine;
  std::vector<std::pair<uint64_t, uint64_t>> ranges;
};

// A stack map: nwords words below the frame pointer; bit i of bits (byte
// i/8, LSB first) set = the word at fp-8*(i+1) is scanned.
struct StackMap {
  uint32_t nwords = 0;
  std::string bits;
};

struct Func {
  uint64_t lo = 0, hi = 0;
  std::string name;
  std::vector<Inline> inl;
  std::vector<std::tuple<uint64_t, uint32_t, uint32_t>> rows; // pc, line, file index
  // Stack maps (version 4) and the call sites that use them: return pc
  // offset from lo, map index. Frames suspended elsewhere stay conservative.
  std::vector<StackMap> maps;
  std::vector<std::pair<uint64_t, uint32_t>> sites;
};

// Functions by start address, and the file names their rows refer to.
struct Table {
  std::map<uint64_t, Func> funcs;
  std::vector<std::string> files;
  std::map<std::string, uint32_t> fileIndex;
  // Rows outside every DWARF subprogram: ELF line-tables-only debug info has
  // no subprograms at all (LLVM 22, as clang -gline-tables-only); they go to
  // the function symbols in addTextSymbols.
  std::vector<std::tuple<uint64_t, uint32_t, uint32_t>> orphans;
  uint32_t fileId(const std::string &f);
};

// Reads the functions of a linked binary or dSYM from its DWARF (inlined
// calls with version 3).
void readDwarf(const llvm::object::ObjectFile &obj, unsigned version, Table &t);

// Adds every text symbol of obj that does not start inside a function of t as
// a function with a name and no lines (C and assembly code), so the range of
// the function before it ends there.
void addTextSymbols(const llvm::object::ObjectFile &obj, Table &t);
// The same from text symbols already read: address -> name, section end.
void addTextSymbols(std::map<uint64_t, std::pair<std::string, uint64_t>> &syms, Table &t);

// A fragment: the functions of one relocatable object with pcs relative to
// their own start, keyed by symbol name. The
// backend writes one next to each object; the linker step turns fragments
// into a table without DWARF processing.
bool makeFragment(llvm::MemoryBufferRef object, unsigned version, std::string &out,
                  std::string &err);
// Adds the functions of a fragment whose symbols addr finds (others were
// dead stripped). addr returns 0 for an unknown name.
bool addFragment(llvm::StringRef fragment, const std::function<uint64_t(llvm::StringRef)> &addr,
                 Table &t, std::string &err);

// A fragment read but not placed: pcs are offsets in their function, files
// are indices into files. parseFragment needs no addresses (it can run
// before the link ends, on many threads); addParsed then places it as
// addFragment does.
struct Fragment {
  struct Fn {
    std::string name;
    uint64_t size = 0;
    std::vector<std::tuple<uint64_t, uint32_t, uint32_t>> rows;
    std::vector<Inline> inl;
    std::vector<StackMap> maps;                       // "M nwords hexbits"
    std::vector<std::pair<uint64_t, uint32_t>> sites; // "S retoff map"
  };
  std::vector<std::string> files;
  std::vector<Fn> funcs;
};
bool parseFragment(llvm::StringRef fragment, Fragment &out, std::string &err);
void addParsed(Fragment &f, const std::function<uint64_t(llvm::StringRef)> &addr, Table &t);

// Encodes t (version 2 or 3). base: image base; quantum: 4 on arm64, 1 on
// x86-64. rename maps function names (main package), may be empty.
std::string encode(Table &t, uint64_t base, uint32_t quantum, unsigned version,
                   const std::function<std::string(const std::string &)> &rename);

// Image base: Mach-O __TEXT vmaddr; ELF lowest loadable address.
uint64_t imageBase(const llvm::object::ObjectFile &obj);

// Writes table into the __TEXT,__marmot_pcln section of a linked Mach-O
// binary in place (the section must be at least as large; the rest is zero
// filled) and updates the hashes of the changed pages in its ad hoc code
// signature. With stripLocals it also drops the local symbols from the
// symbol table (as linking with -x would; the link keeps them for the
// table's function addresses) when the layout allows, and sets
// *droppedLocals to how many.
bool patchMachO(const std::string &binary, llvm::StringRef table, std::string &err,
                bool stripLocals = false, uint32_t *droppedLocals = nullptr);

} // namespace marmot::pcln

#endif
