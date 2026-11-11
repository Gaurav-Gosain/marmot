// RUN: %marmot-opt --split-input-file --verify-diagnostics %s
llvm.func @f(%p: !llvm.ptr, %lo: i64, %hi: i64) -> !llvm.struct<(ptr, i64, i64)> {
  // expected-error @+1 {{result must be !llvm.struct<(ptr, iN, iN)> with max}}
  %s = go.slice %p[%lo : %hi] cap %hi, i8 : i64 -> !llvm.struct<(ptr, i64, i64)>
  llvm.return %s : !llvm.struct<(ptr, i64, i64)>
}

// -----

llvm.func @f(%p: !llvm.ptr, %i: i64, %n: i32) {
  // expected-error @+1 {{all of {index, len} have same type}}
  "go.bounds_check"(%i, %n) : (i64, i32) -> ()
  llvm.return
}
