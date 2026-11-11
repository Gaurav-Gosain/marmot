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

//===----------------------------------------------------------------------===//
// Indexing family
//===----------------------------------------------------------------------===//

LogicalResult IndexOp::verify() {
  if (!isMemoryType(getElemType()))
    return emitOpError("element type must be an LLVM memory type, got ")
           << getElemType();
  return success();
}

LogicalResult SliceOp::verify() {
  if (!isMemoryType(getElemType()))
    return emitOpError("element type must be an LLVM memory type, got ")
           << getElemType();
  auto st = cast<LLVM::LLVMStructType>(getType());
  auto body = st.getBody();
  Type intTy = getCap().getType();
  unsigned want = getMax() ? 3 : 2;
  bool ok = body.size() == want && isa<LLVM::LLVMPointerType>(body[0]);
  for (unsigned i = 1; ok && i < body.size(); ++i)
    ok = body[i] == intTy;
  if (!ok)
    return emitOpError("result must be !llvm.struct<(ptr, iN, iN)> with max and "
                       "!llvm.struct<(ptr, iN)> without, where iN is the type of cap");
  return success();
}

//===----------------------------------------------------------------------===//
// Interfaces family
//===----------------------------------------------------------------------===//

LLVM::LLVMStructType marmot::go::ifaceType(MLIRContext *ctx) {
  Type p = LLVM::LLVMPointerType::get(ctx);
  return LLVM::LLVMStructType::getLiteral(ctx, {p, p});
}

bool marmot::go::isIfaceType(Type t) { return t == ifaceType(t.getContext()); }

LogicalResult IfaceCallOp::verify() {
  if (getNumResults() > 1)
    return emitOpError("has at most one result; return multiple values as a struct");
  if (getMethod() < 0)
    return emitOpError("method index must be non-negative");
  return success();
}

LogicalResult TypeAssertOp::inferReturnTypes(MLIRContext *ctx, std::optional<Location>,
                                             Adaptor adaptor,
                                             SmallVectorImpl<Type> &results) {
  if (adaptor.getToIface() || adaptor.getToAny())
    results.push_back(ifaceType(ctx));
  else
    results.push_back(LLVM::LLVMPointerType::get(ctx));
  if (adaptor.getCommaok())
    results.push_back(IntegerType::get(ctx, 1));
  return success();
}

LogicalResult TypeAssertOp::verify() {
  if (getToIface() && getToAny())
    return emitOpError("to_iface and to_any are exclusive");
  return success();
}

//===----------------------------------------------------------------------===//
// Control family
//===----------------------------------------------------------------------===//

SuccessorOperands DeferFrameOp::getSuccessorOperands(unsigned) {
  return SuccessorOperands(MutableOperandRange(getOperation(), 0, 0));
}

LogicalResult DeferFrameOp::verify() {
  auto fn = (*this)->getParentOfType<LLVM::LLVMFuncOp>();
  if (!fn || (*this)->getBlock() != &fn.getBody().front())
    return emitOpError("must terminate the entry block of an llvm.func");
  for (Block *succ : (*this)->getSuccessors())
    if (succ->getNumArguments() != 0)
      return emitOpError("successors must not take arguments");
  return success();
}

#define GET_OP_CLASSES
#include "marmot/Dialect/Go/GoOps.cpp.inc"
