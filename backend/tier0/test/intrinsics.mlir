module {
  llvm.func @clz64(%a: i64) -> i64 { %f = llvm.mlir.constant(false) : i1
    %r = llvm.call_intrinsic "llvm.ctlz.i64"(%a, %f) : (i64, i1) -> i64
    llvm.return %r : i64 }
  llvm.func @clz8(%a: i8) -> i8 { %f = llvm.mlir.constant(false) : i1
    %r = llvm.call_intrinsic "llvm.ctlz.i8"(%a, %f) : (i8, i1) -> i8
    llvm.return %r : i8 }
  llvm.func @ctz32(%a: i32) -> i32 { %f = llvm.mlir.constant(false) : i1
    %r = llvm.call_intrinsic "llvm.cttz.i32"(%a, %f) : (i32, i1) -> i32
    llvm.return %r : i32 }
  llvm.func @ctz64(%a: i64) -> i64 { %f = llvm.mlir.constant(false) : i1
    %r = llvm.call_intrinsic "llvm.cttz.i64"(%a, %f) : (i64, i1) -> i64
    llvm.return %r : i64 }
  llvm.func @pop64(%a: i64) -> i64 {
    %r = llvm.call_intrinsic "llvm.ctpop.i64"(%a) : (i64) -> i64
    llvm.return %r : i64 }
  llvm.func @bswap32(%a: i32) -> i32 {
    %r = llvm.call_intrinsic "llvm.bswap.i32"(%a) : (i32) -> i32
    llvm.return %r : i32 }
  llvm.func @brev16(%a: i16) -> i16 {
    %r = llvm.call_intrinsic "llvm.bitreverse.i16"(%a) : (i16) -> i16
    llvm.return %r : i16 }
  llvm.func @fshl64(%a: i64, %b: i64, %c: i64) -> i64 {
    %r = llvm.call_intrinsic "llvm.fshl.i64"(%a, %b, %c) : (i64, i64, i64) -> i64
    llvm.return %r : i64 }
  llvm.func @fshl32(%a: i32, %b: i32, %c: i32) -> i32 {
    %r = llvm.call_intrinsic "llvm.fshl.i32"(%a, %b, %c) : (i32, i32, i32) -> i32
    llvm.return %r : i32 }
  llvm.func @fshr8(%a: i8, %b: i8, %c: i8) -> i8 {
    %r = llvm.call_intrinsic "llvm.fshr.i8"(%a, %b, %c) : (i8, i8, i8) -> i8
    llvm.return %r : i8 }
  llvm.func @fshr64(%a: i64, %b: i64, %c: i64) -> i64 {
    %r = llvm.call_intrinsic "llvm.fshr.i64"(%a, %b, %c) : (i64, i64, i64) -> i64
    llvm.return %r : i64 }
  llvm.func @sat(%a: f64) -> i64 {
    %r = llvm.call_intrinsic "llvm.fptosi.sat.i64.f64"(%a) : (f64) -> i64
    llvm.return %r : i64 }
  llvm.func @usat(%a: f64) -> i64 {
    %r = llvm.call_intrinsic "llvm.fptoui.sat.i64.f64"(%a) : (f64) -> i64
    llvm.return %r : i64 }
  llvm.func @fa() -> !llvm.ptr { %z = llvm.mlir.constant(0 : i32) : i32
    %r = llvm.call_intrinsic "llvm.frameaddress.p0"(%z) : (i32) -> !llvm.ptr
    llvm.return %r : !llvm.ptr }
  llvm.func @fmaxi(%a: f64, %b: f64) -> f64 {
    %r = llvm.intr.maximum(%a, %b) : (f64, f64) -> f64
    llvm.return %r : f64 }
  llvm.func @fmini(%a: f32, %b: f32) -> f32 {
    %r = llvm.intr.minimum(%a, %b) : (f32, f32) -> f32
    llvm.return %r : f32 }
  llvm.func @fmaxn(%a: f64, %b: f64) -> f64 {
    %r = llvm.intr.maxnum(%a, %b) : (f64, f64) -> f64
    llvm.return %r : f64 }
}
