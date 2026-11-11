module {
  llvm.func @"t.fill"(%a0: !llvm.struct<(ptr, i64, i64)>, %a1: i64) {
    %v6 = llvm.mlir.constant(0 : i64) : i64
    llvm.br ^bb1(%v6 : i64)
  ^bb1(%v3: i64):
    %v7 = llvm.extractvalue %a0[1] : !llvm.struct<(ptr, i64, i64)>
    %v8 = llvm.icmp "sle" %v3, %v7 : i64
    llvm.cond_br %v8, ^bb2, ^bb3
  ^bb2:
    %v12 = llvm.extractvalue %a0[0] : !llvm.struct<(ptr, i64, i64)>
    %v13 = llvm.extractvalue %a0[1] : !llvm.struct<(ptr, i64, i64)>
    %v14 = go.index %v12[%v3] len %v13, i16 : i64
    %v20 = go.index %v12[%a1] len %v13, i16 : i64
    %v15 = llvm.mlir.constant(1 : i64) : i64
    %v16 = llvm.add %v3, %v15 : i64
    llvm.br ^bb1(%v16 : i64)
  ^bb3:
    llvm.return
  }
}
