// --serve: one JSON request per line, one reply per request; the second
// request of the same batch is served from the cache by the same process.
// RUN: rm -rf %t.cache %t.o && printf '%s %t.o\n' > %t.jobs
// RUN: printf '{"batch":"%t.jobs","threads":2,"cache":"%t.cache"}\n{"batch":"%t.jobs","threads":2,"cache":"%t.cache"}\n{"nobatch":1}\n{"batch":"%t.missing"}\n' | %backend -O2 --serve -time 2> %t.err | %FileCheck %s
// RUN: %llvm-nm %t.o | %FileCheck %s --check-prefix=SYM
// RUN: %FileCheck %s --check-prefix=ERR < %t.err
// CHECK: {"ok":true}
// CHECK-NEXT: {"ok":true}
// CHECK-NEXT: {"error":"bad request: {\"nobatch\":1}","exit":1,"ok":false}
// CHECK-NEXT: {"error":"{{.*}}missing{{.*}}","exit":1,"ok":false}
// SYM: T {{_?}}f
// ERR: sum of jobs
module {
  llvm.func @f(%x: i64) -> i64 {
    llvm.return %x : i64
  }
}
