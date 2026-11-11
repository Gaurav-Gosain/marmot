// Cross targets: ELF objects with portable CPU defaults; Go style names.
// RUN: %backend -target linux/amd64 -emit=llvm -o - %s | %FileCheck %s --check-prefix=AMD64
// RUN: %backend -target linux/arm64 -emit=llvm -o - %s | %FileCheck %s --check-prefix=ARM64
// RUN: %backend -target x86_64-unknown-linux-gnu -mcpu=x86-64-v3 -emit=llvm -o - %s | %FileCheck %s --check-prefix=V3
// RUN: %backend -target linux/arm64 -o %t.o %s
// RUN: %llvm-readobj --file-headers %t.o | %FileCheck %s --check-prefix=ELF
// AMD64: target triple = "x86_64-unknown-linux-gnu"
// AMD64: "target-cpu"="x86-64"
// ARM64: target triple = "aarch64-unknown-linux-gnu"
// ARM64: "target-cpu"="generic"
// V3: "target-cpu"="x86-64-v3"
// ELF: Format: elf64-littleaarch64
module {
  llvm.func @f(%a: i64) -> i64 {
    llvm.return %a : i64
  }
}
