// marmot-opt: mlir-opt with the go dialect and marmot passes, for tests.
#include "marmot/Dialect/Go/Passes.h"

#include "mlir/Dialect/LLVMIR/LLVMDialect.h"
#include "mlir/IR/DialectRegistry.h"
#include "mlir/Tools/mlir-opt/MlirOptMain.h"
#include "mlir/Transforms/Passes.h"

int main(int argc, char **argv) {
  mlir::DialectRegistry registry;
  registry.insert<mlir::LLVM::LLVMDialect, marmot::go::GoDialect>();
  marmot::go::registerGoPasses();
  mlir::registerCanonicalizerPass();
  mlir::registerCSEPass();
  mlir::registerSymbolDCEPass();
  return mlir::asMainReturnCode(
      mlir::MlirOptMain(argc, argv, "marmot optimizer driver\n", registry));
}
