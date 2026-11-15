module {
  llvm.func @marmot.card(!llvm.ptr, i1)
  // Stores v to *slot with a card barrier and returns a + b, which stay live
  // across the barrier.
  llvm.func @store_card(%slot: !llvm.ptr, %v: i64, %on: i1, %a: i64, %b: i64) -> i64 {
    %s = llvm.add %a, %b : i64
    llvm.store %v, %slot : i64, !llvm.ptr
    llvm.call @marmot.card(%slot, %on) : (!llvm.ptr, i1) -> ()
    %r = llvm.add %s, %v : i64
    llvm.return %r : i64
  }
}
