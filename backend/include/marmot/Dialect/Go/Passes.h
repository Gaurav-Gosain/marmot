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

} // namespace marmot::go

#endif
