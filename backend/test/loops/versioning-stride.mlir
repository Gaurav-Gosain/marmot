// Rule V, equal steps (-loops-bcv-stride) and pinned starts (-loops-bcv-pin),
// in the versioning-only mode (MARMOT_LOOPS_VERSION=1, no -par), which turns
// both on unless the command line sets them.
// pop: v := s[len(s)-1]; s = s[:len(s)-1]. The index and the length go down
// together, so a range compare (max index < min length) never holds; the
// equal step compare does.
// RUN: env MARMOT_LOOPS_VERSION=1 %backend -O2 -loops-bcv-pin=false -loops-remarks -emit=llvm -o %t.ll %s 2>&1 | %FileCheck %s --check-prefix=REMARK
// RUN: %FileCheck %s --check-prefix=STRIDE < %t.ll
// RUN: env MARMOT_LOOPS_VERSION=1 %backend -O2 -loops-bcv-stride=false -loops-bcv-pin=false -emit=llvm -o %t2.ll %s
// RUN: %FileCheck %s --check-prefix=NOSTRIDE < %t2.ll
// RUN: env MARMOT_LOOPS_VERSION=1 %backend -O2 -emit=llvm -o %t3.ll %s
// RUN: %FileCheck %s --check-prefix=PIN < %t3.ll
// Without the switch and without -par no loop is versioned.
// RUN: env -u MARMOT_LOOPS_VERSION %backend -O2 -emit=llvm -o %t4.ll %s
// RUN: %FileCheck %s --check-prefix=OFF < %t4.ll
// OFF-NOT: bcv.ok

// REMARK: loops: pop: loop {{.*}}: versioned (version-only mode)
// REMARK: loops: popbad: loop {{.*}}: versioned (version-only mode)
// REMARK: loops: pushpop: loop {{.*}}: versioned (version-only mode)

// With the switch the guard is len - n + 1 > 0 (all pops find len > 0);
// the slow copy keeps both checks.
// STRIDE-LABEL: define i64 @pop(
// STRIDE-NOT: icmp slt i64 %1, 0
// STRIDE: br i1 %bcv.ok
// STRIDE: .fast
// STRIDE: call void @panicIndex(
// Without it len != 0 becomes len < 0 in the guard, which never holds.
// NOSTRIDE-LABEL: define i64 @pop(
// NOSTRIDE: icmp slt i64 %1, 0
// NOSTRIDE: br i1 %bcv.ok
// STRIDE-LABEL: define i64 @popbad(
// STRIDE: icmp ugt i64 %3, -9223372036854775808
// STRIDE: .fast
// STRIDE: call void @panicIndex(
// PIN-LABEL: define i64 @pushpop(
// PIN: .fast
// PIN: call void @panicIndex(

module {
  llvm.func @panicIndex(i64, i64) attributes {passthrough = ["noreturn"]}

  // n pops from a slice of length len over p.
  llvm.func @pop(%p: !llvm.ptr, %len: i64, %n: i64) -> i64 {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %z = llvm.icmp "sgt" %n, %c0 : i64
    llvm.cond_br %z, ^loop(%c0, %len, %c0 : i64, i64, i64), ^done(%c0 : i64)
  ^loop(%i: i64, %l: i64, %s: i64):
    %e = llvm.icmp "eq" %l, %c0 : i64
    llvm.cond_br %e, ^panic(%l : i64), ^c1
  ^c1:
    %k = llvm.sub %l, %c1 : i64
    %ok = llvm.icmp "ult" %k, %l : i64
    llvm.cond_br %ok, ^body, ^panic(%k : i64)
  ^body:
    %a = llvm.getelementptr %p[%k] : (!llvm.ptr, i64) -> !llvm.ptr, i64
    %v = llvm.load %a : !llvm.ptr -> i64
    %s2 = llvm.add %s, %v : i64
    %i2 = llvm.add %i, %c1 : i64
    %c = llvm.icmp "slt" %i2, %n : i64
    llvm.cond_br %c, ^loop(%i2, %k, %s2 : i64, i64, i64), ^done(%s2 : i64)
  ^done(%r: i64):
    llvm.return %r : i64
  ^panic(%x: i64):
    llvm.call @panicIndex(%x, %l) : (i64, i64) -> ()
    llvm.unreachable
  }

  // Equal steps with a symbolic difference (s[len(s)+d]): the guard tests
  // 0 < -d at run time, and the slow copy keeps the check for d >= 0.
  llvm.func @popbad(%p: !llvm.ptr, %len: i64, %n: i64, %d: i64) -> i64 {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %z = llvm.icmp "sgt" %n, %c0 : i64
    llvm.cond_br %z, ^loop(%c0, %len, %c0 : i64, i64, i64), ^done(%c0 : i64)
  ^loop(%i: i64, %l: i64, %s: i64):
    %l1 = llvm.add %l, %d : i64
    %ok = llvm.icmp "ult" %l1, %l : i64
    llvm.cond_br %ok, ^body, ^panic(%l1 : i64)
  ^body:
    %a = llvm.getelementptr %p[%l1] : (!llvm.ptr, i64) -> !llvm.ptr, i64
    %v = llvm.load %a : !llvm.ptr -> i64
    %s2 = llvm.add %s, %v : i64
    %k = llvm.sub %l, %c1 : i64
    %i2 = llvm.add %i, %c1 : i64
    %c = llvm.icmp "slt" %i2, %n : i64
    llvm.cond_br %c, ^loop(%i2, %k, %s2 : i64, i64, i64), ^done(%s2 : i64)
  ^done(%r: i64):
    llvm.return %r : i64
  ^panic(%x: i64):
    llvm.call @panicIndex(%x, %l) : (i64, i64) -> ()
    llvm.unreachable
  }

  // Push m values (checked against cap), then pop n: the pop loop's start is
  // the push loop's exit value.
  llvm.func @pushpop(%p: !llvm.ptr, %cap: i64, %m: i64, %n: i64) -> i64 {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %zm = llvm.icmp "sgt" %m, %c0 : i64
    llvm.cond_br %zm, ^push(%c0, %c0 : i64, i64), ^mid(%c0 : i64)
  ^push(%j: i64, %l0: i64):
    %pok = llvm.icmp "ult" %l0, %cap : i64
    llvm.cond_br %pok, ^pbody, ^panic(%l0, %cap : i64, i64)
  ^pbody:
    %pa = llvm.getelementptr %p[%l0] : (!llvm.ptr, i64) -> !llvm.ptr, i64
    llvm.store %j, %pa : i64, !llvm.ptr
    %l1 = llvm.add %l0, %c1 : i64
    %j2 = llvm.add %j, %c1 : i64
    %pc = llvm.icmp "slt" %j2, %m : i64
    llvm.cond_br %pc, ^push(%j2, %l1 : i64, i64), ^mid(%l1 : i64)
  ^mid(%len: i64):
    %z = llvm.icmp "sgt" %n, %c0 : i64
    llvm.cond_br %z, ^loop(%c0, %len, %c0 : i64, i64, i64), ^done(%c0 : i64)
  ^loop(%i: i64, %l: i64, %s: i64):
    %e = llvm.icmp "eq" %l, %c0 : i64
    llvm.cond_br %e, ^panic(%l, %l : i64, i64), ^c1
  ^c1:
    %k = llvm.sub %l, %c1 : i64
    %ok = llvm.icmp "ult" %k, %l : i64
    llvm.cond_br %ok, ^body, ^panic(%k, %l : i64, i64)
  ^body:
    %a = llvm.getelementptr %p[%k] : (!llvm.ptr, i64) -> !llvm.ptr, i64
    %v = llvm.load %a : !llvm.ptr -> i64
    %s2 = llvm.add %s, %v : i64
    %i2 = llvm.add %i, %c1 : i64
    %c = llvm.icmp "slt" %i2, %n : i64
    llvm.cond_br %c, ^loop(%i2, %k, %s2 : i64, i64, i64), ^done(%s2 : i64)
  ^done(%r: i64):
    llvm.return %r : i64
  ^panic(%x: i64, %y: i64):
    llvm.call @panicIndex(%x, %y) : (i64, i64) -> ()
    llvm.unreachable
  }
}
