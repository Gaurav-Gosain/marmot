// wasm: the calls after a setjmp are wrapped in try/catch of __c_longjmp
// (the wasm exception model), so a longjmp from a recovered panic returns
// to the setjmp.
// REQUIRES: wasm
// RUN: %backend -target wasip1/wasm -emit=asm -o - %s | %FileCheck %s
// CHECK-LABEL: f:
// CHECK: call __wasm_setjmp
// CHECK: try
// CHECK: call g
// CHECK: catch __c_longjmp
module {
  llvm.func @setjmp(!llvm.ptr) -> i32 attributes {passthrough = ["returns_twice"]}
  llvm.func @g()
  llvm.func @f(%buf: !llvm.ptr) -> i32 {
    %r = llvm.call @setjmp(%buf) : (!llvm.ptr) -> i32
    llvm.call @g() : () -> ()
    llvm.return %r : i32
  }
}
