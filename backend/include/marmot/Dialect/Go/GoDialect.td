#ifndef MARMOT_GO_DIALECT
#define MARMOT_GO_DIALECT

include "mlir/IR/OpBase.td"

def Go_Dialect : Dialect {
  let name = "go";
  let cppNamespace = "::marmot::go";
  let summary = "Go semantics carried down to the marmot backend";
  let description = [{
    Ops for Go operations that are not plain LLVM instructions. Values use the
    LLVM dialect types of Go values. Every op lowers to the llvm dialect
    plus calls into package runtime.
  }];
  let dependentDialects = ["::mlir::LLVM::LLVMDialect"];
}

class Go_Op<string mnemonic, list<Trait> traits = []>
    : Op<Go_Dialect, mnemonic, traits>;

#endif
