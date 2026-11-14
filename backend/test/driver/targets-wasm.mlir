// The wasm32 target: wasip1/wasm, generic CPU, exception handling (setjmp of
// defer frames), a wasm object.
// REQUIRES: wasm
// RUN: %backend -target wasip1/wasm -emit=llvm -o - %s | %FileCheck %s --check-prefix=WASM
// RUN: %backend -target wasip1/wasm -o %t.o %s
// RUN: %llvm-readobj --file-headers %t.o | %FileCheck %s --check-prefix=WASMOBJ
// WASM: target triple = "wasm32-unknown-wasip1"
// WASM: "target-cpu"="generic"
// WASM-SAME: "target-features"="{{[^"]*}}+exception-handling
// WASMOBJ: Format: WASM
module {
  llvm.func @f(%a: i64) -> i64 {
    llvm.return %a : i64
  }
}
