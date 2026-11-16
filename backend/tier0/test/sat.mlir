// Saturating conversions to results narrower than 64 bits (MIR only).
module {
  llvm.func @sat32(%a: f64) -> i32 {
    %r = llvm.call_intrinsic "llvm.fptosi.sat.i32.f64"(%a) : (f64) -> i32
    llvm.return %r : i32 }
  llvm.func @sat32f(%a: f32) -> i32 {
    %r = llvm.call_intrinsic "llvm.fptosi.sat.i32.f32"(%a) : (f32) -> i32
    llvm.return %r : i32 }
  llvm.func @sat8(%a: f64) -> i8 {
    %r = llvm.call_intrinsic "llvm.fptosi.sat.i8.f64"(%a) : (f64) -> i8
    llvm.return %r : i8 }
  llvm.func @usat32(%a: f64) -> i32 {
    %r = llvm.call_intrinsic "llvm.fptoui.sat.i32.f64"(%a) : (f64) -> i32
    llvm.return %r : i32 }
  llvm.func @usat16f(%a: f32) -> i16 {
    %r = llvm.call_intrinsic "llvm.fptoui.sat.i16.f32"(%a) : (f32) -> i16
    llvm.return %r : i16 }
}
