// Passes.h: passes on the go dialect.
#ifndef MARMOT_DIALECT_GO_PASSES_H
#define MARMOT_DIALECT_GO_PASSES_H

#include "marmot/Dialect/Go/GoDialect.h"
#include "mlir/IR/BuiltinOps.h"
#include "mlir/Pass/Pass.h"

namespace marmot::go {

#define GEN_PASS_DECL
#include "marmot/Dialect/Go/Passes.h.inc"

#define GEN_PASS_REGISTRATION
#include "marmot/Dialect/Go/Passes.h.inc"

/// Recomputes the go.noescape argument attributes of the functions defined
/// in m (the escape summaries), trusting go.noescape and llvm.nocapture on
/// declarations. The go-escape pass does this as its first step.
void summarizeEscapes(mlir::ModuleOp m);

} // namespace marmot::go

#endif
