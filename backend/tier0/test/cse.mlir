// One value converted to f32 and to f64 in one block: two results (MIR CSE).
module {
  llvm.func @s64both(%a: i64, %p: !llvm.ptr) -> f64 {
    %f = llvm.sitofp %a : i64 to f32
    llvm.store %f, %p : f32, !llvm.ptr
    %d = llvm.sitofp %a : i64 to f64
    llvm.return %d : f64 }
  llvm.func @u32both(%a: i32, %p: !llvm.ptr) -> f64 {
    %f = llvm.uitofp %a : i32 to f32
    llvm.store %f, %p : f32, !llvm.ptr
    %d = llvm.uitofp %a : i32 to f64
    llvm.return %d : f64 }
}
