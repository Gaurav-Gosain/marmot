// Module.cpp: see Module.h.
#include "Module.h"

#include "mlir/IR/AttrTypeSubElements.h"
#include "llvm/Support/Format.h"
#include "llvm/Support/raw_ostream.h"

#include <cstring>
#include <memory>
#include <thread>

namespace marmot {
namespace tier0 {

namespace {

// ---- constant evaluation for global initializers ----

struct ConstVal {
  std::vector<uint8_t> bytes;
  struct Rel {
    uint64_t off;
    std::string sym;
    int64_t addend;
  };
  std::vector<Rel> rels;
};

void putInt(std::vector<uint8_t> &b, uint64_t off, uint64_t v, uint64_t n) {
  for (uint64_t i = 0; i < n; i++) b[off + i] = uint8_t(v >> (8 * i));
}
void putAPInt(std::vector<uint8_t> &b, uint64_t off, const APInt &v, uint64_t n) {
  for (uint64_t i = 0; i < n; i += 8)
    putInt(b, off + i, v.zextOrTrunc(128).extractBitsAsZExtValue(64, unsigned(8 * i)), std::min<uint64_t>(8, n - i));
}

void attrBytes(Attribute a, Type t, std::vector<uint8_t> &b, uint64_t off) {
  if (auto i = dyn_cast<IntegerAttr>(a)) {
    putAPInt(b, off, i.getValue(), sizeOf(t));
    return;
  }
  if (auto f = dyn_cast<FloatAttr>(a)) {
    uint64_t bits = f.getValue().bitcastToAPInt().getZExtValue();
    putInt(b, off, bits, sizeOf(t));
    return;
  }
  if (auto s = dyn_cast<StringAttr>(a)) {
    for (size_t i = 0; i < s.getValue().size() && off + i < b.size(); i++) b[off + i] = uint8_t(s.getValue()[i]);
    return;
  }
  if (auto d = dyn_cast<DenseElementsAttr>(a)) {
    auto at = dyn_cast<LL::LLVMArrayType>(t);
    if (!at) unsupported("dense constant of non-array type");
    Type et = at.getElementType();
    uint64_t es = sizeOf(et), i = 0;
    if (isa<IntegerType>(et)) {
      for (const APInt &v : d.getValues<APInt>()) putAPInt(b, off + es * i++, v, es);
    } else if (isFloat(et)) {
      for (const APFloat &v : d.getValues<APFloat>())
        putInt(b, off + es * i++, v.bitcastToAPInt().getZExtValue(), es);
    } else {
      unsupported("dense constant element type");
    }
    return;
  }
  if (auto arr = dyn_cast<ArrayAttr>(a)) {
    auto at = dyn_cast<LL::LLVMArrayType>(t);
    if (!at) unsupported("array attribute for non-array");
    uint64_t es = sizeOf(at.getElementType());
    for (size_t i = 0; i < arr.size(); i++) attrBytes(arr[i], at.getElementType(), b, off + es * i);
    return;
  }
  if (isa<LL::ZeroAttr>(a) || isa<LL::UndefAttr>(a)) return;
  unsupported("constant attribute");
}

ConstVal evalInitializer(LL::GlobalOp g) {
  Type t = g.getGlobalType();
  ConstVal out;
  out.bytes.assign(sizeOf(t), 0);
  if (auto v = g.getValueOrNull()) {
    attrBytes(v, t, out.bytes, 0);
    return out;
  }
  Region &r = g.getInitializerRegion();
  if (r.empty()) return out;
  llvm::DenseMap<Value, ConstVal> vals;
  // A value's bytes, moved out when this is its only use, else copied.
  auto take = [&](Value v) -> ConstVal {
    auto it = vals.find(v);
    if (it == vals.end()) return ConstVal();
    if (!v.hasOneUse()) return it->second;
    ConstVal c = std::move(it->second);
    vals.erase(it);
    return c;
  };
  for (Operation &op : r.front()) {
    llvm::TypeSwitch<Operation *>(&op)
        .Case<LL::ConstantOp>([&](auto c) {
          ConstVal cv;
          cv.bytes.assign(sizeOf(c.getType()), 0);
          attrBytes(c.getValue(), c.getType(), cv.bytes, 0);
          vals[c.getResult()] = std::move(cv);
        })
        .Case<LL::ZeroOp, LL::UndefOp, LL::PoisonOp>([&](auto z) {
          ConstVal cv;
          cv.bytes.assign(sizeOf(z.getType()), 0);
          vals[z.getResult()] = std::move(cv);
        })
        .Case<LL::AddressOfOp>([&](auto a) {
          ConstVal cv;
          cv.bytes.assign(8, 0);
          cv.rels.push_back({0, machoName(a.getGlobalName()), 0});
          vals[a.getResult()] = std::move(cv);
        })
        .Case<LL::GEPOp>([&](auto gep) {
          auto base = vals.find(gep.getBase());
          if (base == vals.end() || base->second.rels.size() != 1) unsupported("constant gep base");
          int64_t off = 0;
          Type et = gep.getElemType();
          bool first = true;
          for (auto idx : gep.getIndices()) {
            auto ia = llvm::dyn_cast_if_present<IntegerAttr>(idx);
            if (!ia) unsupported("constant gep with a dynamic index");
            int64_t k = ia.getInt();
            if (first) {
              off += k * int64_t(sizeOf(et));
              first = false;
            } else if (auto s = dyn_cast<LL::LLVMStructType>(et)) {
              off += int64_t(fieldOffset(s, unsigned(k)));
              et = s.getBody()[k];
            } else if (auto at = dyn_cast<LL::LLVMArrayType>(et)) {
              off += k * int64_t(sizeOf(at.getElementType()));
              et = at.getElementType();
            }
          }
          ConstVal cv = take(gep.getBase());
          cv.rels[0].addend += off;
          vals[gep.getResult()] = std::move(cv);
        })
        .Case<LL::InsertValueOp>([&](auto ins) {
          // The container is moved along the chain when this is its only
          // use: building a large table stays linear.
          ConstVal cv = take(ins.getContainer());
          if (cv.bytes.empty()) cv.bytes.assign(sizeOf(ins.getType()), 0);
          auto [off, ft] = elementAt(ins.getType(), ins.getPosition());
          ConstVal v = take(ins.getValue());
          std::copy(v.bytes.begin(), v.bytes.end(), cv.bytes.begin() + off);
          for (auto &rel : v.rels) cv.rels.push_back({off + rel.off, std::move(rel.sym), rel.addend});
          (void)ft;
          vals[ins.getResult()] = std::move(cv);
        })
        .Case<LL::PtrToIntOp, LL::IntToPtrOp, LL::BitcastOp, LL::AddrSpaceCastOp>([&](auto c) {
          // The same 8 bytes (an address stays a relocation).
          if (sizeOf(c.getType()) != 8 || sizeOf(c.getArg().getType()) != 8) unsupported("narrowing constant cast");
          vals[c.getResult()] = take(c.getArg());
        })
        .Case<LL::ReturnOp>([&](auto ret) { out = take(ret.getOperand(0)); })
        .Default([&](Operation *o) { unsupported("initializer op " + o->getName().getStringRef().str()); });
  }
  return out;
}

// llvm.used and llvm.compiler.used: lists of symbols to keep, not data.
bool isUsedList(LL::GlobalOp g) {
  StringRef n = g.getSymName();
  return n == "llvm.used" || n == "llvm.compiler.used";
}


} // namespace

void ModuleGen::emitGlobal(LL::GlobalOp g) {
  if (g.getThreadLocal_()) unsupported("thread local global");
  if (g.getSymName().starts_with("llvm.")) unsupported("special global " + g.getSymName().str());
  ConstVal cv = evalInitializer(g);
  std::string name = machoName(g.getSymName());
  uint64_t align = std::max<uint64_t>(g.getAlignment().value_or(0), alignOf(g.getGlobalType()));
  bool zero = cv.rels.empty() && std::all_of(cv.bytes.begin(), cv.bytes.end(), [](uint8_t b) { return b == 0; });
  bool local = isLocalLinkage(g.getLinkage()), weak = isWeakLinkage(g.getLinkage());
  if (g.getLinkage() == LL::Linkage::Common && zero && !local) {
    unsigned l2 = 0;
    while ((uint64_t(1) << l2) < align) l2++;
    w.common(name, cv.bytes.size(), l2);
    return;
  }
  if (zero && !g.getConstant() && !weak) {
    w.reserveBss(0, unsigned(align));
    w.define(name, Sect::Bss, w.size(Sect::Bss), !local, false, isHidden(g));
    w.reserveBss(std::max<uint64_t>(cv.bytes.size(), 1), 1);
    return;
  }
  // Read-only data without pointers can live in __TEXT,__const; anything
  // with relocations goes to __DATA. The only named section is the type
  // link table.
  Sect s = g.getConstant() && cv.rels.empty() ? Sect::Const : Sect::Data;
  if (auto sec = g.getSection()) {
    StringRef n = *sec;
    if (n != "marmot_tlinks" && !n.starts_with("__DATA,__marmot_tlinks")) unsupported("section " + n.str());
    s = Sect::TLinks;
  }
  w.align(s, unsigned(align));
  uint64_t at = w.size(s);
  w.define(name, s, at, !local, weak, isHidden(g));
  w.emitData(s, cv.bytes.data(), cv.bytes.size());
  if (cv.bytes.empty()) {
    uint8_t z = 0;
    w.emitData(s, &z, 1);
  }
  for (auto &r : cv.rels) w.reloc(s, at + r.off, Reloc::Abs64, w.symbol(r.sym), r.addend);
}

// Dev builds skip the MLIR verifier for modules tier0 compiles, so tier0
// checks what it relies on before it emits anything for a function: every
// value has a type it knows (integers of at most 64 bits or 128, pointers,
// f32, f64, and structs and arrays of them), and every direct call matches
// its callee's signature. Anything else leaves the function to LLVM, which
// verifies the module first: an invalid module is reported there with the
// operation and its location, never compiled.
static bool knownValueType(Type t) {
  if (auto i = dyn_cast<IntegerType>(t)) return i.getWidth() >= 1 && (i.getWidth() <= 64 || i.getWidth() == 128);
  if (isa<LL::LLVMPointerType>(t) || t.isF32() || t.isF64()) return true;
  if (auto s = dyn_cast<LL::LLVMStructType>(t)) {
    if (s.isOpaque()) return false;
    return llvm::all_of(s.getBody(), knownValueType);
  }
  if (auto a = dyn_cast<LL::LLVMArrayType>(t)) return knownValueType(a.getElementType());
  return false;
}

void ModuleGen::checkFunction(LL::LLVMFuncOp fn) {
  auto check = [&](Type t) {
    if (knownTypes.contains(t)) return;
    if (!knownValueType(t)) unsupported("unexpected value type");
    knownTypes.insert(t);
  };
  fn.walk([&](Operation *op) {
    for (Type t : op->getResultTypes()) check(t);
    for (Value v : op->getOperands()) check(v.getType());
    for (Region &rg : op->getRegions())
      for (Block &b : rg)
        for (BlockArgument a : b.getArguments()) check(a.getType());
    if (auto call = dyn_cast<LL::CallOp>(op)) {
      if (auto callee = call.getCalleeAttr()) {
        auto it = funcs.find(callee.getAttr());
        if (it == funcs.end() || it->second.getFunctionType() != call.getCalleeFunctionType())
          unsupported("call that does not match its callee");
      }
    }
  });
}

// Every function definition, into this writer or, with threads > 1, into a
// writer per thread for a contiguous range of functions (balanced by
// operation count), merged in module order: the object is the same as from
// one thread. Functions only read the module and the read-only tables of
// this ModuleGen; each part has its own writer, line rows and type cache.
void ModuleGen::emitFunctions(std::vector<std::string> &fallback, std::vector<std::string> &reasons,
                              Stats &stats) {
  std::vector<LL::LLVMFuncOp> fns;
  std::vector<size_t> weight;
  size_t total = 0;
  for (Operation &op : module.getBody()->getOperations())
    if (auto f = dyn_cast<LL::LLVMFuncOp>(op); f && !f.isExternal()) {
      size_t n = 0;
      for (Block &blk : f.getBody()) n += blk.getOperations().size();
      fns.push_back(f);
      weight.push_back(n);
      total += n;
    }
  auto one = [&](ModuleGen &g, LL::LLVMFuncOp f, std::vector<std::string> &fb, std::vector<std::string> &rs,
                 Stats &st) {
    // A function that falls back leaves no code or relocations behind.
    auto cp = g.w.checkpoint();
    try {
      if (x64)
        runX64(g, f);
      else
        runArm64(g, f);
      st.functions++;
    } catch (const Unsupported &u) {
      g.w.rollback(cp);
      fb.push_back(f.getSymName().str());
      rs.push_back(u.why);
      st.fallbacks++;
    }
  };
  unsigned n = threads;
  if (n > fns.size() / 8) n = unsigned(fns.size() / 8);
  if (n <= 1 || total < 20000) {
    for (LL::LLVMFuncOp f : fns) one(*this, f, fallback, reasons, stats);
    return;
  }
  struct Part {
    std::unique_ptr<ModuleGen> g;
    size_t lo, hi;
    std::vector<std::string> fallback, reasons;
    Stats stats;
  };
  std::vector<Part> parts(n);
  size_t at = 0, acc = 0;
  for (unsigned k = 0; k < n; k++) {
    Part &pt = parts[k];
    pt.lo = at;
    size_t goal = total * (k + 1) / n;
    while (at < fns.size() && (acc < goal || k == n - 1)) acc += weight[at++];
    pt.hi = at;
    pt.g = std::make_unique<ModuleGen>(module, unit, x64);
    pt.g->definedData = definedData;
    pt.g->definedNames = definedNames;
    pt.g->returnsTwice = returnsTwice;
    pt.g->funcs = funcs;
  }
  // Accessors such as CallOp::getCalleeFunctionType may unique types.
  bool threaded = ctx->isMultithreadingEnabled();
  if (!threaded) ctx->enableMultithreading();
  {
    std::vector<std::thread> ts;
    for (Part &pt : parts)
      ts.emplace_back([&, pp = &pt] {
        elfNames = x64;  // thread_local: each thread names symbols for the target
        for (size_t i = pp->lo; i < pp->hi; i++) one(*pp->g, fns[i], pp->fallback, pp->reasons, pp->stats);
      });
    for (std::thread &t : ts) t.join();
  }
  if (!threaded) ctx->disableMultithreading();
  std::vector<unsigned> symMap;
  for (Part &pt : parts) {
    w.appendText(pt.g->w, symMap);
    for (Lines &l : pt.g->lines) {
      std::vector<Row> rows = std::move(l.rows);
      for (auto &[off, line, file] : rows) file = fileId(pt.g->files[file]);
      lines.push_back({symMap[l.sym], l.size, std::move(rows), std::move(l.maps), std::move(l.sites)});
    }
    fallback.insert(fallback.end(), pt.fallback.begin(), pt.fallback.end());
    reasons.insert(reasons.end(), pt.reasons.begin(), pt.reasons.end());
    stats.functions += pt.stats.functions;
    stats.fallbacks += pt.stats.fallbacks;
  }
}

bool ModuleGen::isDefinedData(StringRef name) const {
  return definedNames.contains(name);
}

// Removes discardable definitions (linkonce, linkonce_odr, internal,
// private) that no other definition reaches, as LLVM does: unused method
// value thunks, for example, may refer to functions nothing defines.
static void dropUnused(ModuleOp module) {
  auto discardable = [](LL::Linkage l) {
    return l == LL::Linkage::Linkonce || l == LL::Linkage::LinkonceODR || l == LL::Linkage::Internal ||
           l == LL::Linkage::Private || l == LL::Linkage::AvailableExternally;
  };
  llvm::DenseMap<StringAttr, Operation *> syms;
  std::vector<Operation *> work;
  for (Operation &op : module.getBody()->getOperations()) {
    LL::Linkage l;
    StringAttr name;
    if (auto f = dyn_cast<LL::LLVMFuncOp>(op)) {
      l = f.getLinkage();
      name = f.getSymNameAttr();
    } else if (auto g = dyn_cast<LL::GlobalOp>(op)) {
      l = g.getLinkage();
      name = g.getSymNameAttr();
    } else {
      // Aliases, comdats and anything else stay, and keep what they use.
      work.push_back(&op);
      continue;
    }
    syms[name] = &op;
    if (!discardable(l)) work.push_back(&op);
  }
  llvm::DenseMap<Operation *, bool> live;
  for (Operation *op : work) live[op] = true;
  while (!work.empty()) {
    Operation *op = work.back();
    work.pop_back();
    auto mark = [&](StringAttr name) {
      auto it = syms.find(name);
      if (it != syms.end() && !live.count(it->second)) {
        live[it->second] = true;
        work.push_back(it->second);
      }
    };
    op->walk([&](Operation *inner) {
      // The operations of function bodies and initializers: calls and
      // addressof name symbols, the others name none. Anything else gets
      // the general walk over its attributes (slow: it visits every type).
      if (auto c = dyn_cast<LL::CallOp>(inner)) {
        if (auto callee = c.getCalleeAttr()) mark(callee.getAttr());
        return;
      }
      if (auto a = dyn_cast<LL::AddressOfOp>(inner)) {
        mark(a.getGlobalNameAttr().getAttr());
        return;
      }
      if (isa<LL::ConstantOp, LL::ICmpOp, LL::FCmpOp, LL::LoadOp, LL::StoreOp, LL::GEPOp, LL::InsertValueOp,
              LL::ExtractValueOp, LL::BrOp, LL::CondBrOp, LL::ReturnOp, LL::UnreachableOp, LL::ZeroOp,
              LL::UndefOp, LL::SelectOp, LL::AllocaOp, LL::AddOp, LL::SubOp, LL::MulOp, LL::AndOp, LL::OrOp,
              LL::XOrOp, LL::ShlOp, LL::LShrOp, LL::AShrOp, LL::SDivOp, LL::UDivOp, LL::SRemOp, LL::URemOp,
              LL::FAddOp, LL::FSubOp, LL::FMulOp, LL::FDivOp, LL::FNegOp, LL::ZExtOp, LL::SExtOp, LL::TruncOp,
              LL::PtrToIntOp, LL::IntToPtrOp, LL::AddrSpaceCastOp, LL::SIToFPOp, LL::UIToFPOp, LL::FPToSIOp, LL::FPToUIOp,
              LL::FPTruncOp, LL::FPExtOp, LL::FMAOp, LL::MinimumOp, LL::MaximumOp, LL::CallIntrinsicOp,
              LL::AtomicRMWOp, LL::AtomicCmpXchgOp, LL::MemmoveOp, LL::MemsetOp, LL::MemcpyOp>(inner))
        return;
      if (auto g = dyn_cast<LL::GlobalOp>(inner)) {
        // Its value is a string or a number; a comdat names a symbol.
        if (auto c = g.getComdatAttr()) mark(c.getRootReference());
        if (!g.getValueOrNull() || isa<StringAttr, IntegerAttr, FloatAttr>(g.getValueOrNull())) {
          for (NamedAttribute a : inner->getDiscardableAttrs())
            a.getValue().walk([&](FlatSymbolRefAttr r) { mark(r.getAttr()); });
          return;
        }
      }
      for (NamedAttribute a : inner->getAttrs()) a.getValue().walk([&](FlatSymbolRefAttr r) { mark(r.getAttr()); });
    });
  }
  std::vector<Operation *> dead;
  for (auto &[name, op] : syms)
    if (!live.count(op)) dead.push_back(op);
  for (Operation *op : dead) op->dropAllReferences();
  for (Operation *op : dead) op->erase();
}

// Internal and private symbols become hidden globals with names unique to
// unit, so the tier0 object and the LLVM object of the fallback functions
// can both refer to them.
// Internal and private symbols, renamed unique to unit: in a module with
// fallback functions both objects must reach them, as hidden globals.
static llvm::DenseMap<StringAttr, StringAttr> localNames(ModuleOp module, const std::string &unit) {
  MLIRContext *ctx = module.getContext();
  llvm::DenseMap<StringAttr, StringAttr> renamed;
  for (Operation &op : module.getBody()->getOperations()) {
    StringAttr old;
    if (auto f = dyn_cast<LL::LLVMFuncOp>(op)) {
      if (isLocalLinkage(f.getLinkage())) old = f.getSymNameAttr();
    } else if (auto g = dyn_cast<LL::GlobalOp>(op)) {
      if (isLocalLinkage(g.getLinkage())) old = g.getSymNameAttr();
    }
    if (old) renamed[old] = StringAttr::get(ctx, old.getValue() + ".t0." + unit);
  }
  return renamed;
}

static void shareLocals(ModuleOp module, const llvm::DenseMap<StringAttr, StringAttr> &renamed) {
  if (renamed.empty()) return;
  auto share = [&](auto op) {
    auto it = renamed.find(op.getSymNameAttr());
    if (it == renamed.end()) return;
    op.setSymName(it->second.getValue());
    op.setLinkage(LL::Linkage::External);
    op.setVisibility_(LL::Visibility::Hidden);
  };
  for (Operation &op : module.getBody()->getOperations()) {
    if (auto f = dyn_cast<LL::LLVMFuncOp>(op))
      share(f);
    else if (auto g = dyn_cast<LL::GlobalOp>(op))
      share(g);
  }
  AttrTypeReplacer r;
  r.addReplacement([&](FlatSymbolRefAttr a) -> std::optional<Attribute> {
    auto it = renamed.find(a.getAttr());
    if (it == renamed.end()) return std::nullopt;
    return FlatSymbolRefAttr::get(it->second);
  });
  r.recursivelyReplaceElementsIn(module, /*replaceAttrs=*/true, /*replaceLocs=*/false, /*replaceTypes=*/false);
}

bool ModuleGen::run(std::string &fragment, std::vector<std::string> &fallback,
                    std::vector<std::string> &reasons, Stats &stats, std::string &err) {
  for (Operation &op : module.getBody()->getOperations()) {
    if (auto f = dyn_cast<LL::LLVMFuncOp>(op)) {
      if (!f.isExternal()) {
        definedData[f.getSymNameAttr()] = true;
        definedNames.insert(f.getSymName());
      }
      bool twice = f.getSymName() == "rt_setjmp";
      if (auto pt = f.getPassthrough())
        for (Attribute a : *pt)
          if (auto sa = dyn_cast<StringAttr>(a); sa && sa.getValue() == "returns_twice") twice = true;
      if (twice) returnsTwice.insert(f.getSymNameAttr());
      funcs[f.getSymNameAttr()] = f;
    } else if (auto g = dyn_cast<LL::GlobalOp>(op)) {
      if (g.getValueOrNull() || !g.getInitializerRegion().empty()) {
        definedData[g.getSymNameAttr()] = true;
        definedNames.insert(g.getSymName());
      }
    }
  }
  for (Operation &op : module.getBody()->getOperations()) {
    if (auto g = dyn_cast<LL::GlobalOp>(op)) {
      if (!definedData.count(g.getSymNameAttr())) continue;  // a declaration
      if (isUsedList(g)) {
        // Not data. llvm.used: the symbols it lists survive dead stripping;
        // llvm.compiler.used only keeps them in this object (they are all
        // emitted anyway). The list stays in the module for a fallback object.
        if (g.getSymName() == "llvm.used")
          g.getInitializerRegion().walk([&](LL::AddressOfOp a) { w.retain(symName(a.getGlobalName())); });
        continue;
      }
      try {
        emitGlobal(g);
        stats.globals++;
      } catch (const Unsupported &u) {
        err = "global " + g.getSymName().str() + ": " + u.why;
        return false;
      }
    }
  }
  emitFunctions(fallback, reasons, stats);
  stats.codeBytes = w.size(Sect::Text);
  llvm::DenseMap<StringAttr, StringAttr> renamed;
  if (!fallback.empty()) {
    renamed = localNames(module, unit);
    for (auto &[from, to] : renamed) w.renameShared(machoName(from.getValue()), machoName(to.getValue()));
  }
  // The pc table fragment (Pcln.h): files, then each function and its rows.
  fragment.clear();
  llvm::raw_string_ostream os(fragment);
  for (const std::string &f : files) os << "P\t" << f << "\n";
  for (const Lines &l : lines) {
    // Mach-O names carry a leading "_"; fragments use the IR name.
    os << "F\t" << l.size << "\t" << StringRef(w.name(l.sym)).drop_front(x64 ? 0 : 1) << "\n";
    for (auto &[off, line, file] : l.rows) os << "R\t" << off << "\t" << line << "\t" << file << "\n";
    for (auto &[nwords, bits] : l.maps) {
      os << "M\t" << nwords << "\t";
      for (unsigned char c : bits) os << llvm::format("%02x", c);
      os << "\n";
    }
    for (auto &[off, id] : l.sites) os << "S\t" << off << "\t" << id << "\n";
  }
  os.flush();
  // What stays for LLVM: the fallback functions, everything else declared.
  llvm::DenseMap<StringAttr, bool> keep;
  for (auto &n : fallback) keep[StringAttr::get(ctx, n)] = true;
  for (Operation &op : module.getBody()->getOperations()) {
    if (auto f = dyn_cast<LL::LLVMFuncOp>(op)) {
      if (f.isExternal() || keep.count(f.getSymNameAttr())) continue;
      f.getBody().dropAllReferences();
      f.getBody().getBlocks().clear();
      if (isWeakLinkage(f.getLinkage()) || isLocalLinkage(f.getLinkage())) f.setLinkage(LL::Linkage::External);
    } else if (auto g = dyn_cast<LL::GlobalOp>(op)) {
      if (!definedData.count(g.getSymNameAttr()) || isUsedList(g)) continue;
      g.getInitializerRegion().dropAllReferences();
      g.getInitializerRegion().getBlocks().clear();
      g.removeValueAttr();
      g.setLinkage(LL::Linkage::External);
    }
  }
  shareLocals(module, renamed);
  return true;
}

bool emit(ModuleOp module, const std::string &unit, const std::string &outObject, std::string &fragment,
          std::vector<std::string> &fallback, std::vector<std::string> &reasons, Stats &stats,
          std::string &err, Target target, unsigned threads) {
  dropUnused(module);
  bool x64 = target == Target::X64Elf;
  elfNames = x64;
  ModuleGen mg(module, unit, x64);
  mg.threads = threads ? threads : 1;
  // MARMOT_TIER0_THREADS=N forces N threads (tests of the parallel path).
  if (const char *e = getenv("MARMOT_TIER0_THREADS"); e && atoi(e) > 0) mg.threads = unsigned(atoi(e));
  if (!mg.run(fragment, fallback, reasons, stats, err)) return false;
  return mg.w.write(outObject, err);
}

std::map<std::string, uint64_t> opBytes() {
  std::lock_guard<std::mutex> l(opBytesMu);
  return opBytesAll;
}

} // namespace tier0
} // namespace marmot
