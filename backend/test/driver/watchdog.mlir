// The watchdog ends the process with status 3 and names the input.
// RUN: sleep 4 | env MARMOT_MAX_SECONDS=1 %not %backend -o %t.o - 2>&1 | %FileCheck %s --check-prefix=TIME
// RUN: sleep 2 | env MARMOT_MAX_MEM_MB=1 %not %backend -o %t.o - 2>&1 | %FileCheck %s --check-prefix=MEM
// RUN: env MARMOT_MAX_MEM_MB=0 MARMOT_MAX_SECONDS=0 %backend -o %t.o %s
// TIME: marmot-backend: time limit exceeded: {{[0-9]+}} s > 1 s (MARMOT_MAX_SECONDS) while compiling -
// MEM: marmot-backend: memory limit exceeded: {{[0-9]+}} MB > 1 MB (MARMOT_MAX_MEM_MB)
module {
  llvm.func @f() {
    llvm.return
  }
}
