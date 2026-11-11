module {
  llvm.func @"t.f"(%a0: !llvm.ptr, %a1: !llvm.ptr, %a2: i1) {
    llvm.cond_br %a2, ^bb1, ^bb2
  ^bb1:
    go.nil_check %a0
    llvm.br ^bb2
  ^bb2:
    go.nil_check %a0
    go.nil_check %a1
    llvm.return
  }
}
