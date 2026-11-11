// The lowering checks that symbols exist and runtime declarations agree.
// RUN: %marmot-opt --go-to-llvm --split-input-file --verify-diagnostics %s
llvm.func @f() -> !llvm.ptr {
  // expected-error @+1 {{symbol @"type:missing" is not an llvm global or function}}
  %p = go.new @"type:missing", i64
  llvm.return %p : !llvm.ptr
}

// -----

llvm.mlir.global external constant @"type:int"() : !llvm.array<56 x i8>
llvm.func @runtime.newobject(i64) -> !llvm.ptr
llvm.func @f() -> !llvm.ptr {
  // expected-error @+1 {{exists with a different type}}
  %p = go.new @"type:int", i64
  llvm.return %p : !llvm.ptr
}

// -----

llvm.func @f() -> !llvm.ptr {
  // expected-error @+1 {{element type must be an LLVM memory type}}
  %p = go.new @"type:int", i1
  llvm.return %p : !llvm.ptr
}
