#ifndef MARMOT_GO_OPS
#define MARMOT_GO_OPS

include "marmot/Dialect/Go/GoDialect.td"
include "mlir/Dialect/LLVMIR/LLVMTypes.td"
include "mlir/Interfaces/SideEffectInterfaces.td"
include "mlir/IR/SymbolInterfaces.td"
include "mlir/Interfaces/InferTypeOpInterface.td"
include "mlir/Interfaces/ControlFlowInterfaces.td"

//===----------------------------------------------------------------------===//
// Allocation family
//===----------------------------------------------------------------------===//

def Go_NewOp : Go_Op<"new", [MemoryEffects<[MemAlloc<DefaultResource>]>,
                             DeclareOpInterfaceMethods<InferTypeOpInterface>]> {
  let summary = "allocate a zeroed value (new(T), escaping locals)";
  let arguments = (ins FlatSymbolRefAttr:$desc, TypeAttr:$elem_type, UnitAttr:$stack);
  let results = (outs LLVM_DefaultPointer:$result);
  let assemblyFormat = "$desc `,` $elem_type (`stack` $stack^)? attr-dict";
  let hasVerifier = 1;
}

def Go_MakeSliceOp : Go_Op<"make_slice", [AllTypesMatch<["len", "cap"]>,
    DeclareOpInterfaceMethods<InferTypeOpInterface>]> {
  let summary = "make([]T, len, cap)";
  let arguments = (ins FlatSymbolRefAttr:$desc, TypeAttr:$elem_type,
                       AnySignlessInteger:$len, AnySignlessInteger:$cap, UnitAttr:$stack);
  let results = (outs LLVM_AnyStruct:$result);
  let assemblyFormat = [{
    $desc `,` $elem_type `(` $len `,` $cap `)` (`stack` $stack^)? attr-dict `:` type($len)
  }];
  let hasVerifier = 1;
}

def Go_MakeClosureOp : Go_Op<"make_closure", [MemoryEffects<[MemAlloc<DefaultResource>]>]> {
  let summary = "allocate a closure object {code, captures...}";
  let arguments = (ins FlatSymbolRefAttr:$fn, FlatSymbolRefAttr:$desc,
                       Variadic<AnyType>:$captures, UnitAttr:$stack);
  let results = (outs LLVM_DefaultPointer:$result);
  let assemblyFormat = [{
    $fn `(` $captures `)` `,` $desc (`stack` $stack^)? attr-dict
    `:` functional-type($captures, $result)
  }];
  let extraClassDeclaration = [{
    /// The LLVM struct type of the closure object.
    ::mlir::LLVM::LLVMStructType getEnvType();
  }];
}

def Go_BoxOp : Go_Op<"box", [MemoryEffects<[MemAlloc<DefaultResource>]>,
                             DeclareOpInterfaceMethods<InferTypeOpInterface>]> {
  let summary = "copy a value to a new heap object (interface data word)";
  let arguments = (ins FlatSymbolRefAttr:$desc, AnyType:$value, UnitAttr:$stack);
  let results = (outs LLVM_DefaultPointer:$result);
  let assemblyFormat = "$desc `,` $value (`stack` $stack^)? attr-dict `:` type($value)";
}

//===----------------------------------------------------------------------===//
// Indexing family
//===----------------------------------------------------------------------===//

def Go_BoundsCheckOp : Go_Op<"bounds_check", [AllTypesMatch<["index", "len"]>]> {
  let summary = "panic unless 0 <= index < len";
  let arguments = (ins AnySignlessInteger:$index, AnySignlessInteger:$len,
                       UnitAttr:$unsigned_index);
  let assemblyFormat = "$index `,` $len (`unsigned` $unsigned_index^)? attr-dict `:` type($index)";
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
                       AnySignlessInteger:$len, TypeAttr:$elem_type, UnitAttr:$unsigned_index,
                       UnitAttr:$nocheck);
  let results = (outs LLVM_DefaultPointer:$result);
  let assemblyFormat = [{
    $base `[` $index `]` `len` $len `,` $elem_type (`unsigned` $unsigned_index^)?
    (`nocheck` $nocheck^)? attr-dict `:` type($index)
  }];
  let hasVerifier = 1;
}

def Go_SliceOp : Go_Op<"slice", [AllTypesMatch<["lo", "hi", "cap"]>,
    TypesMatchWith<"max has the type of lo", "lo", "max", "$_self">]> {
  let summary = "bounds checked slice expression base[lo:hi] or base[lo:hi:max]";
  let arguments = (ins LLVM_DefaultPointer:$base, AnySignlessInteger:$lo,
                       AnySignlessInteger:$hi, Optional<AnySignlessInteger>:$max,
                       AnySignlessInteger:$cap, TypeAttr:$elem_type,
                       UnitAttr:$len_bound, UnitAttr:$unsigned_index);
  let results = (outs LLVM_AnyStruct:$result);
  let assemblyFormat = [{
    $base `[` $lo `:` $hi (`:` $max^)? `]` `cap` $cap `,` $elem_type
    (`len_bound` $len_bound^)? (`unsigned` $unsigned_index^)? attr-dict
    `:` type($lo) `->` type($result)
  }];
  let hasVerifier = 1;
}

//===----------------------------------------------------------------------===//
// Interfaces family
//===----------------------------------------------------------------------===//

def Go_IfaceType : Type<CPred<"::marmot::go::isIfaceType($_self)">,
    "Go interface value !llvm.struct<(ptr, ptr)>", "::mlir::LLVM::LLVMStructType">,
    BuildableType<"::marmot::go::ifaceType($_builder.getContext())">;

def Go_MakeIfaceOp : Go_Op<"make_iface", [Pure,
    DeclareOpInterfaceMethods<InferTypeOpInterface>]> {
  let summary = "build an interface value {tab, data}";
  let arguments = (ins FlatSymbolRefAttr:$tab, LLVM_DefaultPointer:$data);
  let results = (outs Go_IfaceType:$result);
  let assemblyFormat = "$tab `,` $data attr-dict";
}

def Go_IfaceCallOp : Go_Op<"iface_call"> {
  let summary = "call method number `method` of a non-empty interface value";
  let arguments = (ins Go_IfaceType:$iface, I64Attr:$method,
                       Variadic<AnyType>:$args);
  let results = (outs Variadic<AnyType>:$results);
  let assemblyFormat = [{
    $iface `[` $method `]` `` `(` $args `)` attr-dict `:` functional-type($args, $results)
  }];
  let hasVerifier = 1;
}

def Go_TypeAssertOp : Go_Op<"type_assert", [InferTypeOpAdaptor]> {
  let summary = "type assertion x.(T), optionally with comma-ok";
  let arguments = (ins Go_IfaceType:$x, FlatSymbolRefAttr:$src,
                       FlatSymbolRefAttr:$desc, UnitAttr:$from_iface,
                       UnitAttr:$to_iface, UnitAttr:$to_any, UnitAttr:$commaok);
  let results = (outs AnyType:$result, Optional<I1>:$ok);
  let assemblyFormat = [{
    $x `,` $src `->` $desc (`from_iface` $from_iface^)? (`to_iface` $to_iface^)?
    (`to_any` $to_any^)? (`commaok` $commaok^)? attr-dict
  }];
  let hasVerifier = 1;
}

//===----------------------------------------------------------------------===//
// Control family
//===----------------------------------------------------------------------===//

def Go_DeferFrameOp : Go_Op<"defer_frame", [Terminator,
    DeclareOpInterfaceMethods<BranchOpInterface>]> {
  let summary = "set up the defer frame of a function; second entry after recover";
  let arguments = (ins UnitAttr:$norecover);
  let successors = (successor AnySuccessor:$normal, AnySuccessor:$recover);
  let assemblyFormat = "$normal `,` $recover (`norecover` $norecover^)? attr-dict";
  let hasVerifier = 1;
}

def Go_DeferOp : Go_Op<"defer"> {
  let summary = "push a deferred call of a func() value";
  let arguments = (ins LLVM_DefaultPointer:$fn, UnitAttr:$wrapped);
  let assemblyFormat = "$fn (`wrapped` $wrapped^)? attr-dict";
}

def Go_RunDefersOp : Go_Op<"run_defers"> {
  let summary = "run and pop the deferred calls of this frame";
  let assemblyFormat = "attr-dict";
}

def Go_GoOp : Go_Op<"go"> {
  let summary = "start a goroutine running a func() value";
  let arguments = (ins LLVM_DefaultPointer:$fn);
  let assemblyFormat = "$fn attr-dict";
}

def Go_PanicOp : Go_Op<"panic", [Terminator]> {
  let summary = "panic(v)";
  let arguments = (ins Go_IfaceType:$value);
  let assemblyFormat = "$value attr-dict";
}

def Go_RecoverOp : Go_Op<"recover"> {
  let summary = "recover()";
  let results = (outs Go_IfaceType:$result);
  let assemblyFormat = "attr-dict";
}

//===----------------------------------------------------------------------===//
// Strings family
//===----------------------------------------------------------------------===//

def Go_StringType : Type<CPred<"::marmot::go::isStringType($_self)">,
    "Go string !llvm.struct<(ptr, i64)>", "::mlir::LLVM::LLVMStructType">,
    BuildableType<"::marmot::go::stringType($_builder.getContext())">;

def Go_SliceType : Type<CPred<"::marmot::go::isSliceType($_self)">,
    "Go slice !llvm.struct<(ptr, i64, i64)>", "::mlir::LLVM::LLVMStructType">,
    BuildableType<"::marmot::go::sliceType($_builder.getContext())">;

def Go_StrConcatOp : Go_Op<"str_concat"> {
  let summary = "concatenate two or more strings";
  let arguments = (ins Variadic<Go_StringType>:$parts);
  let results = (outs Go_StringType:$result);
  let assemblyFormat = "$parts attr-dict";
  let hasVerifier = 1;
}

def Go_StrToBytesOp : Go_Op<"str_to_bytes"> {
  let summary = "[]byte(s), a fresh copy";
  let arguments = (ins Go_StringType:$str);
  let results = (outs Go_SliceType:$result);
  let assemblyFormat = "$str attr-dict";
}

def Go_BytesToStrOp : Go_Op<"bytes_to_str"> {
  let summary = "string(b), a copy unless `nocopy`";
  let arguments = (ins Go_SliceType:$bytes, UnitAttr:$nocopy);
  let results = (outs Go_StringType:$result);
  let assemblyFormat = "$bytes (`nocopy` $nocopy^)? attr-dict";
}

//===----------------------------------------------------------------------===//
// Maps and channels family
//===----------------------------------------------------------------------===//

def Go_MakeMapOp : Go_Op<"make_map"> {
  let summary = "make(map[K]V, hint)";
  let arguments = (ins FlatSymbolRefAttr:$desc, I64:$hint);
  let results = (outs LLVM_DefaultPointer:$result);
  let assemblyFormat = "$desc `,` $hint attr-dict";
}

def Go_MapLookupOp : Go_Op<"map_lookup", [InferTypeOpAdaptor]> {
  let summary = "m[k], optionally with comma-ok";
  let arguments = (ins FlatSymbolRefAttr:$desc, LLVM_DefaultPointer:$map,
                       AnyType:$key, TypeAttr:$value_type, UnitAttr:$commaok);
  let results = (outs AnyType:$result, Optional<I1>:$ok);
  let assemblyFormat = [{
    $map `[` $key `]` `,` $desc (`commaok` $commaok^)? attr-dict
    `:` type($key) `->` $value_type
  }];
}

def Go_MapUpdateOp : Go_Op<"map_update"> {
  let summary = "m[k] = v";
  let arguments = (ins FlatSymbolRefAttr:$desc, LLVM_DefaultPointer:$map,
                       AnyType:$key, AnyType:$value);
  let assemblyFormat = [{
    $map `[` $key `]` `=` $value `,` $desc attr-dict `:` type($key) `,` type($value)
  }];
}

def Go_MapDeleteOp : Go_Op<"map_delete"> {
  let summary = "delete(m, k)";
  let arguments = (ins FlatSymbolRefAttr:$desc, LLVM_DefaultPointer:$map, AnyType:$key);
  let assemblyFormat = "$map `[` $key `]` `,` $desc attr-dict `:` type($key)";
}

def Go_MakeChanOp : Go_Op<"make_chan"> {
  let summary = "make(chan T, size)";
  let arguments = (ins FlatSymbolRefAttr:$desc, I64:$size);
  let results = (outs LLVM_DefaultPointer:$result);
  let assemblyFormat = "$desc `,` $size attr-dict";
}

def Go_ChanSendOp : Go_Op<"chan_send"> {
  let summary = "c <- v";
  let arguments = (ins LLVM_DefaultPointer:$chan, AnyType:$value);
  let assemblyFormat = "$chan `,` $value attr-dict `:` type($value)";
}

def Go_ChanRecvOp : Go_Op<"chan_recv", [InferTypeOpAdaptor]> {
  let summary = "<-c, optionally with comma-ok";
  let arguments = (ins LLVM_DefaultPointer:$chan, TypeAttr:$value_type,
                       UnitAttr:$commaok);
  let results = (outs AnyType:$result, Optional<I1>:$ok);
  let assemblyFormat = "$chan (`commaok` $commaok^)? attr-dict `:` $value_type";
}

#endif
