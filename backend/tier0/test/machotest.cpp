// machotest: writes an object with hand-encoded functions and data through
// the Mach-O writer, for a C driver to link against and call. Covers
// arithmetic, a frame record and a call to an undefined function (BRANCH26),
// ADRP/ADD to a defined data symbol with an addend (PAGE21/PAGEOFF12 with
// ADDEND), a load through the GOT of an undefined data symbol, and a data
// pointer with an addend (UNSIGNED).
#include "../Arm64.h"
#include "../MachO.h"

#include <cstdio>
#include <string>

using namespace marmot::tier0;
using namespace marmot::tier0::a64;

int main(int argc, char **argv) {
  if (argc != 2) {
    std::fprintf(stderr, "usage: machotest out.o\n");
    return 2;
  }
  MachOWriter w;

  // int64 t0_add(int64 a, int64 b) { return a + b; }
  w.define("_t0_add", Sect::Text, w.size(Sect::Text), true);
  w.emit32(addReg(0, 0, 1));
  w.emit32(ret());

  // int64 t0_call(int64 x) { return ext_twice(x) + 1; }
  w.define("_t0_call", Sect::Text, w.size(Sect::Text), true);
  w.emit32(stpPre(FP, LR, SP, -16));
  w.emit32(movSp(FP, SP));
  uint64_t at = w.emit32(bl(0));
  w.reloc(Sect::Text, at, Reloc::Branch26, w.symbol("_ext_twice"));
  w.emit32(addImm(0, 0, 1));
  w.emit32(ldpPost(FP, LR, SP, 16));
  w.emit32(ret());

  // int64 t0_second(void) { return t0_data[1]; } via ADRP/ADD with addend 8.
  w.define("_t0_second", Sect::Text, w.size(Sect::Text), true);
  unsigned data = w.symbol("_t0_data");
  at = w.emit32(adrp(8));
  w.reloc(Sect::Text, at, Reloc::Page21, data, 8);
  at = w.emit32(addImm(8, 8, 0));
  w.reloc(Sect::Text, at, Reloc::PageOff12, data, 8);
  w.emit32(ldrImm(0, 8, 0, 3));
  w.emit32(ret());

  // int64 t0_ext(void) { return ext_var; } through the GOT.
  w.define("_t0_ext", Sect::Text, w.size(Sect::Text), true);
  unsigned ev = w.symbol("_ext_var");
  at = w.emit32(adrp(8));
  w.reloc(Sect::Text, at, Reloc::GotPage21, ev);
  at = w.emit32(ldrImm(8, 8, 0, 3));
  w.reloc(Sect::Text, at, Reloc::GotPageOff12, ev);
  w.emit32(ldrImm(0, 8, 0, 3));
  w.emit32(ret());

  // int64 t0_data[2] = {42, 4242}; int64 *t0_ptr = &t0_data[1];
  w.align(Sect::Data, 8);
  w.define("_t0_data", Sect::Data, w.size(Sect::Data), true);
  int64_t vals[2] = {42, 4242};
  w.emitData(Sect::Data, vals, sizeof vals);
  w.define("_t0_ptr", Sect::Data, w.size(Sect::Data), true);
  uint64_t zero = 0;
  uint64_t poff = w.size(Sect::Data);
  w.emitData(Sect::Data, &zero, 8);
  w.reloc(Sect::Data, poff, Reloc::Abs64, data, 8);

  // A local helper and zero fill.
  w.define("_t0_local", Sect::Text, w.size(Sect::Text), false);
  w.emit32(ret());
  w.reserveBss(64, 8);
  w.define("_t0_bss", Sect::Bss, 0, true);

  std::string err;
  if (!w.write(argv[1], err)) {
    std::fprintf(stderr, "%s\n", err.c_str());
    return 1;
  }
  return 0;
}
