// RUN: %marmot-opt --split-input-file --verify-diagnostics %s
llvm.func @f(%a: !llvm.struct<(ptr, i64)>) -> !llvm.struct<(ptr, i64)> {
  // expected-error @+1 {{needs at least two operands}}
  %r = go.str_concat %a
  llvm.return %r : !llvm.struct<(ptr, i64)>
}
