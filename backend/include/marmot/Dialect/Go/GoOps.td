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
  let results = (outs LLVM_DefaultPointer:$result);
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
  let results = (outs LLVM_DefaultPointer:$result);
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
  let results = (outs LLVM_DefaultPointer:$result);
  let assemblyFormat = "$desc `,` $value attr-dict `:` type($value)";
}

//===----------------------------------------------------------------------===//
// Indexing family
//===----------------------------------------------------------------------===//

def Go_BoundsCheckOp : Go_Op<"bounds_check", [AllTypesMatch<["index", "len"]>]> {
  let summary = "panic unless 0 <= index < len";
  let arguments = (ins AnySignlessInteger:$index, AnySignlessInteger:$len);
  let assemblyFormat = "$index `,` $len attr-dict `:` type($index)";
}

def Go_NilCheckOp : Go_Op<"nil_check"> {
  let summary = "panic if the pointer is nil";
  let arguments = (ins LLVM_DefaultPointer:$ptr);
  let assemblyFormat = "$ptr attr-dict";
}

def Go_IndexOp : Go_Op<"index", [AllTypesMatch<["index", "len"]>,
                                 DeclareOpInterfaceMethods<InferTypeOpInterface>]> {
  let summary = "bounds checked element address &base[index]";
  let arguments = (ins LLVM_DefaultPointer:$base, AnySignlessInteger:$index,
                       AnySignlessInteger:$len, TypeAttr:$elem_type);
  let results = (outs LLVM_DefaultPointer:$result);
  let assemblyFormat = [{
    $base `[` $index `]` `len` $len `,` $elem_type attr-dict `:` type($index)
  }];
  let hasVerifier = 1;
}

def Go_SliceOp : Go_Op<"slice", [AllTypesMatch<["lo", "hi", "cap"]>,
    TypesMatchWith<"max has the type of lo", "lo", "max", "$_self">]> {
  let summary = "bounds checked slice expression base[lo:hi] or base[lo:hi:max]";
  let arguments = (ins LLVM_DefaultPointer:$base, AnySignlessInteger:$lo,
                       AnySignlessInteger:$hi, Optional<AnySignlessInteger>:$max,
                       AnySignlessInteger:$cap, TypeAttr:$elem_type);
  let results = (outs LLVM_AnyStruct:$result);
  let assemblyFormat = [{
    $base `[` $lo `:` $hi (`:` $max^)? `]` `cap` $cap `,` $elem_type attr-dict
    `:` type($lo) `->` type($result)
  }];
  let hasVerifier = 1;
}

#endif
