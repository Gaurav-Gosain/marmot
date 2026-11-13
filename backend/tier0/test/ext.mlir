// llvm.signext on narrow integer parameters and results (ext_main.c): the
// caller, or for a result the callee, extends the value by its sign.
module {
  llvm.func @take8(i8 {llvm.signext}) -> i64
  llvm.func @take16(i16 {llvm.signext}) -> i64
  llvm.func @takeu8(i8 {llvm.zeroext}) -> i64
  llvm.func @call8(%a: i8) -> i64 {
    %0 = llvm.call @take8(%a) : (i8) -> i64
    llvm.return %0 : i64
  }
  llvm.func @call16() -> i64 {
    %c = llvm.mlir.constant(-2 : i16) : i16
    %0 = llvm.call @take16(%c) : (i16) -> i64
    llvm.return %0 : i64
  }
  llvm.func @callu8() -> i64 {
    %c = llvm.mlir.constant(-1 : i8) : i8
    %0 = llvm.call @takeu8(%c) : (i8) -> i64
    llvm.return %0 : i64
  }
  llvm.func @neg8(%a: i8) -> (i8 {llvm.signext}) {
    %c = llvm.mlir.constant(1 : i8) : i8
    %0 = llvm.sub %a, %c : i8
    llvm.return %0 : i8
  }
}
