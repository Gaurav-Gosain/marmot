// RUN: %marmot-opt --go-bce %s | %FileCheck %s
!slice = !llvm.struct<(ptr, i64, i64)>
module {
  llvm.mlir.global external constant @"type:int"() : !llvm.array<56 x i8>

  // B1: constant index below a constant length.
  // CHECK-LABEL: llvm.func @consts
  // CHECK: go.index %arg0[%{{.*}}] len %{{.*}}, i64 nocheck
  // CHECK-NOT: go.bounds_check %{{.*}}, %{{.*}} : i64{{$}}
  // CHECK: go.index %arg0[%{{.*}}] len %{{.*}}, i64 : i64
  llvm.func @consts(%p: !llvm.ptr) {
    %c3 = llvm.mlir.constant(3 : i64) : i64
    %c4 = llvm.mlir.constant(4 : i64) : i64
    %a = go.index %p[%c3] len %c4, i64 : i64
    go.bounds_check %c3, %c4 : i64
    // 4 is not < 4: kept.
    %b = go.index %p[%c4] len %c4, i64 : i64
    llvm.return
  }

  // B2: a dominating check with the same index and length (the length read
  // again from the same slice value counts as the same).
  // CHECK-LABEL: llvm.func @dominated
  // CHECK: go.index %{{.*}}[%arg1] len %{{.*}}, i64 : i64
  // CHECK: go.index %{{.*}}[%arg1] len %{{.*}}, i64 nocheck
  // CHECK: ^bb1:
  // CHECK: go.index %{{.*}}[%arg2] len %{{.*}}, i64 : i64
  llvm.func @dominated(%s: !slice, %i: i64, %j: i64, %c: i1) {
    %p = llvm.extractvalue %s[0] : !slice
    %n = llvm.extractvalue %s[1] : !slice
    %a = go.index %p[%i] len %n, i64 : i64
    %n2 = llvm.extractvalue %s[1] : !slice
    %b = go.index %p[%i] len %n2, i64 : i64
    llvm.cond_br %c, ^x, ^y
  ^x:
    // Not dominated by a check of %j.
    %d = go.index %p[%j] len %n, i64 : i64
    llvm.br ^y
  ^y:
    llvm.return
  }

  // B3: for i := 0; i < len(s); i++ { s[i] }
  // CHECK-LABEL: llvm.func @loop
  // CHECK: go.index %{{.*}}[%[[I:.*]]] len %{{.*}}, i64 nocheck
  // The i+1 index is not covered by the rule.
  // CHECK: go.index %{{.*}} len %{{.*}}, i64 : i64
  llvm.func @loop(%s: !slice) -> i64 {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %p = llvm.extractvalue %s[0] : !slice
    %n = llvm.extractvalue %s[1] : !slice
    llvm.br ^head(%c0, %c0 : i64, i64)
  ^head(%i: i64, %acc: i64):
    %more = llvm.icmp "slt" %i, %n : i64
    llvm.cond_br %more, ^body, ^done
  ^body:
    %a = go.index %p[%i] len %n, i64 : i64
    %v = llvm.load %a : !llvm.ptr -> i64
    %i1 = llvm.add %i, %c1 : i64
    %b = go.index %p[%i1] len %n, i64 : i64
    %acc1 = llvm.add %acc, %v : i64
    llvm.br ^head(%i1, %acc1 : i64, i64)
  ^done:
    llvm.return %acc : i64
  }

  // B3 does not apply: the step is 2, the start may be negative, or the
  // guard compares with another length.
  // CHECK-LABEL: llvm.func @loop_no
  // CHECK: go.index %{{.*}}, i64 : i64
  // CHECK: go.index %{{.*}}, i64 : i64
  // CHECK: go.index %{{.*}}, i64 : i64
  llvm.func @loop_no(%s: !slice, %t: !slice, %start: i64) {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c2 = llvm.mlir.constant(2 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %p = llvm.extractvalue %s[0] : !slice
    %n = llvm.extractvalue %s[1] : !slice
    %m = llvm.extractvalue %t[1] : !slice
    llvm.br ^h1(%c0 : i64)
  ^h1(%i: i64):
    %c = llvm.icmp "slt" %i, %n : i64
    llvm.cond_br %c, ^b1, ^h2(%start : i64)
  ^b1:
    %a = go.index %p[%i] len %n, i64 : i64
    %i2 = llvm.add %i, %c2 : i64
    llvm.br ^h1(%i2 : i64)
  ^h2(%j: i64):
    %c3 = llvm.icmp "slt" %j, %n : i64
    llvm.cond_br %c3, ^b2, ^h3(%c0 : i64)
  ^b2:
    %b = go.index %p[%j] len %n, i64 : i64
    %j1 = llvm.add %j, %c1 : i64
    llvm.br ^h2(%j1 : i64)
  ^h3(%k: i64):
    %c4 = llvm.icmp "slt" %k, %m : i64
    llvm.cond_br %c4, ^b3, ^out
  ^b3:
    %d = go.index %p[%k] len %n, i64 : i64
    %k1 = llvm.add %k, %c1 : i64
    llvm.br ^h3(%k1 : i64)
  ^out:
    llvm.return
  }

  // N1, N2: nil checks of fresh allocations and repeated checks go away.
  // CHECK-LABEL: llvm.func @nils
  // CHECK-NOT: go.nil_check %{{.*}}{{$}}
  // CHECK: go.nil_check %arg0
  // CHECK-NOT: go.nil_check
  // CHECK: llvm.return
  llvm.func @nils(%q: !llvm.ptr) {
    %p = go.new @"type:int", i64
    go.nil_check %p
    go.nil_check %q
    go.nil_check %q
    llvm.return
  }
}
