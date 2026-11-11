// GoDialect.h: the go dialect. See docs/dialect.md.
#ifndef MARMOT_DIALECT_GO_GODIALECT_H
#define MARMOT_DIALECT_GO_GODIALECT_H

#include "mlir/Dialect/LLVMIR/LLVMDialect.h"
#include "mlir/Dialect/LLVMIR/LLVMTypes.h"
#include "mlir/IR/BuiltinAttributes.h"
#include "mlir/IR/Dialect.h"
#include "mlir/IR/OpDefinition.h"
#include "mlir/IR/OpImplementation.h"
#include "mlir/Interfaces/InferTypeOpInterface.h"
#include "mlir/Interfaces/SideEffectInterfaces.h"

#include "marmot/Dialect/Go/GoDialect.h.inc"

#define GET_OP_CLASSES
#include "marmot/Dialect/Go/GoOps.h.inc"

namespace marmot::go {

/// The LLVM type used to store a value of SSA type t in memory: i1 becomes
/// i8 (SPEC 2.5), everything else is unchanged.
mlir::Type memoryType(mlir::Type t);

} // namespace marmot::go

#endif
