// A -no-pie ELF binary (image base 0x400000) whose DWARF still describes a
// function the linker removed (at address 0): marmot-pcln leaves it out and
// says so. Before, 0 - base wrapped to 0xffc00000 and the function table was
// no longer sorted, so FuncForPC and runtime.Caller found nothing.
// UNSUPPORTED: arm64
// RUN: %clang --target=x86_64-unknown-linux-gnu -g -O1 -ffunction-sections -c %S/Inputs/pcln_dead.c -o %t.o
// RUN: %clang --target=x86_64-unknown-linux-gnu -nostdlib -static -no-pie -Wl,--gc-sections %t.o -o %t.exe
// RUN: %llvm-dwarfdump --debug-info %t.exe | %FileCheck %s --check-prefix=DWARF
// DWARF: DW_AT_low_pc (0x0000000000000000)
// RUN: %pcln -o %t.pcln %t.exe 2>&1 | %FileCheck %s
// CHECK: 1 functions without an address left out
// RUN: python3 -c "import struct,sys; t=open(sys.argv[1],'rb').read(); nf,te,fo=struct.unpack_from('<I',t,12)[0],struct.unpack_from('<I',t,32)[0],struct.unpack_from('<I',t,36)[0]; s=[struct.unpack_from('<I',t,fo+12*k)[0] for k in range(nf)]; print('sorted', all(a<b for a,b in zip(s,s[1:])), 'inside', all(x<te for x in s), 'end', te < 0x100000)" %t.pcln | %FileCheck %s --check-prefix=TABLE
// TABLE: sorted True inside True end True
