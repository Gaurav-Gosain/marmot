#ifndef MARMOT_GO_OPS
#define MARMOT_GO_OPS

include "marmot/Dialect/Go/GoDialect.td"
include "mlir/Dialect/LLVMIR/LLVMTypes.td"
include "mlir/Interfaces/SideEffectInterfaces.td"
include "mlir/IR/SymbolInterfaces.td"
include "mlir/Interfaces/InferTypeOpInterface.td"

//===----------------------------------------------------------------------===//
// Allocation family
//===----------------------------------------------------------------------===//

def Go_NewOp : Go_Op<"new", [MemoryEffects<[MemAlloc<DefaultResource>]>,
                             DeclareOpInterfaceMethods<InferTypeOpInterface>]> {
  let summary = "allocate a zeroed value (new(T), escaping locals)";
  let arguments = (ins FlatSymbolRefAttr:$desc, TypeAttr:$elem_type);
  let results = (outs LLVM_AnyPointer:$result);
  let assemblyFormat = "$desc `,` $elem_type attr-dict";
  let hasVerifier = 1;
}

def Go_MakeSliceOp : Go_Op<"make_slice", [AllTypesMatch<["len", "cap"]>,
    DeclareOpInterfaceMethods<InferTypeOpInterface>]> {
  let summary = "make([]T, len, cap)";
  let arguments = (ins FlatSymbolRefAttr:$desc, TypeAttr:$elem_type,
                       AnySignlessInteger:$len, AnySignlessInteger:$cap);
  let results = (outs LLVM_AnyStruct:$result);
  let assemblyFormat = [{
    $desc `,` $elem_type `(` $len `,` $cap `)` attr-dict `:` type($len)
  }];
  let hasVerifier = 1;
}

def Go_MakeClosureOp : Go_Op<"make_closure", [MemoryEffects<[MemAlloc<DefaultResource>]>]> {
  let summary = "allocate a closure object {code, captures...}";
  let arguments = (ins FlatSymbolRefAttr:$fn, FlatSymbolRefAttr:$desc,
                       Variadic<AnyType>:$captures);
  let results = (outs LLVM_AnyPointer:$result);
  let assemblyFormat = [{
    $fn `(` $captures `)` `,` $desc attr-dict `:` functional-type($captures, $result)
  }];
  let extraClassDeclaration = [{
    /// The LLVM struct type of the closure object.
    ::mlir::LLVM::LLVMStructType getEnvType();
  }];
}

def Go_BoxOp : Go_Op<"box", [MemoryEffects<[MemAlloc<DefaultResource>]>,
                             DeclareOpInterfaceMethods<InferTypeOpInterface>]> {
  let summary = "copy a value to a new heap object (interface data word)";
  let arguments = (ins FlatSymbolRefAttr:$desc, AnyType:$value);
  let results = (outs LLVM_AnyPointer:$result);
  let assemblyFormat = "$desc `,` $value attr-dict `:` type($value)";
}

#endif
