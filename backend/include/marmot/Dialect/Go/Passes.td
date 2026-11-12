#ifndef MARMOT_GO_PASSES
#define MARMOT_GO_PASSES

include "mlir/Pass/PassBase.td"

def GoToLLVM : Pass<"go-to-llvm", "::mlir::ModuleOp"> {
  let summary = "Lower go dialect ops to the llvm dialect and runtime calls";
  let dependentDialects = ["::mlir::LLVM::LLVMDialect"];
}

def GoEscape : Pass<"go-escape", "::mlir::ModuleOp"> {
  let summary = "Escape analysis: put non-escaping allocations on the stack";
  let description = [{
    Marks go.new, go.box, go.make_closure and constant-size go.make_slice
    results that cannot outlive the frame with `stack`. Interprocedural within
    the module: each llvm.func parameter that does not escape gets the
    `go.noescape` argument attribute, computed to a fixpoint. Calls to
    functions outside the module trust `go.noescape` or `llvm.nocapture` on
    the declaration.
  }];
  let options = [
    Option<"maxObjectSize", "max-object-size", "uint64_t", "8192",
           "Largest allocation in bytes that may move to the stack">,
    Option<"maxFrameSize", "max-frame-size", "uint64_t", "65536",
           "Most bytes of moved allocations per function">,
  ];
}

def AggregateCopies : Pass<"aggregate-copies", "::mlir::ModuleOp"> {
  let summary = "Copy large aggregates through memory instead of SSA values";
  let description = [{
    LLVM code generation expands a load or store of a struct or array value
    into one operation per scalar, which for a large type takes unbounded
    time and memory. This pass rewrites, for types of at least `min-size`
    bytes: a load whose uses are stores into memmove calls, a load whose uses
    are extractvalue into element loads, and a store of a zero constant into
    memset. Any load, store, call argument or result of an aggregate of more
    than `max-value-size` bytes that remains is an error.
  }];
  let options = [
    Option<"minSize", "min-size", "uint64_t", "256",
           "Smallest aggregate in bytes that is rewritten">,
    Option<"maxValueSize", "max-value-size", "uint64_t", "1048576",
           "Largest aggregate SSA value in bytes that is accepted">,
  ];
}

def GoBCE : Pass<"go-bce", "::mlir::ModuleOp"> {
  let summary = "Bounds check and nil check elimination";
  let description = [{
    Removes go.bounds_check and go.nil_check ops, and marks go.index
    `nocheck`, when a rule of the pass proves the check cannot fail.
  }];
  let options = [
    Option<"report", "report", "std::string", /*default=*/"\"\"",
           "Append one line per removed check to this file: func, walk index, rule, "
           "and for B2/N2 the walk index of the dominating check">
  ];
}

def GoDevirt : Pass<"go-devirt", "::mlir::ModuleOp"> {
  let summary = "Devirtualize interface calls and fold type assertions on known itabs";
  let dependentDialects = ["::mlir::LLVM::LLVMDialect"];
}

def GoOpenDefers : Pass<"go-open-defers", "::mlir::ModuleOp"> {
  let summary = "Drop the setjmp of defer frames whose deferred calls cannot recover";
}

def GoStrings : Pass<"go-strings", "::mlir::ModuleOp"> {
  let summary = "String concat folding and no-copy []byte to string conversions";
  let dependentDialects = ["::mlir::LLVM::LLVMDialect"];
  let options = [
    Option<"noCopy", "no-copy", "bool", /*default=*/"false",
           "Rule S3: no-copy string(b) for transient uses (off until the matrix passes with it)">
  ];
}

def GoBoolPack : Pass<"go-bool-pack", "::mlir::ModuleOp"> {
  let summary = "Store local []bool slices as bit arrays";
  let dependentDialects = ["::mlir::LLVM::LLVMDialect"];
}

#endif
