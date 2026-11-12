// Bounds check versioning (Rule V): a loop whose only extra exits are bounds
// checks gets a fast copy without them, guarded by a precondition; the fast
// copy is then parallelized.
// RUN: %backend -O2 -par -loops-all-rules -loops-remarks -emit=llvm -o %t.ll %s 2>&1 | %FileCheck %s --check-prefix=REMARK
// RUN: %backend -O2 -par -loops-remarks -emit=llvm -o %t2.ll %s 2>&1 | %FileCheck %s --check-prefix=GATED
// GATED: loops: fill: {{.*}}bounds checks versioned (1 checks
// GATED: loops: gather: {{.*}}loaded bound (-loops-bcv-loaded)
// RUN: %FileCheck %s --check-prefix=IR < %t.ll

// REMARK: loops: fill: {{.*}}bounds checks versioned (1 checks
// REMARK: loops: fill: {{.*}}parallel, 0 reductions
// REMARK: loops: gather: {{.*}}bounds checks versioned (1 checks, {{.*}}1 check loops)
// REMARK: loops: gather: {{.*}}parallel, 1 reductions
// REMARK: loops: search: {{.*}}not versioned: exit that does not panic
// REMARK-NOT: bcv.load

// IR-LABEL: define void @fill(
// IR: bcv.ok
// IR: call void @rt_parallel_for
// IR: call void @panicIndex(
// IR-LABEL: define i64 @gather(
// IR: bcv.load
// IR: call void @rt_parallel_for

module {
  llvm.func @panicIndex(i64, i64) attributes {passthrough = ["noreturn"]}

  // for i in [0, n): p[i] = i*i, with a bounds check against len.
  llvm.func @fill(%p: !llvm.ptr, %len: i64, %n: i64) {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %z = llvm.icmp "sgt" %n, %c0 : i64
    llvm.cond_br %z, ^loop(%c0 : i64), ^done
  ^loop(%i: i64):
    %ok = llvm.icmp "ult" %i, %len : i64
    llvm.cond_br %ok, ^body, ^panic(%i : i64)
  ^body:
    %a = llvm.getelementptr %p[%i] : (!llvm.ptr, i64) -> !llvm.ptr, i64
    %sq = llvm.mul %i, %i : i64
    llvm.store %sq, %a : i64, !llvm.ptr
    %i2 = llvm.add %i, %c1 : i64
    %c = llvm.icmp "slt" %i2, %n : i64
    llvm.cond_br %c, ^loop(%i2 : i64), ^done
  ^done:
    llvm.return
  ^panic(%x: i64):
    llvm.call @panicIndex(%x, %len) : (i64, i64) -> ()
    llvm.unreachable
  }

  // q[idx[i]]: the checked index is loaded; a check loop computes max(idx[i])
  // before the loop.
  llvm.func @gather(%q: !llvm.ptr, %idx: !llvm.ptr, %len: i64, %n: i64) -> i64 {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %z = llvm.icmp "sgt" %n, %c0 : i64
    llvm.cond_br %z, ^loop(%c0, %c0 : i64, i64), ^done(%c0 : i64)
  ^loop(%i: i64, %s: i64):
    %ia = llvm.getelementptr %idx[%i] : (!llvm.ptr, i64) -> !llvm.ptr, i64
    %j = llvm.load %ia : !llvm.ptr -> i64
    %ok = llvm.icmp "ult" %j, %len : i64
    llvm.cond_br %ok, ^body, ^panic(%j : i64)
  ^body:
    %a = llvm.getelementptr %q[%j] : (!llvm.ptr, i64) -> !llvm.ptr, i64
    %v = llvm.load %a : !llvm.ptr -> i64
    %s2 = llvm.add %s, %v : i64
    %i2 = llvm.add %i, %c1 : i64
    %c = llvm.icmp "slt" %i2, %n : i64
    llvm.cond_br %c, ^loop(%i2, %s2 : i64, i64), ^done(%s2 : i64)
  ^done(%r: i64):
    llvm.return %r : i64
  ^panic(%x: i64):
    llvm.call @panicIndex(%x, %len) : (i64, i64) -> ()
    llvm.unreachable
  }

  // Quicksort's `for a[i] < p { i++ }` after rotation: the compare with p
  // exits to normal code, not to a panic. It is the loop's own condition,
  // not a check: versioning it would scan a[i:n] before every entry.
  llvm.func @search(%p: !llvm.ptr, %n: i64, %x: i64, %out: !llvm.ptr) -> i64 {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %z = llvm.icmp "sgt" %n, %c0 : i64
    llvm.cond_br %z, ^loop(%c0 : i64), ^done(%c0 : i64)
  ^loop(%i: i64):
    %a = llvm.getelementptr %p[%i] : (!llvm.ptr, i64) -> !llvm.ptr, i64
    %v = llvm.load %a : !llvm.ptr -> i64
    %lt = llvm.icmp "slt" %v, %x : i64
    llvm.cond_br %lt, ^next, ^found(%i : i64)
  ^next:
    %i2 = llvm.add %i, %c1 : i64
    %c = llvm.icmp "slt" %i2, %n : i64
    llvm.cond_br %c, ^loop(%i2 : i64), ^done(%n : i64)
  ^found(%k: i64):
    llvm.store %k, %out : i64, !llvm.ptr
    llvm.return %k : i64
  ^done(%r: i64):
    llvm.return %r : i64
  }
}
