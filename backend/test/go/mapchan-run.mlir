// End to end: maps and channels against a fake runtime.
// RUN: %clang -c %S/Inputs/fakert.c -o %t.rt.o
// RUN: %clang -c %S/Inputs/fakert_map.c -o %t.rtm.o
// RUN: %backend -O2 -o %t.o %s
// RUN: %clang %t.o %t.rt.o %t.rtm.o -o %t.exe
// RUN: %t.exe 4 | %FileCheck %s
// RUN: %backend -O0 -o %t.0.o %s
// RUN: %clang %t.0.o %t.rt.o %t.rtm.o -o %t.0.exe
// RUN: %t.0.exe 4 | %FileCheck %s
// m[k] = k*k for k < n; delete(m, 1); sum m[k] for k < n (0 for the deleted),
// plus 1000 per present key; then send n through a channel and receive it twice.
// n=4: values 0+0+4+9 = 13, present 3 -> 3013; chan 4 + 0 (empty, ok false) -> 3017,
// plus 100 for the first ok -> 3117.
// CHECK: result 3117
module {
  llvm.mlir.global external constant @"type:map[int]int"(dense<0> : tensor<56xi8>) : !llvm.array<56 x i8>
  llvm.mlir.global external constant @"type:int"(dense<0> : tensor<56xi8>) : !llvm.array<56 x i8>
  llvm.func @test_main(%n: i64) -> i64 {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %c100 = llvm.mlir.constant(100 : i64) : i64
    %c1000 = llvm.mlir.constant(1000 : i64) : i64
    %m = go.make_map @"type:map[int]int", %n
    llvm.br ^fill(%c0 : i64)
  ^fill(%i: i64):
    %more = llvm.icmp "slt" %i, %n : i64
    llvm.cond_br %more, ^fillbody, ^filled
  ^fillbody:
    %sq = llvm.mul %i, %i : i64
    go.map_update %m[%i] = %sq, @"type:map[int]int" : i64, i64
    %i1 = llvm.add %i, %c1 : i64
    llvm.br ^fill(%i1 : i64)
  ^filled:
    go.map_delete %m[%c1], @"type:map[int]int" : i64
    llvm.br ^sum(%c0, %c0 : i64, i64)
  ^sum(%j: i64, %acc: i64):
    %more2 = llvm.icmp "slt" %j, %n : i64
    llvm.cond_br %more2, ^sumbody, ^summed
  ^sumbody:
    %v = go.map_lookup %m[%j], @"type:map[int]int" : i64 -> i64
    %v2, %ok = go.map_lookup %m[%j], @"type:map[int]int" commaok : i64 -> i64
    %okx = llvm.zext %ok : i1 to i64
    %okk = llvm.mul %okx, %c1000 : i64
    %a1 = llvm.add %acc, %v : i64
    %a2 = llvm.add %a1, %okk : i64
    %j1 = llvm.add %j, %c1 : i64
    llvm.br ^sum(%j1, %a2 : i64, i64)
  ^summed:
    %c = go.make_chan @"type:int", %c1
    go.chan_send %c, %n : i64
    %r1, %ok1 = go.chan_recv %c commaok : i64
    %r2, %ok2 = go.chan_recv %c commaok : i64
    %ok1x = llvm.zext %ok1 : i1 to i64
    %ok2x = llvm.zext %ok2 : i1 to i64
    %oks = llvm.add %ok1x, %ok2x : i64
    %okh = llvm.mul %oks, %c100 : i64
    %t1 = llvm.add %acc, %r1 : i64
    %t2 = llvm.add %t1, %r2 : i64
    %t3 = llvm.add %t2, %okh : i64
    llvm.return %t3 : i64
  }
}
