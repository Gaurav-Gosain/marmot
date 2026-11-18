// Rule V, cached check loops (versioning-only mode, MARMOT_LOOPS_VERSION=1).
// A row length len(rows[k]) loaded in the inner loop gets a check loop at the
// inner loop's guard, as before, but it runs only on the first entry in a run
// of the outer loop; later entries reuse its minimum (the done flag). The
// check loop still runs only after the guard's scalar conditions hold, so its
// reads are where the original loop's reads are.
// RUN: env MARMOT_LOOPS_VERSION=1 %backend -O2 -verify-loop-info -loops-remarks -emit=llvm -o %t.ll %s 2>&1 | %FileCheck %s --check-prefix=REMARK
// RUN: %FileCheck %s --check-prefix=CACHE < %t.ll
// Without the switch and without -par no loop is versioned.
// RUN: env -u MARMOT_LOOPS_VERSION %backend -O2 -emit=llvm -o %t2.ll %s
// RUN: %FileCheck %s --check-prefix=OFF < %t2.ll

// REMARK-DAG: loops: colsum: loop {{.*}}: versioned (version-only mode)
// REMARK-DAG: loops: colsumw: loop {{.*}}: not versioned: loaded bound check loop cannot be cached outside the loop
// REMARK-DAG: loops: colsumif: loop {{.*}}: versioned (version-only mode)

// done is false on entry to the outer loop; the guard (k < nrows over k in
// [0, 63]) leads to bcv.cache, which runs the check loop only when done is
// false.
// CACHE-LABEL: define i64 @colsum(
// CACHE: [[G:%[0-9]+]] = icmp sgt i64 %1, 63
// CACHE: %bcv.done.0 = phi i1 [ false,
// CACHE: br i1 [[G]], label %bcv.cache, label %.slow.pre
// CACHE: bcv.cache:
// CACHE-NEXT: br i1 %bcv.done.0, label %bcv.use, label %bcv.load
// CACHE: bcv.load:
// CACHE: bcv.use:
// CACHE: call void @panicIndex(
// CACHE-LABEL: define i64 @colsumw(
// CACHE-NOT: bcv.cache
// CACHE: call void @panicIndex(
// The bound of the row index comes from an if around the loops (no k < nrows
// check in the inner loop). The check loop must not run before that if, nor
// when the outer loop runs zero times: it stays behind bcv.cache inside the
// outer loop.
// CACHE-LABEL: define i64 @colsumif(
// CACHE: icmp sgt i64 %1, 63
// CACHE: br i1 {{.*}}, label %bcv.cache, label %.loopexit
// CACHE: bcv.cache:
// CACHE: %bcv.done.0 = phi i1
// CACHE: br i1 %bcv.done.0, label %bcv.use, label %bcv.load
// OFF-NOT: bcv.

!row = !llvm.struct<(ptr, i64, i64)>
module {
  llvm.func @panicIndex(i64, i64) attributes {passthrough = ["noreturn"]}

  // sum over j < n of sum over k < 64 of rows[k][j]
  llvm.func @colsum(%rows: !llvm.ptr, %nrows: i64, %n: i64) -> i64 {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %c64 = llvm.mlir.constant(64 : i64) : i64
    %z = llvm.icmp "sgt" %n, %c0 : i64
    llvm.cond_br %z, ^outer(%c0, %c0 : i64, i64), ^done(%c0 : i64)
  ^outer(%j: i64, %acc: i64):
    llvm.br ^inner(%c0, %acc : i64, i64)
  ^inner(%k: i64, %s: i64):
    %okk = llvm.icmp "ult" %k, %nrows : i64
    llvm.cond_br %okk, ^b1, ^panic(%k, %nrows : i64, i64)
  ^b1:
    %lenp = llvm.getelementptr %rows[%k, 1] : (!llvm.ptr, i64) -> !llvm.ptr, !row
    %len = llvm.load %lenp : !llvm.ptr -> i64
    %okj = llvm.icmp "ult" %j, %len : i64
    llvm.cond_br %okj, ^b2, ^panic(%j, %len : i64, i64)
  ^b2:
    %pp = llvm.getelementptr %rows[%k, 0] : (!llvm.ptr, i64) -> !llvm.ptr, !row
    %ptr = llvm.load %pp : !llvm.ptr -> !llvm.ptr
    %ep = llvm.getelementptr %ptr[%j] : (!llvm.ptr, i64) -> !llvm.ptr, i64
    %v = llvm.load %ep : !llvm.ptr -> i64
    %s2 = llvm.add %s, %v : i64
    %k2 = llvm.add %k, %c1 : i64
    %c = llvm.icmp "slt" %k2, %c64 : i64
    llvm.cond_br %c, ^inner(%k2, %s2 : i64, i64), ^latch(%s2 : i64)
  ^latch(%t: i64):
    %j2 = llvm.add %j, %c1 : i64
    %cj = llvm.icmp "slt" %j2, %n : i64
    llvm.cond_br %cj, ^outer(%j2, %t : i64, i64), ^done(%t : i64)
  ^done(%r: i64):
    llvm.return %r : i64
  ^panic(%x: i64, %y: i64):
    llvm.call @panicIndex(%x, %y) : (i64, i64) -> ()
    llvm.unreachable
  }

  // The same, but the outer loop writes a row length: the check loop cannot
  // move out of the inner loop, so the inner loop is not versioned.
  llvm.func @colsumw(%rows: !llvm.ptr, %nrows: i64, %n: i64) -> i64 {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %c64 = llvm.mlir.constant(64 : i64) : i64
    %z = llvm.icmp "sgt" %n, %c0 : i64
    llvm.cond_br %z, ^outer(%c0, %c0 : i64, i64), ^done(%c0 : i64)
  ^outer(%j: i64, %acc: i64):
    llvm.br ^inner(%c0, %acc : i64, i64)
  ^inner(%k: i64, %s: i64):
    %okk = llvm.icmp "ult" %k, %nrows : i64
    llvm.cond_br %okk, ^b1, ^panic(%k, %nrows : i64, i64)
  ^b1:
    %lenp = llvm.getelementptr %rows[%k, 1] : (!llvm.ptr, i64) -> !llvm.ptr, !row
    %len = llvm.load %lenp : !llvm.ptr -> i64
    %okj = llvm.icmp "ult" %j, %len : i64
    llvm.cond_br %okj, ^b2, ^panic(%j, %len : i64, i64)
  ^b2:
    %pp = llvm.getelementptr %rows[%k, 0] : (!llvm.ptr, i64) -> !llvm.ptr, !row
    %ptr = llvm.load %pp : !llvm.ptr -> !llvm.ptr
    %ep = llvm.getelementptr %ptr[%j] : (!llvm.ptr, i64) -> !llvm.ptr, i64
    %v = llvm.load %ep : !llvm.ptr -> i64
    %s2 = llvm.add %s, %v : i64
    %k2 = llvm.add %k, %c1 : i64
    %c = llvm.icmp "slt" %k2, %c64 : i64
    llvm.cond_br %c, ^inner(%k2, %s2 : i64, i64), ^latch(%s2 : i64)
  ^latch(%t: i64):
    %len0 = llvm.getelementptr %rows[%c0, 1] : (!llvm.ptr, i64) -> !llvm.ptr, !row
    llvm.store %j, %len0 : i64, !llvm.ptr
    %j2 = llvm.add %j, %c1 : i64
    %cj = llvm.icmp "slt" %j2, %n : i64
    llvm.cond_br %cj, ^outer(%j2, %t : i64, i64), ^done(%t : i64)
  ^done(%r: i64):
    llvm.return %r : i64
  ^panic(%x: i64, %y: i64):
    llvm.call @panicIndex(%x, %y) : (i64, i64) -> ()
    llvm.unreachable
  }
  // if 64 <= nrows { for j < n { for k < 64 { s += rows[k][j] } } }
  llvm.func @colsumif(%rows: !llvm.ptr, %nrows: i64, %n: i64) -> i64 {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %c64 = llvm.mlir.constant(64 : i64) : i64
    %fits = llvm.icmp "sle" %c64, %nrows : i64
    llvm.cond_br %fits, ^start, ^done(%c0 : i64)
  ^start:
    %z = llvm.icmp "sgt" %n, %c0 : i64
    llvm.cond_br %z, ^outer(%c0, %c0 : i64, i64), ^done(%c0 : i64)
  ^outer(%j: i64, %acc: i64):
    llvm.br ^inner(%c0, %acc : i64, i64)
  ^inner(%k: i64, %s: i64):
    %lenp = llvm.getelementptr %rows[%k, 1] : (!llvm.ptr, i64) -> !llvm.ptr, !row
    %len = llvm.load %lenp : !llvm.ptr -> i64
    %okj = llvm.icmp "ult" %j, %len : i64
    llvm.cond_br %okj, ^b2, ^panic(%j, %len : i64, i64)
  ^b2:
    %pp = llvm.getelementptr %rows[%k, 0] : (!llvm.ptr, i64) -> !llvm.ptr, !row
    %ptr = llvm.load %pp : !llvm.ptr -> !llvm.ptr
    %ep = llvm.getelementptr %ptr[%j] : (!llvm.ptr, i64) -> !llvm.ptr, i64
    %v = llvm.load %ep : !llvm.ptr -> i64
    %s2 = llvm.add %s, %v : i64
    %k2 = llvm.add %k, %c1 : i64
    %c = llvm.icmp "slt" %k2, %c64 : i64
    llvm.cond_br %c, ^inner(%k2, %s2 : i64, i64), ^latch(%s2 : i64)
  ^latch(%t: i64):
    %j2 = llvm.add %j, %c1 : i64
    %cj = llvm.icmp "slt" %j2, %n : i64
    llvm.cond_br %cj, ^outer(%j2, %t : i64, i64), ^done(%t : i64)
  ^done(%r: i64):
    llvm.return %r : i64
  ^panic(%x: i64, %y: i64):
    llvm.call @panicIndex(%x, %y) : (i64, i64) -> ()
    llvm.unreachable
  }
}
