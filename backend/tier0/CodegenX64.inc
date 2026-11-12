// CodegenX64.inc: the x86-64 function generator (SysV, as LLVM lowers it),
// included by Codegen.cpp after the arm64 one; same frame scheme: every
// value in a slot at rsp + offset, integers zero extended in 64 bits.

namespace X = marmot::tier0::x64;

// Where LLVM's x86-64 SysV lowering puts each piece of the arguments:
// integers and pointers in rdi, rsi, rdx, rcx, r8, r9, floats in xmm0-7,
// the rest in 8-byte stack slots in order.
std::vector<ArgLoc> assignArgsX64(TypeRange types, uint64_t &stackSize, bool sret) {
  static const unsigned gpr[6] = {X::RDI, X::RSI, X::RDX, X::RCX, X::R8, X::R9};
  std::vector<ArgLoc> out;
  unsigned ngp = sret ? 1 : 0, nfp = 0;
  uint64_t sp = 0;
  for (unsigned i = 0; i < types.size(); i++) {
    std::vector<std::pair<uint64_t, Type>> parts;
    flatten(types[i], 0, parts);
    for (auto [off, t] : parts) {
      ArgLoc l{i, off, t, isFloat(t), -1, 0};
      if (l.fp ? nfp < 8 : ngp < 6) {
        l.reg = l.fp ? int(nfp++) : int(gpr[ngp++]);
      } else {
        l.stack = sp;
        sp += 8;
      }
      out.push_back(l);
    }
  }
  stackSize = (sp + 15) & ~uint64_t(15);
  return out;
}

// Results: up to four integer pieces (rax, rdx, rcx, r8) and two float
// pieces (xmm0, xmm1); more and the result goes through a pointer passed in
// rdi, returned again in rax.
bool returnsInMemoryX64(Type t) {
  std::vector<std::pair<uint64_t, Type>> parts;
  flatten(t, 0, parts);
  unsigned ngp = 0, nfp = 0;
  for (auto &p : parts) (isFloat(p.second) ? nfp : ngp)++;
  return ngp > 4 || nfp > 2;
}
const unsigned retGpr[4] = {X::RAX, X::RDX, X::RCX, X::R8};

class FuncGenX64 {
public:
  FuncGenX64(ModuleGen &mg, LL::LLVMFuncOp f) : mg(mg), w(mg.w), fn(f), a(w.bytes(Sect::Text)) {}
  void run();

private:
  ModuleGen &mg;
  MachOWriter &w;
  LL::LLVMFuncOp fn;
  X::Asm a;
  // Scratch: rax, rcx, rdx and r11 inside an operation; r10 holds an
  // indirect callee; argument registers only around calls.
  static constexpr unsigned T1 = X::RAX, T2 = X::RCX, T3 = X::RDX, T4 = X::R11, T5 = X::R10;
  static constexpr unsigned XF1 = 14, XF2 = 15;  // xmm14, xmm15

  llvm::DenseMap<Value, uint64_t> slot;
  llvm::DenseMap<Operation *, uint64_t> allocaStorage;
  llvm::DenseMap<Block *, uint64_t> blockStart;
  struct Fixup {
    size_t field;  // rel32 field, relative to the end of the field
    Block *dest;
  };
  std::vector<Fixup> fixups;
  uint64_t frameSize = 0, tempArea = 0, tempSize = 0, sretSlot = 0;
  bool sret = false;

  uint64_t alloc(uint64_t size, uint64_t align) {
    frameSize = (frameSize + align - 1) & ~(align - 1);
    uint64_t at = frameSize;
    frameSize += size;
    return at;
  }
  int32_t d32(uint64_t off) {
    if (off >= (uint64_t(1) << 31)) unsupported("frame larger than 2 GiB");
    return int32_t(off);
  }
  uint64_t of(Value v) {
    auto it = slot.find(v);
    if (it == slot.end()) unsupported("value without a slot");
    return it->second;
  }
  void ld(unsigned r, uint64_t off) { a.load(r, X::RSP, d32(off), 8); }
  void st(unsigned r, uint64_t off) { a.store(r, X::RSP, d32(off), 8); }
  void get(unsigned r, Value v) {
    if (sizeOf(v.getType()) > 8) unsupported("i128 operation");
    ld(r, of(v));
  }
  void getSigned(unsigned r, Value v) {
    get(r, v);
    signExtend(r, intWidth(v.getType()));
  }
  void signExtend(unsigned r, unsigned wdt) {
    switch (wdt) {
    case 64: break;
    case 32: a.movsx32(r, r); break;
    case 16: a.movsx16(r, r); break;
    case 8: a.movsx8(r, r); break;
    default:
      a.shiftImm(4, r, uint8_t(64 - wdt));
      a.shiftImm(7, r, uint8_t(64 - wdt));
    }
  }
  void normalize(unsigned r, Type t) {
    unsigned wdt = intWidth(t);
    switch (wdt) {
    case 64: break;
    case 32: a.mov32(r, r); break;
    case 16: a.movzx16(r, r); break;
    case 8: a.movzx8(r, r); break;
    default:
      a.shiftImm(4, r, uint8_t(64 - wdt));
      a.shiftImm(5, r, uint8_t(64 - wdt));
    }
  }
  void put(unsigned r, Value v) {
    if (sizeOf(v.getType()) > 8) unsupported("i128 operation");
    if (!isFloat(v.getType())) normalize(r, v.getType());
    st(r, of(v));
  }
  void getF(unsigned x, Value v) {
    get(T4, v);
    a.movqToX(x, T4);
  }
  void putF(unsigned x, Value v) {
    a.movqFromX(T4, x);
    if (v.getType().isF32()) a.mov32(T4, T4);
    st(T4, of(v));
  }
  // Copies n bytes from [sb + so] to [db + do_] through r11.
  void copyBytes(unsigned db, uint64_t do_, unsigned sb, uint64_t so, uint64_t n) {
    uint64_t i = 0;
    for (; i + 8 <= n; i += 8) {
      a.load(T4, sb, d32(so + i), 8);
      a.store(T4, db, d32(do_ + i), 8);
    }
    for (unsigned sz = 4; sz >= 1; sz /= 2)
      for (; i + sz <= n; i += sz) {
        a.load(T4, sb, d32(so + i), sz);
        a.store(T4, db, d32(do_ + i), sz);
      }
  }
  void callSym(const std::string &name) {
    size_t at = a.call();
    w.reloc(Sect::Text, at, Reloc::Call32, w.symbol(name));
  }
  void patch32(size_t field, int64_t target) {
    int32_t rel = int32_t(target - int64_t(field + 4));
    std::memcpy(&w.bytes(Sect::Text)[field], &rel, 4);
  }

  void layout();
  void prologue();
  void epilogue();
  void emitOp(Operation &op);
  bool emitWide(Operation &op);
  void emitCall(LL::CallOp call);
  void emitReturn(LL::ReturnOp ret);
  void emitIntrinsic(LL::CallIntrinsicOp c);
  void fminmax(Operation &op, bool max, bool num);
  void edgeMoves(Block *dest, OperandRange args);
  void branchTo(Block *dest) {
    size_t f = a.jmp();
    fixups.push_back({f, dest});
  }
};

void FuncGenX64::layout() {
  uint64_t out = 0;
  fn.walk([&](LL::CallOp c) {
    uint64_t n = 0;
    bool mem = c.getNumResults() == 1 && returnsInMemoryX64(c.getResult().getType());
    assignArgsX64(c.getArgOperands().getTypes(), n, mem);
    out = std::max(out, n);
  });
  alloc(out, 16);
  auto rt = fn.getFunctionType().getReturnType();
  if (!isa<LL::LLVMVoidType>(rt) && returnsInMemoryX64(rt)) {
    sret = true;
    sretSlot = alloc(8, 8);
  }
  for (Block &b : fn.getBody()) {
    for (BlockArgument arg : b.getArguments())
      slot[arg] = alloc(std::max<uint64_t>(sizeOf(arg.getType()), 8), std::max<uint64_t>(8, alignOf(arg.getType())));
    for (Operation &op : b) {
      for (Value r : op.getResults())
        slot[r] = alloc(std::max<uint64_t>(sizeOf(r.getType()), 8), std::max<uint64_t>(8, alignOf(r.getType())));
      if (auto al = dyn_cast<LL::AllocaOp>(op)) {
        if (&b != &fn.getBody().front()) unsupported("alloca outside the entry block");
        auto c = al.getArraySize().getDefiningOp<LL::ConstantOp>();
        if (!c) unsupported("dynamic alloca");
        uint64_t n = cast<IntegerAttr>(c.getValue()).getValue().getZExtValue();
        uint64_t algn = std::max<uint64_t>(al.getAlignment().value_or(0), alignOf(al.getElemType()));
        allocaStorage[&op] = alloc(std::max<uint64_t>(n * sizeOf(al.getElemType()), 1), std::max<uint64_t>(algn, 8));
      }
    }
  }
  for (Block &b : fn.getBody()) {
    uint64_t n = 0;
    for (BlockArgument arg : b.getArguments()) n += std::max<uint64_t>((sizeOf(arg.getType()) + 7) & ~uint64_t(7), 8);
    tempSize = std::max(tempSize, n);
  }
  tempArea = alloc(tempSize, 8);
  frameSize = (frameSize + 15) & ~uint64_t(15);
}

void FuncGenX64::prologue() {
  a.push(X::RBP);
  a.mov(X::RBP, X::RSP);
  if (frameSize) a.subImm(X::RSP, d32(frameSize));
  if (sret) st(X::RDI, sretSlot);
  auto params = fn.getBody().front().getArguments();
  uint64_t stackSize = 0;
  for (const ArgLoc &l : assignArgsX64(TypeRange(fn.getFunctionType().getParams()), stackSize, sret)) {
    BlockArgument arg = params[l.arg];
    unsigned r;
    if (l.reg < 0) {
      a.load(T4, X::RBP, d32(16 + l.stack), 8);
      r = T4;
    } else if (l.fp) {
      a.movqFromX(T4, unsigned(l.reg));
      if (l.t.isF32()) a.mov32(T4, T4);
      r = T4;
    } else {
      r = unsigned(l.reg);
    }
    if (isAggregate(arg.getType())) {
      a.store(r, X::RSP, d32(of(arg) + l.off), unsigned(sizeOf(l.t)));
    } else {
      if (r != T4) {
        a.mov(T4, r);
        r = T4;
      }
      if (!l.fp) normalize(r, l.t);
      st(r, of(arg));
    }
  }
}

void FuncGenX64::epilogue() {
  a.mov(X::RSP, X::RBP);
  a.pop(X::RBP);
  a.ret();
}

void FuncGenX64::edgeMoves(Block *dest, OperandRange args) {
  if (args.empty()) return;
  uint64_t t = tempArea;
  std::vector<uint64_t> tmp;
  for (Value v : args) {
    uint64_t sz = std::max<uint64_t>((sizeOf(v.getType()) + 7) & ~uint64_t(7), 8);
    copyBytes(X::RSP, t, X::RSP, of(v), sz);
    tmp.push_back(t);
    t += sz;
  }
  for (unsigned i = 0; i < args.size(); i++) {
    BlockArgument arg = dest->getArgument(i);
    uint64_t sz = std::max<uint64_t>((sizeOf(arg.getType()) + 7) & ~uint64_t(7), 8);
    copyBytes(X::RSP, of(arg), X::RSP, tmp[i], sz);
  }
}

void FuncGenX64::emitCall(LL::CallOp call) {
  if (call.getVarCalleeType()) unsupported("variadic call");
  auto args = call.getArgOperands();
  bool memResult = call.getNumResults() == 1 && returnsInMemoryX64(call.getResult().getType());
  uint64_t stackSize = 0;
  std::vector<ArgLoc> locs = assignArgsX64(args.getTypes(), stackSize, memResult);
  auto load = [&](unsigned r, const ArgLoc &l) {
    Value v = args[l.arg];
    if (isAggregate(v.getType()))
      a.load(r, X::RSP, d32(of(v) + l.off), unsigned(sizeOf(l.t)));
    else
      get(r, v);
  };
  // Stack and float pieces through r11, then the indirect callee into r10,
  // then the integer registers (rcx and rdx are arguments too).
  for (const ArgLoc &l : locs) {
    if (l.reg >= 0 && !l.fp) continue;
    load(T4, l);
    if (l.reg >= 0)
      a.movqToX(unsigned(l.reg), T4);
    else
      a.store(T4, X::RSP, d32(l.stack), 8);
  }
  bool indirect = !call.getCallee();
  if (indirect) get(T5, call.getCalleeOperands().front());
  for (const ArgLoc &l : locs)
    if (l.reg >= 0 && !l.fp) load(unsigned(l.reg), l);
  if (memResult) a.lea(X::RDI, X::RSP, d32(of(call.getResult())));
  if (indirect) {
    a.callReg(T5);
  } else {
    StringRef callee = *call.getCallee();
    if (callee == "marmot_gpu_for")
      a.movImm(X::RAX, 0);
    else
      callSym(symName(callee));
  }
  if (call.getNumResults() == 0 || memResult) return;
  Value r = call.getResult();
  std::vector<std::pair<uint64_t, Type>> parts;
  flatten(r.getType(), 0, parts);
  // Into r11 one at a time: the result registers overlap the scratch ones.
  std::vector<std::pair<uint64_t, Type>> gp, fp;
  for (auto &p : parts) (isFloat(p.second) ? fp : gp).push_back(p);
  auto store = [&](unsigned reg, uint64_t off, Type t) {
    if (isAggregate(r.getType())) {
      a.store(reg, X::RSP, d32(of(r) + off), unsigned(sizeOf(t)));
    } else {
      a.mov(T4, reg);
      if (!isFloat(t)) normalize(T4, t);
      st(T4, of(r));
    }
  };
  for (unsigned i = 0; i < gp.size(); i++) store(retGpr[i], gp[i].first, gp[i].second);
  for (unsigned i = 0; i < fp.size(); i++) {
    a.movqFromX(X::RAX, i);
    if (fp[i].second.isF32()) a.mov32(X::RAX, X::RAX);
    store(X::RAX, fp[i].first, fp[i].second);
  }
}

void FuncGenX64::emitReturn(LL::ReturnOp ret) {
  if (sret) {
    Value v = ret.getOperand(0);
    ld(T2, sretSlot);
    copyBytes(T2, 0, X::RSP, of(v), sizeOf(v.getType()));
    a.mov(X::RAX, T2);
  } else if (ret.getNumOperands() == 1) {
    Value v = ret.getOperand(0);
    std::vector<std::pair<uint64_t, Type>> parts;
    flatten(v.getType(), 0, parts);
    unsigned ngp = 0, nfp = 0;
    // Floats first (through r11), then the integer registers.
    for (auto [off, t] : parts) {
      if (!isFloat(t)) continue;
      if (isAggregate(v.getType()))
        a.load(T4, X::RSP, d32(of(v) + off), unsigned(sizeOf(t)));
      else
        get(T4, v);
      a.movqToX(nfp++, T4);
    }
    for (auto [off, t] : parts) {
      if (isFloat(t)) continue;
      if (isAggregate(v.getType()))
        a.load(retGpr[ngp++], X::RSP, d32(of(v) + off), unsigned(sizeOf(t)));
      else
        get(retGpr[ngp++], v);
    }
  }
  epilogue();
}

X::CC icmpCC(LL::ICmpPredicate p) {
  using P = LL::ICmpPredicate;
  switch (p) {
  case P::eq: return X::E;
  case P::ne: return X::NE;
  case P::slt: return X::L;
  case P::sle: return X::LE;
  case P::sgt: return X::G;
  case P::sge: return X::GE;
  case P::ult: return X::B;
  case P::ule: return X::BE;
  case P::ugt: return X::A;
  case P::uge: return X::AE;
  }
  return X::E;
}

// llvm.maximum / minimum (NaN propagates, -0 < +0) and maxnum / minnum
// (a NaN operand gives the other one).
void FuncGenX64::fminmax(Operation &op, bool max, bool num) {
  bool dbl = op.getResult(0).getType().isF64();
  getF(XF1, op.getOperand(0));
  getF(XF2, op.getOperand(1));
  std::vector<size_t> toEnd;
  if (num) {
    a.ucomi(XF1, XF1, dbl);  // a NaN: result b
    size_t j = a.jcc(X::NP);
    a.movqFromX(T1, XF2);
    toEnd.push_back(a.jmp());
    patch32(j, int64_t(a.here()));
    a.ucomi(XF2, XF2, dbl);  // b NaN: result a
    j = a.jcc(X::NP);
    a.movqFromX(T1, XF1);
    toEnd.push_back(a.jmp());
    patch32(j, int64_t(a.here()));
  }
  a.ucomi(XF1, XF2, dbl);
  size_t unord = a.jcc(X::P);
  size_t ne = a.jcc(X::NE);
  // Equal (maybe +0 and -0): and of the bits for max, or for min.
  a.movqFromX(T1, XF1);
  a.movqFromX(T2, XF2);
  if (max)
    a.and_(T1, T2);
  else
    a.or_(T1, T2);
  toEnd.push_back(a.jmp());
  patch32(ne, int64_t(a.here()));
  if (max)
    a.maxsd(XF1, XF2, dbl);
  else
    a.minsd(XF1, XF2, dbl);
  a.movqFromX(T1, XF1);
  toEnd.push_back(a.jmp());
  patch32(unord, int64_t(a.here()));
  a.addsd(XF1, XF2, dbl);  // a NaN in, a NaN out
  a.movqFromX(T1, XF1);
  for (size_t j : toEnd) patch32(j, int64_t(a.here()));
  if (!dbl) a.mov32(T1, T1);
  st(T1, of(op.getResult(0)));
}

void FuncGenX64::emitOp(Operation &op) {
  if (emitWide(op)) return;
  auto bin = [&](auto f) {
    get(T1, op.getOperand(0));
    get(T2, op.getOperand(1));
    f(T1, T2);
    put(T1, op.getResult(0));
  };
  auto fbin = [&](auto f) {
    bool dbl = op.getResult(0).getType().isF64();
    getF(XF1, op.getOperand(0));
    getF(XF2, op.getOperand(1));
    f(XF1, XF2, dbl);
    putF(XF1, op.getResult(0));
  };
  auto shift = [&](unsigned kind, bool sgn) {
    if (sgn)
      getSigned(T1, op.getOperand(0));
    else
      get(T1, op.getOperand(0));
    get(T2, op.getOperand(1));  // rcx
    a.shiftCl(kind, T1);
    put(T1, op.getResult(0));
  };
  auto divide = [&](bool sgn, bool rem) {
    if (sgn) {
      getSigned(T1, op.getOperand(0));
      getSigned(T2, op.getOperand(1));
      a.cqo();
      a.idiv(T2);
    } else {
      get(T1, op.getOperand(0));
      get(T2, op.getOperand(1));
      a.xor_(T3, T3);
      a.div(T2);
    }
    put(rem ? T3 : T1, op.getResult(0));
  };
  llvm::TypeSwitch<Operation *>(&op)
      .Case<LL::ConstantOp>([&](auto c) {
        uint64_t bits = 0;
        if (auto i = dyn_cast<IntegerAttr>(c.getValue()))
          bits = i.getValue().getZExtValue();
        else if (auto f = dyn_cast<FloatAttr>(c.getValue()))
          bits = f.getValue().bitcastToAPInt().getZExtValue();
        else
          unsupported("constant kind");
        if (isAggregate(c.getType())) unsupported("aggregate constant");
        a.movImm(T1, bits);
        put(T1, c.getResult());
      })
      .Case<LL::ZeroOp, LL::UndefOp, LL::PoisonOp>([&](auto z) {
        uint64_t n = std::max<uint64_t>((sizeOf(z.getType()) + 7) & ~uint64_t(7), 8);
        a.xor_(T1, T1);
        for (uint64_t i = 0; i < n; i += 8) st(T1, of(z.getResult()) + i);
      })
      .Case<LL::AddressOfOp>([&](auto ad) {
        unsigned sym = w.symbol(symName(ad.getGlobalName()));
        if (mg.isDefinedData(ad.getGlobalName()))
          w.reloc(Sect::Text, a.leaRip(T1), Reloc::Pc32, sym);
        else
          w.reloc(Sect::Text, a.loadRip(T1), Reloc::GotPc32, sym);
        put(T1, ad.getResult());
      })
      .Case<LL::AddOp>([&](auto) { bin([&](unsigned d, unsigned s) { a.add(d, s); }); })
      .Case<LL::SubOp>([&](auto) { bin([&](unsigned d, unsigned s) { a.sub(d, s); }); })
      .Case<LL::MulOp>([&](auto) { bin([&](unsigned d, unsigned s) { a.imul(d, s); }); })
      .Case<LL::AndOp>([&](auto) { bin([&](unsigned d, unsigned s) { a.and_(d, s); }); })
      .Case<LL::OrOp>([&](auto) { bin([&](unsigned d, unsigned s) { a.or_(d, s); }); })
      .Case<LL::XOrOp>([&](auto) { bin([&](unsigned d, unsigned s) { a.xor_(d, s); }); })
      .Case<LL::ShlOp>([&](auto) { shift(4, false); })
      .Case<LL::LShrOp>([&](auto) { shift(5, false); })
      .Case<LL::AShrOp>([&](auto) { shift(7, true); })
      .Case<LL::UDivOp>([&](auto) { divide(false, false); })
      .Case<LL::SDivOp>([&](auto) { divide(true, false); })
      .Case<LL::URemOp>([&](auto) { divide(false, true); })
      .Case<LL::SRemOp>([&](auto) { divide(true, true); })
      .Case<LL::ICmpOp>([&](auto c) {
        bool sgn = isSignedPred(c.getPredicate());
        if (sgn) {
          getSigned(T1, c.getLhs());
          getSigned(T2, c.getRhs());
        } else {
          get(T1, c.getLhs());
          get(T2, c.getRhs());
        }
        a.cmp(T1, T2);
        a.setcc(icmpCC(c.getPredicate()), T1);
        a.movzx8(T1, T1);
        put(T1, c.getResult());
      })
      .Case<LL::SelectOp>([&](auto s) {
        Type t = s.getType();
        get(T3, s.getCondition());
        if (isAggregate(t)) {
          uint64_t sz = std::max<uint64_t>((sizeOf(t) + 7) & ~uint64_t(7), 8);
          a.test(T3, T3);
          size_t jz = a.jcc(X::E);
          copyBytes(X::RSP, of(s.getResult()), X::RSP, of(s.getTrueValue()), sz);
          size_t j = a.jmp();
          patch32(jz, int64_t(a.here()));
          copyBytes(X::RSP, of(s.getResult()), X::RSP, of(s.getFalseValue()), sz);
          patch32(j, int64_t(a.here()));
          return;
        }
        get(T1, s.getFalseValue());
        get(T2, s.getTrueValue());
        a.test(T3, T3);
        a.cmov(X::NE, T1, T2);
        st(T1, of(s.getResult()));
      })
      .Case<LL::ZExtOp, LL::TruncOp, LL::PtrToIntOp, LL::IntToPtrOp>([&](auto c) {
        get(T1, c.getArg());
        put(T1, c.getResult());
      })
      .Case<LL::SExtOp>([&](auto c) {
        getSigned(T1, c.getArg());
        put(T1, c.getResult());
      })
      .Case<LL::BitcastOp>([&](auto c) {
        if (isAggregate(c.getType())) unsupported("aggregate bitcast");
        get(T1, c.getArg());
        st(T1, of(c.getResult()));
      })
      .Case<LL::FAddOp>([&](auto) { fbin([&](unsigned d, unsigned s, bool x) { a.addsd(d, s, x); }); })
      .Case<LL::FSubOp>([&](auto) { fbin([&](unsigned d, unsigned s, bool x) { a.subsd(d, s, x); }); })
      .Case<LL::FMulOp>([&](auto) { fbin([&](unsigned d, unsigned s, bool x) { a.mulsd(d, s, x); }); })
      .Case<LL::FDivOp>([&](auto) { fbin([&](unsigned d, unsigned s, bool x) { a.divsd(d, s, x); }); })
      .Case<LL::MaximumOp>([&](auto) { fminmax(op, true, false); })
      .Case<LL::MinimumOp>([&](auto) { fminmax(op, false, false); })
      .Case<LL::MaxNumOp>([&](auto) { fminmax(op, true, true); })
      .Case<LL::MinNumOp>([&](auto) { fminmax(op, false, true); })
      .Case<LL::FNegOp>([&](auto o) {
        get(T1, o.getOperand());
        a.movImm(T2, o.getType().isF64() ? uint64_t(1) << 63 : uint64_t(1) << 31);
        a.xor_(T1, T2);
        st(T1, of(o.getResult()));
      })
      .Case<LL::FCmpOp>([&](auto c) {
        bool dbl = c.getLhs().getType().isF64();
        getF(XF1, c.getLhs());
        getF(XF2, c.getRhs());
        using P = LL::FCmpPredicate;
        auto one = [&](bool swap, X::CC k) {
          swap ? a.ucomi(XF2, XF1, dbl) : a.ucomi(XF1, XF2, dbl);
          a.setcc(k, T1);
          a.movzx8(T1, T1);
        };
        auto two = [&](X::CC k1, X::CC k2, bool both) {
          a.ucomi(XF1, XF2, dbl);
          a.setcc(k1, T1);
          a.setcc(k2, T2);
          a.movzx8(T1, T1);
          a.movzx8(T2, T2);
          both ? a.and_(T1, T2) : a.or_(T1, T2);
        };
        switch (c.getPredicate()) {
        case P::_false: a.movImm(T1, 0); break;
        case P::_true: a.movImm(T1, 1); break;
        case P::oeq: two(X::E, X::NP, true); break;
        case P::ogt: one(false, X::A); break;
        case P::oge: one(false, X::AE); break;
        case P::olt: one(true, X::A); break;
        case P::ole: one(true, X::AE); break;
        case P::one: one(false, X::NE); break;
        case P::ord: one(false, X::NP); break;
        case P::ueq: one(false, X::E); break;
        case P::ugt: one(true, X::B); break;
        case P::uge: one(true, X::BE); break;
        case P::ult: one(false, X::B); break;
        case P::ule: one(false, X::BE); break;
        case P::une: two(X::NE, X::P, false); break;
        case P::uno: one(false, X::P); break;
        }
        put(T1, c.getResult());
      })
      .Case<LL::SIToFPOp>([&](auto c) {
        getSigned(T1, c.getArg());
        a.cvtsi2f(XF1, T1, c.getType().isF64());
        putF(XF1, c.getResult());
      })
      .Case<LL::UIToFPOp>([&](auto c) {
        bool dbl = c.getType().isF64();
        get(T1, c.getArg());
        // Below 2^63 a signed conversion; above, halve (keeping the low bit
        // for rounding), convert and double.
        a.test(T1, T1);
        size_t neg = a.jcc(X::S);
        a.cvtsi2f(XF1, T1, dbl);
        size_t j = a.jmp();
        patch32(neg, int64_t(a.here()));
        a.mov(T2, T1);
        a.shiftImm(5, T2, 1);
        a.movImm(T3, 1);
        a.and_(T1, T3);
        a.or_(T2, T1);
        a.cvtsi2f(XF1, T2, dbl);
        a.addsd(XF1, XF1, dbl);
        patch32(j, int64_t(a.here()));
        putF(XF1, c.getResult());
      })
      .Case<LL::FPToSIOp>([&](auto c) {
        getF(XF1, c.getArg());
        a.cvttf2si(T1, XF1, c.getArg().getType().isF64());
        put(T1, c.getResult());
      })
      .Case<LL::FPToUIOp>([&](auto c) {
        bool dbl = c.getArg().getType().isF64();
        getF(XF1, c.getArg());
        // At or above 2^63: subtract 2^63 first and set the top bit after.
        a.movImm(T2, dbl ? 0x43E0000000000000ull : 0x5F000000ull);
        a.movqToX(XF2, T2);
        a.ucomi(XF1, XF2, dbl);
        size_t big = a.jcc(X::AE);
        a.cvttf2si(T1, XF1, dbl);
        size_t j = a.jmp();
        patch32(big, int64_t(a.here()));
        a.subsd(XF1, XF2, dbl);
        a.cvttf2si(T1, XF1, dbl);
        a.movImm(T2, uint64_t(1) << 63);
        a.xor_(T1, T2);
        patch32(j, int64_t(a.here()));
        put(T1, c.getResult());
      })
      .Case<LL::FPExtOp>([&](auto c) {
        getF(XF1, c.getArg());
        a.cvtss2sd(XF1, XF1);
        putF(XF1, c.getResult());
      })
      .Case<LL::FPTruncOp>([&](auto c) {
        getF(XF1, c.getArg());
        a.cvtsd2ss(XF1, XF1);
        putF(XF1, c.getResult());
      })
      .Case<LL::LoadOp>([&](auto l) {
        Type t = l.getType();
        get(T3, l.getAddr());
        if (isAggregate(t)) {
          copyBytes(X::RSP, of(l.getResult()), T3, 0, sizeOf(t));
          return;
        }
        a.load(T1, T3, 0, unsigned(sizeOf(t)));  // atomic loads are plain loads on x86
        if (intWidth(t) == 1) {
          a.movImm(T2, 1);
          a.and_(T1, T2);
        }
        st(T1, of(l.getResult()));
      })
      .Case<LL::StoreOp>([&](auto s) {
        Type t = s.getValue().getType();
        get(T3, s.getAddr());
        if (isAggregate(t)) {
          copyBytes(T3, 0, X::RSP, of(s.getValue()), sizeOf(t));
          return;
        }
        get(T1, s.getValue());
        unsigned sz = unsigned(sizeOf(t));
        if (s.getOrdering() == LL::AtomicOrdering::seq_cst)
          a.xchg(T1, T3, sz);
        else
          a.store(T1, T3, 0, sz);
      })
      .Case<LL::AllocaOp>([&](auto al) {
        a.lea(T1, X::RSP, d32(allocaStorage[al.getOperation()]));
        st(T1, of(al.getResult()));
      })
      .Case<LL::GEPOp>([&](auto g) {
        get(T1, g.getBase());
        Type et = g.getElemType();
        bool first = true;
        int64_t constOff = 0;
        for (auto idx : g.getIndices()) {
          uint64_t scale;
          if (first) {
            scale = sizeOf(et);
          } else if (auto s = dyn_cast<LL::LLVMStructType>(et)) {
            auto ia = llvm::dyn_cast_if_present<IntegerAttr>(idx);
            if (!ia) unsupported("dynamic struct index");
            constOff += int64_t(fieldOffset(s, unsigned(ia.getInt())));
            et = s.getBody()[ia.getInt()];
            continue;
          } else if (auto at = dyn_cast<LL::LLVMArrayType>(et)) {
            scale = sizeOf(at.getElementType());
            et = at.getElementType();
          } else {
            unsupported("gep into a scalar");
          }
          first = false;
          if (auto ia = llvm::dyn_cast_if_present<IntegerAttr>(idx)) {
            constOff += ia.getInt() * int64_t(scale);
          } else {
            getSigned(T2, llvm::cast<Value>(idx));
            if (scale != 1) {
              a.movImm(T3, scale);
              a.imul(T2, T3);
            }
            a.add(T1, T2);
          }
        }
        if (constOff) {
          a.movImm(T2, uint64_t(constOff));
          a.add(T1, T2);
        }
        st(T1, of(g.getResult()));
      })
      .Case<LL::ExtractValueOp>([&](auto x) {
        auto [off, t] = elementAt(x.getContainer().getType(), x.getPosition());
        uint64_t src = of(x.getContainer()) + off;
        if (isAggregate(t)) {
          copyBytes(X::RSP, of(x.getResult()), X::RSP, src, std::max<uint64_t>((sizeOf(t) + 7) & ~uint64_t(7), 8));
          return;
        }
        a.load(T1, X::RSP, d32(src), unsigned(sizeOf(t)));
        st(T1, of(x.getResult()));
      })
      .Case<LL::InsertValueOp>([&](auto ins) {
        Type at = ins.getType();
        uint64_t sz = std::max<uint64_t>((sizeOf(at) + 7) & ~uint64_t(7), 8);
        copyBytes(X::RSP, of(ins.getResult()), X::RSP, of(ins.getContainer()), sz);
        auto [off, t] = elementAt(at, ins.getPosition());
        uint64_t dst = of(ins.getResult()) + off;
        if (isAggregate(t)) {
          copyBytes(X::RSP, dst, X::RSP, of(ins.getValue()), sizeOf(t));
          return;
        }
        get(T1, ins.getValue());
        a.store(T1, X::RSP, d32(dst), unsigned(sizeOf(t)));
      })
      .Case<LL::CallOp>([&](auto c) { emitCall(c); })
      .Case<LL::MemcpyOp, LL::MemmoveOp>([&](auto m) {
        get(X::RDI, m.getDst());
        get(X::RSI, m.getSrc());
        get(X::RDX, m.getLen());
        callSym(symName("memmove"));
      })
      .Case<LL::MemsetOp>([&](auto m) {
        get(X::RDI, m.getDst());
        get(X::RSI, m.getVal());
        get(X::RDX, m.getLen());
        callSym(symName("memset"));
      })
      .Case<LL::FMAOp>([&](auto f) {
        // No FMA instruction at x86-64-v2: the C library's fma.
        bool dbl = f.getType().isF64();
        getF(0, f.getA());
        getF(1, f.getB());
        getF(2, f.getC());
        callSym(symName(dbl ? "fma" : "fmaf"));
        putF(0, f.getResult());
      })
      .Case<LL::FMulAddOp>([&](auto f) {
        bool dbl = f.getType().isF64();
        getF(XF1, f.getA());
        getF(XF2, f.getB());
        a.mulsd(XF1, XF2, dbl);
        getF(XF2, f.getC());
        a.addsd(XF1, XF2, dbl);
        putF(XF1, f.getResult());
      })
      .Case<LL::AtomicRMWOp>([&](auto r) {
        Type t = r.getVal().getType();
        unsigned sz = unsigned(sizeOf(t));
        get(T3, r.getPtr());
        get(T1, r.getVal());
        using B = LL::AtomicBinOp;
        switch (r.getBinOp()) {
        case B::sub: a.neg(T1); [[fallthrough]];
        case B::add: a.lockXadd(T1, T3, sz); break;
        case B::xchg: a.xchg(T1, T3, sz); break;
        case B::_and:
        case B::_or:
        case B::_xor: {
          // Compare-and-swap loop: rax old, r11 new, rcx the operand.
          a.mov(T2, T1);
          a.load(T1, T3, 0, sz);
          size_t loop = a.here();
          a.mov(T4, T1);
          if (r.getBinOp() == B::_and) a.and_(T4, T2);
          if (r.getBinOp() == B::_or) a.or_(T4, T2);
          if (r.getBinOp() == B::_xor) a.xor_(T4, T2);
          a.lockCmpxchg(T4, T3, sz);
          patch32(a.jcc(X::NE), int64_t(loop));
          break;
        }
        default: unsupported("atomicrmw operation");
        }
        put(T1, r.getResult());
      })
      .Case<LL::AtomicCmpXchgOp>([&](auto x) {
        Type t = x.getVal().getType();
        unsigned sz = unsigned(sizeOf(t));
        get(T3, x.getPtr());
        get(T1, x.getCmp());
        get(T4, x.getVal());
        a.lockCmpxchg(T4, T3, sz);  // rax <- old
        a.setcc(X::E, T2);
        a.movzx8(T2, T2);
        uint64_t res = of(x.getResult());
        auto stt = cast<LL::LLVMStructType>(x.getType());
        a.store(T1, X::RSP, d32(res + fieldOffset(stt, 0)), sz);
        a.store(T2, X::RSP, d32(res + fieldOffset(stt, 1)), 1);
      })
      .Case<LL::FenceOp>([&](auto) { a.mfence(); })
      .Case<LL::UnreachableOp>([&](auto) { a.ud2(); })
      .Case<LL::ReturnOp>([&](auto r) { emitReturn(r); })
      .Case<LL::BrOp>([&](auto b) {
        edgeMoves(b.getDest(), b.getDestOperands());
        branchTo(b.getDest());
      })
      .Case<LL::CondBrOp>([&](auto c) {
        get(T3, c.getCondition());
        a.test(T3, T3);
        size_t jt = a.jcc(X::NE);
        edgeMoves(c.getFalseDest(), c.getFalseDestOperands());
        branchTo(c.getFalseDest());
        patch32(jt, int64_t(a.here()));
        edgeMoves(c.getTrueDest(), c.getTrueDestOperands());
        branchTo(c.getTrueDest());
      })
      .Case<LL::SwitchOp>([&](auto s) {
        get(T3, s.getValue());
        auto cases = s.getCaseValues();
        if (cases) {
          unsigned wdt = intWidth(s.getValue().getType());
          unsigned i = 0;
          for (const APInt &v : cases->template getValues<APInt>()) {
            a.movImm(T4, v.getZExtValue() & (wdt == 64 ? ~uint64_t(0) : ((uint64_t(1) << wdt) - 1)));
            a.cmp(T3, T4);
            size_t jn = a.jcc(X::NE);
            edgeMoves(s.getCaseDestinations()[i], s.getCaseOperands(i));
            branchTo(s.getCaseDestinations()[i]);
            patch32(jn, int64_t(a.here()));
            i++;
          }
        }
        edgeMoves(s.getDefaultDestination(), s.getDefaultOperands());
        branchTo(s.getDefaultDestination());
      })
      .Case<LL::CallIntrinsicOp>([&](auto c) { emitIntrinsic(c); })
      .Default([&](Operation *o) { unsupported("op " + o->getName().getStringRef().str()); });
}

// i128 as two words in a 16-byte slot, as on arm64.
bool FuncGenX64::emitWide(Operation &op) {
  bool wide = false;
  for (Type t : op.getOperandTypes()) wide |= isWide(t);
  for (Type t : op.getResultTypes()) wide |= isWide(t);
  if (!wide) return false;
  auto lo = [&](unsigned r, Value v) { ld(r, of(v)); };
  auto hi = [&](unsigned r, Value v) { ld(r, of(v) + 8); };
  auto store = [&](unsigned l, unsigned h, Value v) {
    st(l, of(v));
    st(h, of(v) + 8);
  };
  auto pair = [&](auto fl, auto fh) {
    lo(T1, op.getOperand(0));
    hi(T3, op.getOperand(0));
    lo(T2, op.getOperand(1));
    hi(T4, op.getOperand(1));
    fl(T1, T2);
    fh(T3, T4);
    store(T1, T3, op.getResult(0));
  };
  // a:b (rax low, rdx high) shifted by cl in [0, 128).
  auto shift = [&](unsigned kind) {
    lo(T1, op.getOperand(0));
    hi(T3, op.getOperand(0));
    lo(T2, op.getOperand(1));
    if (kind == 4) {  // shl
      a.shldCl(T3, T1);
      a.shiftCl(4, T1);
      a.xor_(T4, T4);
      a.cmpImm(T2, 64);  // by 64 or more: high = low, low = 0
      a.cmov(X::AE, T3, T1);
      a.cmov(X::AE, T1, T4);
    } else {
      a.shrdCl(T1, T3);
      a.mov(T4, T3);
      a.shiftCl(kind, T3);
      if (kind == 7)
        a.shiftImm(7, T4, 63);
      else
        a.xor_(T4, T4);
      a.cmpImm(T2, 64);  // low = high, high = fill
      a.cmov(X::AE, T1, T3);
      a.cmov(X::AE, T3, T4);
    }
    store(T1, T3, op.getResult(0));
  };
  llvm::TypeSwitch<Operation *>(&op)
      .Case<LL::ConstantOp>([&](auto c) {
        auto ia = dyn_cast<IntegerAttr>(c.getValue());
        if (!ia) unsupported("i128 constant");
        APInt v = ia.getValue().zextOrTrunc(128);
        a.movImm(T1, v.extractBitsAsZExtValue(64, 0));
        a.movImm(T2, v.extractBitsAsZExtValue(64, 64));
        store(T1, T2, c.getResult());
      })
      .Case<LL::ZExtOp>([&](auto c) {
        get(T1, c.getArg());
        a.xor_(T2, T2);
        store(T1, T2, c.getResult());
      })
      .Case<LL::SExtOp>([&](auto c) {
        getSigned(T1, c.getArg());
        a.mov(T2, T1);
        a.shiftImm(7, T2, 63);
        store(T1, T2, c.getResult());
      })
      .Case<LL::TruncOp>([&](auto c) {
        if (isWide(c.getResult().getType())) unsupported("i128 operation");
        lo(T1, c.getArg());
        put(T1, c.getResult());
      })
      .Case<LL::AddOp>([&](auto) {
        pair([&](unsigned d, unsigned s) { a.add(d, s); }, [&](unsigned d, unsigned s) { a.adc(d, s); });
      })
      .Case<LL::SubOp>([&](auto) {
        pair([&](unsigned d, unsigned s) { a.sub(d, s); }, [&](unsigned d, unsigned s) { a.sbb(d, s); });
      })
      .Case<LL::AndOp>([&](auto) {
        pair([&](unsigned d, unsigned s) { a.and_(d, s); }, [&](unsigned d, unsigned s) { a.and_(d, s); });
      })
      .Case<LL::OrOp>([&](auto) {
        pair([&](unsigned d, unsigned s) { a.or_(d, s); }, [&](unsigned d, unsigned s) { a.or_(d, s); });
      })
      .Case<LL::XOrOp>([&](auto) {
        pair([&](unsigned d, unsigned s) { a.xor_(d, s); }, [&](unsigned d, unsigned s) { a.xor_(d, s); });
      })
      .Case<LL::MulOp>([&](auto) {
        // (a1:a0) * (b1:b0) = a0*b0 + (a0*b1 + a1*b0) << 64.
        lo(T2, op.getOperand(0));
        hi(T4, op.getOperand(1));
        a.imul(T4, T2);  // a0 * b1
        hi(T5, op.getOperand(0));
        lo(T1, op.getOperand(1));
        a.imul(T5, T1);  // a1 * b0
        a.add(T4, T5);
        a.mulRdx(T2);  // rdx:rax = b0 * a0
        a.add(T3, T4);
        store(T1, T3, op.getResult(0));
      })
      .Case<LL::ShlOp>([&](auto) { shift(4); })
      .Case<LL::LShrOp>([&](auto) { shift(5); })
      .Case<LL::AShrOp>([&](auto) { shift(7); })
      .Default([&](Operation *o) { unsupported("i128 " + o->getName().getStringRef().str()); });
  return true;
}

void FuncGenX64::emitIntrinsic(LL::CallIntrinsicOp c) {
  StringRef name = c.getIntrin();
  auto args = c.getArgs();
  if (c.getNumResults() != 1) unsupported("intrinsic " + name.str());
  Value res = c.getResult(0);
  unsigned n = isa<IntegerType>(res.getType()) ? intWidth(res.getType()) : 64;
  auto shiftDown = [&](unsigned r) {
    if (n < 64) a.shiftImm(5, r, uint8_t(64 - n));
  };
  if (name.starts_with("llvm.ctlz.")) {
    get(T2, args[0]);
    a.movImm(T4, ~uint64_t(0));
    a.bsr(T1, T2);
    a.cmov(X::E, T1, T4);  // zero input: -1, so 63 - (-1) = 64
    a.movImm(T2, 63);
    a.sub(T2, T1);
    if (n < 64) a.subImm(T2, int32_t(64 - n));
    put(T2, res);
  } else if (name.starts_with("llvm.cttz.")) {
    get(T2, args[0]);
    if (n < 64) {
      a.movImm(T4, uint64_t(1) << n);
      a.or_(T2, T4);
    }
    a.movImm(T4, 64);
    a.bsf(T1, T2);
    a.cmov(X::E, T1, T4);
    put(T1, res);
  } else if (name.starts_with("llvm.ctpop.")) {
    get(T2, args[0]);
    a.popcnt(T1, T2);
    put(T1, res);
  } else if (name.starts_with("llvm.bswap.")) {
    get(T1, args[0]);
    a.bswap(T1);
    shiftDown(T1);
    put(T1, res);
  } else if (name.starts_with("llvm.bitreverse.")) {
    get(T1, args[0]);
    // Swap bits, pairs and nibbles within each byte, then the bytes.
    static const uint64_t masks[3] = {0x5555555555555555ull, 0x3333333333333333ull, 0x0F0F0F0F0F0F0F0Full};
    for (unsigned k = 0; k < 3; k++) {
      uint8_t s = uint8_t(1u << k);
      a.movImm(T4, masks[k]);
      a.mov(T2, T1);
      a.shiftImm(5, T2, s);
      a.and_(T2, T4);
      a.and_(T1, T4);
      a.shiftImm(4, T1, s);
      a.or_(T1, T2);
    }
    a.bswap(T1);
    shiftDown(T1);
    put(T1, res);
  } else if (name.starts_with("llvm.fshl.") || name.starts_with("llvm.fshr.")) {
    bool left = name.starts_with("llvm.fshl.");
    get(T1, args[0]);
    get(T3, args[1]);
    get(T2, args[2]);  // rcx
    if (n == 64) {
      if (left) {
        a.shldCl(T1, T3);
      } else {
        a.shrdCl(T3, T1);
        a.mov(T1, T3);
      }
    } else if (n <= 32) {
      // a:b in 64 bits, shifted by s mod n.
      a.shiftImm(4, T1, uint8_t(n));
      a.or_(T1, T3);
      a.mov(T4, T1);
      a.mov(T1, T2);
      a.movImm(T5, n);
      a.xor_(T3, T3);
      a.div(T5);  // rdx = s mod n
      a.mov(T2, T3);
      a.mov(T1, T4);
      if (left) {
        a.shiftCl(4, T1);
        a.shiftImm(5, T1, uint8_t(n));
      } else {
        a.shiftCl(5, T1);
      }
    } else {
      unsupported("intrinsic " + name.str());
    }
    put(T1, res);
  } else if (name.starts_with("llvm.fptosi.sat.i64.") || name.starts_with("llvm.fptoui.sat.i64.")) {
    bool dbl = args[0].getType().isF64();
    bool sgn = name.starts_with("llvm.fptosi");
    getF(XF1, args[0]);
    std::vector<size_t> toEnd;
    a.xor_(T1, T1);
    a.ucomi(XF1, XF1, dbl);
    toEnd.push_back(a.jcc(X::P));  // NaN: 0
    // Upper limit 2^63 (signed) or 2^64 (unsigned): the maximum.
    a.movImm(T2, sgn ? (dbl ? 0x43E0000000000000ull : 0x5F000000ull) : (dbl ? 0x43F0000000000000ull : 0x5F800000ull));
    a.movqToX(XF2, T2);
    a.movImm(T1, sgn ? uint64_t(INT64_MAX) : ~uint64_t(0));
    a.ucomi(XF1, XF2, dbl);
    toEnd.push_back(a.jcc(X::AE));
    if (sgn) {
      a.cvttf2si(T1, XF1, dbl);  // below -2^63 gives INT64_MIN, as saturation wants
    } else {
      a.xor_(T1, T1);
      a.xor_(T2, T2);
      a.movqToX(XF2, T2);
      a.ucomi(XF1, XF2, dbl);
      toEnd.push_back(a.jcc(X::BE));  // at or below zero: 0
      a.movImm(T2, dbl ? 0x43E0000000000000ull : 0x5F000000ull);
      a.movqToX(XF2, T2);
      a.ucomi(XF1, XF2, dbl);
      size_t big = a.jcc(X::AE);
      a.cvttf2si(T1, XF1, dbl);
      toEnd.push_back(a.jmp());
      patch32(big, int64_t(a.here()));
      a.subsd(XF1, XF2, dbl);
      a.cvttf2si(T1, XF1, dbl);
      a.movImm(T2, uint64_t(1) << 63);
      a.xor_(T1, T2);
    }
    for (size_t j : toEnd) patch32(j, int64_t(a.here()));
    put(T1, res);
  } else if (name == "llvm.frameaddress.p0") {
    auto k = args[0].getDefiningOp<LL::ConstantOp>();
    auto ia = k ? dyn_cast<IntegerAttr>(k.getValue()) : IntegerAttr();
    if (!ia || ia.getInt() != 0) unsupported("frameaddress above level 0");
    a.mov(T1, X::RBP);
    put(T1, res);
  } else {
    unsupported("intrinsic " + name.str());
  }
}

void FuncGenX64::run() {
  if (fn.isVarArg()) unsupported("variadic function");
  layout();
  uint64_t start = a.here();
  std::vector<ModuleGen::Row> rows;
  auto row = [&](Location l) {
    auto r = lineOf(l);
    if (!r) return;
    uint32_t file = mg.fileId(r->first);
    uint64_t at = a.here() - start;
    if (!rows.empty() && std::get<1>(rows.back()) == r->second && std::get<2>(rows.back()) == file) return;
    if (!rows.empty() && std::get<0>(rows.back()) == at) rows.pop_back();
    rows.push_back({at, r->second, file});
  };
  row(fn.getLoc());
  prologue();
  for (Block &b : fn.getBody()) {
    blockStart[&b] = a.here();
    for (Operation &op : b) {
      row(op.getLoc());
      emitOp(op);
    }
  }
  for (const Fixup &f : fixups) patch32(f.field, int64_t(blockStart[f.dest]));
  auto lk = fn.getLinkage();
  std::string sym = symName(fn.getSymName());
  w.define(sym, Sect::Text, start, !isLocalLinkage(lk), isWeakLinkage(lk), isHidden(fn));
  mg.lines.push_back({w.symbol(sym), a.here() - start, std::move(rows)});
}
