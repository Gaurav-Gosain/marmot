module {
  llvm.func @w_add(%a: i64, %b: i64, %c: i64, %d: i64, %p: !llvm.ptr) {
    %c64 = llvm.mlir.constant(64 : i128) : i128
    %a1 = llvm.zext %a : i64 to i128
    %b1 = llvm.zext %b : i64 to i128
    %bh = llvm.shl %b1, %c64 : i128
    %x = llvm.or %a1, %bh : i128
    %c1 = llvm.zext %c : i64 to i128
    %d1 = llvm.sext %d : i64 to i128
    %dh = llvm.shl %d1, %c64 : i128
    %y0 = llvm.or %c1, %dh : i128
    %r = llvm.add %x, %y0 : i128
    %lo = llvm.trunc %r : i128 to i64
    %h = llvm.lshr %r, %c64 : i128
    %hi = llvm.trunc %h : i128 to i64
    %p1 = llvm.getelementptr %p[1] : (!llvm.ptr) -> !llvm.ptr, i64
    llvm.store %lo, %p : i64, !llvm.ptr
    llvm.store %hi, %p1 : i64, !llvm.ptr
    llvm.return }
  llvm.func @w_sub(%a: i64, %b: i64, %c: i64, %d: i64, %p: !llvm.ptr) {
    %c64 = llvm.mlir.constant(64 : i128) : i128
    %a1 = llvm.zext %a : i64 to i128
    %b1 = llvm.zext %b : i64 to i128
    %bh = llvm.shl %b1, %c64 : i128
    %x = llvm.or %a1, %bh : i128
    %c1 = llvm.zext %c : i64 to i128
    %d1 = llvm.sext %d : i64 to i128
    %dh = llvm.shl %d1, %c64 : i128
    %y0 = llvm.or %c1, %dh : i128
    %r = llvm.sub %x, %y0 : i128
    %lo = llvm.trunc %r : i128 to i64
    %h = llvm.lshr %r, %c64 : i128
    %hi = llvm.trunc %h : i128 to i64
    %p1 = llvm.getelementptr %p[1] : (!llvm.ptr) -> !llvm.ptr, i64
    llvm.store %lo, %p : i64, !llvm.ptr
    llvm.store %hi, %p1 : i64, !llvm.ptr
    llvm.return }
  llvm.func @w_mul(%a: i64, %b: i64, %c: i64, %d: i64, %p: !llvm.ptr) {
    %c64 = llvm.mlir.constant(64 : i128) : i128
    %a1 = llvm.zext %a : i64 to i128
    %b1 = llvm.zext %b : i64 to i128
    %bh = llvm.shl %b1, %c64 : i128
    %x = llvm.or %a1, %bh : i128
    %c1 = llvm.zext %c : i64 to i128
    %d1 = llvm.sext %d : i64 to i128
    %dh = llvm.shl %d1, %c64 : i128
    %y0 = llvm.or %c1, %dh : i128
    %r = llvm.mul %x, %y0 : i128
    %lo = llvm.trunc %r : i128 to i64
    %h = llvm.lshr %r, %c64 : i128
    %hi = llvm.trunc %h : i128 to i64
    %p1 = llvm.getelementptr %p[1] : (!llvm.ptr) -> !llvm.ptr, i64
    llvm.store %lo, %p : i64, !llvm.ptr
    llvm.store %hi, %p1 : i64, !llvm.ptr
    llvm.return }
  llvm.func @w_and(%a: i64, %b: i64, %c: i64, %d: i64, %p: !llvm.ptr) {
    %c64 = llvm.mlir.constant(64 : i128) : i128
    %a1 = llvm.zext %a : i64 to i128
    %b1 = llvm.zext %b : i64 to i128
    %bh = llvm.shl %b1, %c64 : i128
    %x = llvm.or %a1, %bh : i128
    %c1 = llvm.zext %c : i64 to i128
    %d1 = llvm.sext %d : i64 to i128
    %dh = llvm.shl %d1, %c64 : i128
    %y0 = llvm.or %c1, %dh : i128
    %r = llvm.and %x, %y0 : i128
    %lo = llvm.trunc %r : i128 to i64
    %h = llvm.lshr %r, %c64 : i128
    %hi = llvm.trunc %h : i128 to i64
    %p1 = llvm.getelementptr %p[1] : (!llvm.ptr) -> !llvm.ptr, i64
    llvm.store %lo, %p : i64, !llvm.ptr
    llvm.store %hi, %p1 : i64, !llvm.ptr
    llvm.return }
  llvm.func @w_or(%a: i64, %b: i64, %c: i64, %d: i64, %p: !llvm.ptr) {
    %c64 = llvm.mlir.constant(64 : i128) : i128
    %a1 = llvm.zext %a : i64 to i128
    %b1 = llvm.zext %b : i64 to i128
    %bh = llvm.shl %b1, %c64 : i128
    %x = llvm.or %a1, %bh : i128
    %c1 = llvm.zext %c : i64 to i128
    %d1 = llvm.sext %d : i64 to i128
    %dh = llvm.shl %d1, %c64 : i128
    %y0 = llvm.or %c1, %dh : i128
    %r = llvm.or %x, %y0 : i128
    %lo = llvm.trunc %r : i128 to i64
    %h = llvm.lshr %r, %c64 : i128
    %hi = llvm.trunc %h : i128 to i64
    %p1 = llvm.getelementptr %p[1] : (!llvm.ptr) -> !llvm.ptr, i64
    llvm.store %lo, %p : i64, !llvm.ptr
    llvm.store %hi, %p1 : i64, !llvm.ptr
    llvm.return }
  llvm.func @w_xor(%a: i64, %b: i64, %c: i64, %d: i64, %p: !llvm.ptr) {
    %c64 = llvm.mlir.constant(64 : i128) : i128
    %a1 = llvm.zext %a : i64 to i128
    %b1 = llvm.zext %b : i64 to i128
    %bh = llvm.shl %b1, %c64 : i128
    %x = llvm.or %a1, %bh : i128
    %c1 = llvm.zext %c : i64 to i128
    %d1 = llvm.sext %d : i64 to i128
    %dh = llvm.shl %d1, %c64 : i128
    %y0 = llvm.or %c1, %dh : i128
    %r = llvm.xor %x, %y0 : i128
    %lo = llvm.trunc %r : i128 to i64
    %h = llvm.lshr %r, %c64 : i128
    %hi = llvm.trunc %h : i128 to i64
    %p1 = llvm.getelementptr %p[1] : (!llvm.ptr) -> !llvm.ptr, i64
    llvm.store %lo, %p : i64, !llvm.ptr
    llvm.store %hi, %p1 : i64, !llvm.ptr
    llvm.return }
  llvm.func @w_lshr(%a: i64, %b: i64, %c: i64, %d: i64, %p: !llvm.ptr) {
    %c64 = llvm.mlir.constant(64 : i128) : i128
    %a1 = llvm.zext %a : i64 to i128
    %b1 = llvm.zext %b : i64 to i128
    %bh = llvm.shl %b1, %c64 : i128
    %x = llvm.or %a1, %bh : i128
    %c1 = llvm.zext %c : i64 to i128
    %d1 = llvm.sext %d : i64 to i128
    %dh = llvm.shl %d1, %c64 : i128
    %y0 = llvm.or %c1, %dh : i128
    %r = llvm.lshr %x, %y0 : i128
    %lo = llvm.trunc %r : i128 to i64
    %h = llvm.lshr %r, %c64 : i128
    %hi = llvm.trunc %h : i128 to i64
    %p1 = llvm.getelementptr %p[1] : (!llvm.ptr) -> !llvm.ptr, i64
    llvm.store %lo, %p : i64, !llvm.ptr
    llvm.store %hi, %p1 : i64, !llvm.ptr
    llvm.return }
  llvm.func @w_ashr(%a: i64, %b: i64, %c: i64, %d: i64, %p: !llvm.ptr) {
    %c64 = llvm.mlir.constant(64 : i128) : i128
    %a1 = llvm.zext %a : i64 to i128
    %b1 = llvm.zext %b : i64 to i128
    %bh = llvm.shl %b1, %c64 : i128
    %x = llvm.or %a1, %bh : i128
    %c1 = llvm.zext %c : i64 to i128
    %d1 = llvm.sext %d : i64 to i128
    %dh = llvm.shl %d1, %c64 : i128
    %y0 = llvm.or %c1, %dh : i128
    %r = llvm.ashr %x, %y0 : i128
    %lo = llvm.trunc %r : i128 to i64
    %h = llvm.lshr %r, %c64 : i128
    %hi = llvm.trunc %h : i128 to i64
    %p1 = llvm.getelementptr %p[1] : (!llvm.ptr) -> !llvm.ptr, i64
    llvm.store %lo, %p : i64, !llvm.ptr
    llvm.store %hi, %p1 : i64, !llvm.ptr
    llvm.return }
  llvm.func @w_shl(%a: i64, %b: i64, %c: i64, %d: i64, %p: !llvm.ptr) {
    %c64 = llvm.mlir.constant(64 : i128) : i128
    %a1 = llvm.zext %a : i64 to i128
    %b1 = llvm.zext %b : i64 to i128
    %bh = llvm.shl %b1, %c64 : i128
    %x = llvm.or %a1, %bh : i128
    %c1 = llvm.zext %c : i64 to i128
    %d1 = llvm.sext %d : i64 to i128
    %dh = llvm.shl %d1, %c64 : i128
    %y0 = llvm.or %c1, %dh : i128
    %r = llvm.shl %x, %y0 : i128
    %lo = llvm.trunc %r : i128 to i64
    %h = llvm.lshr %r, %c64 : i128
    %hi = llvm.trunc %h : i128 to i64
    %p1 = llvm.getelementptr %p[1] : (!llvm.ptr) -> !llvm.ptr, i64
    llvm.store %lo, %p : i64, !llvm.ptr
    llvm.store %hi, %p1 : i64, !llvm.ptr
    llvm.return }
  llvm.func @w_k(%p: !llvm.ptr) {
    %k = llvm.mlir.constant(-170141183460469231731687303715884105727 : i128) : i128
    %c64 = llvm.mlir.constant(64 : i128) : i128
    %lo = llvm.trunc %k : i128 to i64
    %h = llvm.ashr %k, %c64 : i128
    %hi = llvm.trunc %h : i128 to i64
    %p1 = llvm.getelementptr %p[1] : (!llvm.ptr) -> !llvm.ptr, i64
    llvm.store %lo, %p : i64, !llvm.ptr
    llvm.store %hi, %p1 : i64, !llvm.ptr
    llvm.return }
}