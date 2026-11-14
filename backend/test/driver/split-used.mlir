// -split-large: functions that only llvm.compiler.used keeps (linkonce_odr
// thunks the front end lists there) stay defined when their module is split
// into groups: the used list goes to group 0 with every linkonce copy.
// RUN: rm -rf %t.cache %t.d && mkdir -p %t.d && cp %s %t.d/a.mlir
// RUN: printf '%t.d/a.mlir %t.a.o\n' > %t.jobs
// RUN: %backend -O2 -batch %t.jobs -split-large=8 -cache-dir %t.cache -time 2>&1 | %FileCheck %s
// RUN: %llvm-nm --defined-only $(cat %t.a.o.parts) > %t.syms
// RUN: for i in 1 2 3 4 5 6 7 8; do grep -q "kept$i" %t.syms || { echo "kept$i lost"; exit 1; }; done
// CHECK: 1 modules split into {{[2-9]|1[0-6]}} groups
module {
  llvm.func linkonce_odr @kept1(%a: i64) -> i64 {
    %c = llvm.mlir.constant(1 : i64) : i64
    %r = llvm.mul %a, %c : i64
    llvm.return %r : i64
  }
  llvm.func linkonce_odr @kept2(%a: i64) -> i64 {
    %c = llvm.mlir.constant(2 : i64) : i64
    %r = llvm.mul %a, %c : i64
    llvm.return %r : i64
  }
  llvm.func linkonce_odr @kept3(%a: i64) -> i64 {
    %c = llvm.mlir.constant(3 : i64) : i64
    %r = llvm.mul %a, %c : i64
    llvm.return %r : i64
  }
  llvm.func linkonce_odr @kept4(%a: i64) -> i64 {
    %c = llvm.mlir.constant(4 : i64) : i64
    %r = llvm.mul %a, %c : i64
    llvm.return %r : i64
  }
  llvm.func linkonce_odr @kept5(%a: i64) -> i64 {
    %c = llvm.mlir.constant(5 : i64) : i64
    %r = llvm.mul %a, %c : i64
    llvm.return %r : i64
  }
  llvm.func linkonce_odr @kept6(%a: i64) -> i64 {
    %c = llvm.mlir.constant(6 : i64) : i64
    %r = llvm.mul %a, %c : i64
    llvm.return %r : i64
  }
  llvm.func linkonce_odr @kept7(%a: i64) -> i64 {
    %c = llvm.mlir.constant(7 : i64) : i64
    %r = llvm.mul %a, %c : i64
    llvm.return %r : i64
  }
  llvm.func linkonce_odr @kept8(%a: i64) -> i64 {
    %c = llvm.mlir.constant(8 : i64) : i64
    %r = llvm.mul %a, %c : i64
    llvm.return %r : i64
  }
  llvm.func @work1(%a: i64) -> i64 attributes {passthrough = ["noinline"]} {
    %c = llvm.mlir.constant(1 : i64) : i64
    %x = llvm.add %a, %c : i64
    %y = llvm.mul %x, %x : i64
    %z = llvm.sub %y, %a : i64
    %w = llvm.xor %z, %c : i64
    llvm.return %w : i64
  }
  llvm.func @work2(%a: i64) -> i64 attributes {passthrough = ["noinline"]} {
    %c = llvm.mlir.constant(2 : i64) : i64
    %x = llvm.add %a, %c : i64
    %y = llvm.mul %x, %x : i64
    %z = llvm.sub %y, %a : i64
    %w = llvm.xor %z, %c : i64
    llvm.return %w : i64
  }
  llvm.func @work3(%a: i64) -> i64 attributes {passthrough = ["noinline"]} {
    %c = llvm.mlir.constant(3 : i64) : i64
    %x = llvm.add %a, %c : i64
    %y = llvm.mul %x, %x : i64
    %z = llvm.sub %y, %a : i64
    %w = llvm.xor %z, %c : i64
    llvm.return %w : i64
  }
  llvm.func @work4(%a: i64) -> i64 attributes {passthrough = ["noinline"]} {
    %c = llvm.mlir.constant(4 : i64) : i64
    %x = llvm.add %a, %c : i64
    %y = llvm.mul %x, %x : i64
    %z = llvm.sub %y, %a : i64
    %w = llvm.xor %z, %c : i64
    llvm.return %w : i64
  }
  llvm.func @work5(%a: i64) -> i64 attributes {passthrough = ["noinline"]} {
    %c = llvm.mlir.constant(5 : i64) : i64
    %x = llvm.add %a, %c : i64
    %y = llvm.mul %x, %x : i64
    %z = llvm.sub %y, %a : i64
    %w = llvm.xor %z, %c : i64
    llvm.return %w : i64
  }
  llvm.func @work6(%a: i64) -> i64 attributes {passthrough = ["noinline"]} {
    %c = llvm.mlir.constant(6 : i64) : i64
    %x = llvm.add %a, %c : i64
    %y = llvm.mul %x, %x : i64
    %z = llvm.sub %y, %a : i64
    %w = llvm.xor %z, %c : i64
    llvm.return %w : i64
  }
  llvm.func @work7(%a: i64) -> i64 attributes {passthrough = ["noinline"]} {
    %c = llvm.mlir.constant(7 : i64) : i64
    %x = llvm.add %a, %c : i64
    %y = llvm.mul %x, %x : i64
    %z = llvm.sub %y, %a : i64
    %w = llvm.xor %z, %c : i64
    llvm.return %w : i64
  }
  llvm.func @work8(%a: i64) -> i64 attributes {passthrough = ["noinline"]} {
    %c = llvm.mlir.constant(8 : i64) : i64
    %x = llvm.add %a, %c : i64
    %y = llvm.mul %x, %x : i64
    %z = llvm.sub %y, %a : i64
    %w = llvm.xor %z, %c : i64
    llvm.return %w : i64
  }
  llvm.func @work9(%a: i64) -> i64 attributes {passthrough = ["noinline"]} {
    %c = llvm.mlir.constant(9 : i64) : i64
    %x = llvm.add %a, %c : i64
    %y = llvm.mul %x, %x : i64
    %z = llvm.sub %y, %a : i64
    %w = llvm.xor %z, %c : i64
    llvm.return %w : i64
  }
  llvm.func @work10(%a: i64) -> i64 attributes {passthrough = ["noinline"]} {
    %c = llvm.mlir.constant(10 : i64) : i64
    %x = llvm.add %a, %c : i64
    %y = llvm.mul %x, %x : i64
    %z = llvm.sub %y, %a : i64
    %w = llvm.xor %z, %c : i64
    llvm.return %w : i64
  }
  llvm.func @work11(%a: i64) -> i64 attributes {passthrough = ["noinline"]} {
    %c = llvm.mlir.constant(11 : i64) : i64
    %x = llvm.add %a, %c : i64
    %y = llvm.mul %x, %x : i64
    %z = llvm.sub %y, %a : i64
    %w = llvm.xor %z, %c : i64
    llvm.return %w : i64
  }
  llvm.func @work12(%a: i64) -> i64 attributes {passthrough = ["noinline"]} {
    %c = llvm.mlir.constant(12 : i64) : i64
    %x = llvm.add %a, %c : i64
    %y = llvm.mul %x, %x : i64
    %z = llvm.sub %y, %a : i64
    %w = llvm.xor %z, %c : i64
    llvm.return %w : i64
  }
  llvm.mlir.global appending @llvm.compiler.used() {addr_space = 0 : i32, section = "llvm.metadata"} : !llvm.array<8 x ptr> {
    %u0 = llvm.mlir.undef : !llvm.array<8 x ptr>
    %p1 = llvm.mlir.addressof @kept1 : !llvm.ptr
    %p2 = llvm.mlir.addressof @kept2 : !llvm.ptr
    %p3 = llvm.mlir.addressof @kept3 : !llvm.ptr
    %p4 = llvm.mlir.addressof @kept4 : !llvm.ptr
    %p5 = llvm.mlir.addressof @kept5 : !llvm.ptr
    %p6 = llvm.mlir.addressof @kept6 : !llvm.ptr
    %p7 = llvm.mlir.addressof @kept7 : !llvm.ptr
    %p8 = llvm.mlir.addressof @kept8 : !llvm.ptr
    %u1 = llvm.insertvalue %p1, %u0[0] : !llvm.array<8 x ptr>
    %u2 = llvm.insertvalue %p2, %u1[1] : !llvm.array<8 x ptr>
    %u3 = llvm.insertvalue %p3, %u2[2] : !llvm.array<8 x ptr>
    %u4 = llvm.insertvalue %p4, %u3[3] : !llvm.array<8 x ptr>
    %u5 = llvm.insertvalue %p5, %u4[4] : !llvm.array<8 x ptr>
    %u6 = llvm.insertvalue %p6, %u5[5] : !llvm.array<8 x ptr>
    %u7 = llvm.insertvalue %p7, %u6[6] : !llvm.array<8 x ptr>
    %u8 = llvm.insertvalue %p8, %u7[7] : !llvm.array<8 x ptr>
    llvm.return %u8 : !llvm.array<8 x ptr>
  }
  llvm.func @main() -> i32 {
    %c = llvm.mlir.constant(1 : i64) : i64
    %r = llvm.call @work1(%c) : (i64) -> i64
    %t = llvm.trunc %r : i64 to i32
    llvm.return %t : i32
  }
}
