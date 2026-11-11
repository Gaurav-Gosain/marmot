#ifndef MARMOT_GO_PASSES
#define MARMOT_GO_PASSES

include "mlir/Pass/PassBase.td"

def GoToLLVM : Pass<"go-to-llvm", "::mlir::ModuleOp"> {
  let summary = "Lower go dialect ops to the llvm dialect and runtime calls";
  let dependentDialects = ["::mlir::LLVM::LLVMDialect"];
}

#endif
