// GoDialect.cpp: go dialect registration, verifiers, type inference.
#include "marmot/Dialect/Go/GoDialect.h"

#include "mlir/IR/Builders.h"
#include "mlir/IR/DialectImplementation.h"

using namespace mlir;
using namespace marmot::go;

#include "marmot/Dialect/Go/GoDialect.cpp.inc"

void GoDialect::initialize() {
  addOperations<
#define GET_OP_LIST
#include "marmot/Dialect/Go/GoOps.cpp.inc"
      >();
}

Type marmot::go::memoryType(Type t) {
  if (auto it = dyn_cast<IntegerType>(t); it && it.getWidth() == 1)
    return IntegerType::get(t.getContext(), 8);
  return t;
}

static bool isMemoryType(Type t) {
  if (auto it = dyn_cast<IntegerType>(t))
    return it.getWidth() != 1;
  return LLVM::isCompatibleType(t) && !isa<LLVM::LLVMVoidType, LLVM::LLVMFunctionType>(t);
}

//===----------------------------------------------------------------------===//
// Allocation family
//===----------------------------------------------------------------------===//

LogicalResult NewOp::inferReturnTypes(MLIRContext *ctx, std::optional<Location>,
                                      ValueRange, DictionaryAttr, OpaqueProperties,
                                      RegionRange,
                                      SmallVectorImpl<Type> &results) {
  results.push_back(LLVM::LLVMPointerType::get(ctx));
  return success();
}

LogicalResult NewOp::verify() {
  if (!isMemoryType(getElemType()))
    return emitOpError("element type must be an LLVM memory type, got ")
           << getElemType();
  return success();
}

LogicalResult MakeSliceOp::inferReturnTypes(MLIRContext *ctx, std::optional<Location>,
                                            ValueRange operands, DictionaryAttr,
                                            OpaqueProperties, RegionRange,
                                            SmallVectorImpl<Type> &results) {
  Type intTy = operands[0].getType();
  results.push_back(LLVM::LLVMStructType::getLiteral(
      ctx, {LLVM::LLVMPointerType::get(ctx), intTy, intTy}));
  return success();
}

LogicalResult MakeSliceOp::verify() {
  if (!isMemoryType(getElemType()))
    return emitOpError("element type must be an LLVM memory type, got ")
           << getElemType();
  return success();
}

LLVM::LLVMStructType MakeClosureOp::getEnvType() {
  SmallVector<Type> fields{LLVM::LLVMPointerType::get(getContext())};
  for (Value v : getCaptures())
    fields.push_back(memoryType(v.getType()));
  return LLVM::LLVMStructType::getLiteral(getContext(), fields);
}

LogicalResult BoxOp::inferReturnTypes(MLIRContext *ctx, std::optional<Location>,
                                      ValueRange, DictionaryAttr, OpaqueProperties,
                                      RegionRange, SmallVectorImpl<Type> &results) {
  results.push_back(LLVM::LLVMPointerType::get(ctx));
  return success();
}

#define GET_OP_CLASSES
#include "marmot/Dialect/Go/GoOps.cpp.inc"
