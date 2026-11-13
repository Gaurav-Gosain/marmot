// Reader.h: a reader for the MLIR text the frontend writes for dev builds
// (llvm dialect, the operations and syntax of internal/codegen), much faster
// than the general MLIR parser. It builds the same module the parser would;
// on anything else it gives up and the caller parses the text with
// mlir::parseSourceFile.
#ifndef MARMOT_TIER0_READER_H
#define MARMOT_TIER0_READER_H

#include "mlir/IR/BuiltinOps.h"
#include "mlir/IR/OwningOpRef.h"
#include "llvm/ADT/StringRef.h"
#include <string>

namespace marmot {
namespace tier0 {

// The module in text (whose buffer name is name, used for the locations of
// operations without one, as the parser does), or null with the reason in
// why when the text is outside what the reader knows. The context must have
// the LLVM dialect loaded.
mlir::OwningOpRef<mlir::ModuleOp> readModule(mlir::MLIRContext *ctx, llvm::StringRef text,
                                             llvm::StringRef name, std::string &why);

} // namespace tier0
} // namespace marmot

#endif
