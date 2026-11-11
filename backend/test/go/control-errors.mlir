// RUN: %marmot-opt --go-to-llvm --split-input-file --verify-diagnostics %s
llvm.func @f(%fn: !llvm.ptr) {
  // expected-error @+1 {{needs a go.defer_frame in the function}}
  go.defer %fn
  llvm.return
}

// -----

llvm.func @f() {
  llvm.br ^b
^b:
  // expected-error @+1 {{must terminate the entry block of an llvm.func}}
  go.defer_frame ^c, ^c
^c:
  llvm.return
}
