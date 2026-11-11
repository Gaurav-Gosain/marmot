// RUN: %marmot-opt --go-to-llvm %s | %FileCheck %s
// RUN: %marmot-opt %s | %marmot-opt | %FileCheck %s --check-prefix=ROUND
!str = !llvm.struct<(ptr, i64)>
!slice = !llvm.struct<(ptr, i64, i64)>
module {
  // ROUND-LABEL: llvm.func @cat
  // ROUND: go.str_concat %arg0, %arg1
  // ROUND: go.str_concat %{{.*}}, %arg0, %arg1, %arg1
  // CHECK-LABEL: llvm.func @cat
  // CHECK: %[[ARR:.*]] = llvm.alloca %{{.*}} x !llvm.array<4 x struct<(ptr, i64)>>
  // CHECK: %[[AB:.*]] = llvm.call @runtime.concatstring2(%arg0, %arg1)
  // CHECK: %[[ABC:.*]] = llvm.call @runtime.concatstring3(%[[AB]], %arg0, %arg1)
  // CHECK: llvm.intr.lifetime.start %[[ARR]]
  // CHECK: llvm.getelementptr %[[ARR]][0, 3]
  // CHECK: llvm.call @runtime.concatstrings(%{{.*}}) : (!llvm.struct<(ptr, i64, i64)>) -> !llvm.struct<(ptr, i64)>
  // CHECK: llvm.intr.lifetime.end %[[ARR]]
  llvm.func @cat(%a: !str, %b: !str) -> !str {
    %ab = go.str_concat %a, %b
    %abc = go.str_concat %ab, %a, %b
    %all = go.str_concat %abc, %a, %b, %b
    llvm.return %all : !str
  }

  // ROUND-LABEL: llvm.func @conv
  // ROUND: go.str_to_bytes %arg0
  // ROUND: go.bytes_to_str %{{.*}}{{$}}
  // ROUND: go.bytes_to_str %{{.*}} nocopy
  // CHECK-LABEL: llvm.func @conv
  // CHECK: %[[B:.*]] = llvm.call @runtime.stringtoslicebyte(%arg0) : (!llvm.struct<(ptr, i64)>) -> !llvm.struct<(ptr, i64, i64)>
  // CHECK: llvm.call @runtime.slicebytetostring(%[[B]]) : (!llvm.struct<(ptr, i64, i64)>) -> !llvm.struct<(ptr, i64)>
  // CHECK: %[[P:.*]] = llvm.extractvalue %[[B]][0]
  // CHECK: %[[N:.*]] = llvm.extractvalue %[[B]][1]
  // CHECK-NOT: llvm.call
  // CHECK: llvm.return
  llvm.func @conv(%s: !str) -> !str {
    %b = go.str_to_bytes %s
    %t = go.bytes_to_str %b
    %u = go.bytes_to_str %b nocopy
    llvm.return %u : !str
  }
}
