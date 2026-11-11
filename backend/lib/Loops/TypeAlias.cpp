// TypeAlias.cpp: go-type-alias, type based alias information (Rule T in
// docs/loops.md).
//
// In Go without unsafe, a memory location is only accessed with its own type,
// a type with the same underlying type, or (for structs and arrays) a
// component type. So two accesses whose LLVM value types share no scalar
// class cannot touch the same bytes:
//
//   classes: f64, f32, i64, i32, i16, i8, ptr
//   a scalar access has one class; an aggregate access (struct, array) has
//   the set of classes of its scalar components.
//
// The frontend marks every access that may break this (pointers produced by
// an unsafe.Pointer conversion) with `go.unsafe`; those, atomics, volatiles
// and anything of another type get no classes and alias everything.
//
// The classes are written as scoped alias metadata in one domain: an access
// with class set S gets alias_scopes = {scope(c) : c in S} and
// noalias_scopes = {scope(c) : c not in S}. LLVM's scoped alias analysis
// then answers NoAlias exactly when the two class sets are disjoint.
//
// Runs on the llvm dialect after go-to-llvm (so lowered go ops are covered
// too; the loads they produce have no classes unless this pass gives them).

#include "marmot/Loops/Loops.h"

#include "mlir/Dialect/LLVMIR/LLVMDialect.h"
#include "mlir/IR/BuiltinOps.h"
#include "mlir/Pass/Pass.h"
#include "mlir/Pass/PassManager.h"

using namespace mlir;

namespace marmot {
namespace loops {
namespace {

enum Class : unsigned { F64, F32, I64, I32, I16, I8, Ptr, NumClasses };

// Adds the classes of t to set. False for types without a class.
bool classesOf(Type t, unsigned &set) {
  if (t.isF64()) {
    set |= 1u << F64;
  } else if (t.isF32()) {
    set |= 1u << F32;
  } else if (auto it = dyn_cast<IntegerType>(t)) {
    switch (it.getWidth()) {
    case 64: set |= 1u << I64; break;
    case 32: set |= 1u << I32; break;
    case 16: set |= 1u << I16; break;
    case 8:
    case 1: set |= 1u << I8; break;
    default: return false;
    }
  } else if (isa<LLVM::LLVMPointerType>(t)) {
    set |= 1u << Ptr;
  } else if (auto st = dyn_cast<LLVM::LLVMStructType>(t)) {
    if (st.isOpaque())
      return false;
    for (Type e : st.getBody())
      if (!classesOf(e, set))
        return false;
  } else if (auto at = dyn_cast<LLVM::LLVMArrayType>(t)) {
    return classesOf(at.getElementType(), set);
  } else {
    return false;
  }
  return true;
}

struct TypeAliasPass : public PassWrapper<TypeAliasPass, OperationPass<ModuleOp>> {
  MLIR_DEFINE_EXPLICIT_INTERNAL_INLINE_TYPE_ID(TypeAliasPass)
  StringRef getArgument() const override { return "go-type-alias"; }
  StringRef getDescription() const override {
    return "Scoped alias metadata from Go type classes";
  }

  void runOnOperation() override {
    MLIRContext *ctx = &getContext();
    auto domain = LLVM::AliasScopeDomainAttr::get(ctx, StringAttr::get(ctx, "go.types"));
    static const char *names[NumClasses] = {"f64", "f32", "i64", "i32", "i16", "i8", "ptr"};
    SmallVector<Attribute, NumClasses> scopes;
    for (unsigned c = 0; c < NumClasses; c++)
      scopes.push_back(LLVM::AliasScopeAttr::get(domain, StringAttr::get(ctx, names[c])));
    // One pair of attributes per class set, shared by all accesses.
    DenseMap<unsigned, std::pair<ArrayAttr, ArrayAttr>> cache;
    auto attrsFor = [&](unsigned set) {
      auto it = cache.find(set);
      if (it != cache.end())
        return it->second;
      SmallVector<Attribute, NumClasses> in, out;
      for (unsigned c = 0; c < NumClasses; c++)
        (set & (1u << c) ? in : out).push_back(scopes[c]);
      auto p = std::make_pair(ArrayAttr::get(ctx, in), ArrayAttr::get(ctx, out));
      cache[set] = p;
      return p;
    };
    auto tag = [&](Operation *op, Type t) {
      if (op->hasAttr("go.unsafe"))
        return;
      // Do not combine with scopes someone else attached.
      auto aa = cast<LLVM::AliasAnalysisOpInterface>(op);
      if (aa.getAliasScopesOrNull() || aa.getNoAliasScopesOrNull())
        return;
      unsigned set = 0;
      if (!classesOf(t, set) || set == 0)
        return;
      auto [in, out] = attrsFor(set);
      aa.setAliasScopes(in);
      if (!out.empty())
        aa.setNoAliasScopes(out);
    };
    getOperation().walk([&](Operation *op) {
      if (auto ld = dyn_cast<LLVM::LoadOp>(op)) {
        if (!ld.getVolatile_() && ld.getOrdering() == LLVM::AtomicOrdering::not_atomic)
          tag(op, ld.getResult().getType());
      } else if (auto st = dyn_cast<LLVM::StoreOp>(op)) {
        if (!st.getVolatile_() && st.getOrdering() == LLVM::AtomicOrdering::not_atomic)
          tag(op, st.getValue().getType());
      }
    });
  }
};

} // namespace

void addMLIRPasses(OpPassManager &pm, const Options &opts) {
  if (!opts.enable || !opts.typeAlias)
    return;
  pm.addPass(std::make_unique<TypeAliasPass>());
}

} // namespace loops
} // namespace marmot
