// MirAlloc.cpp: see MirAlloc.h.
#include "MirAlloc.h"

#include "llvm/ADT/BitVector.h"

#include <algorithm>

namespace marmot {
namespace tier0 {
namespace mir {

bool isCall(const Inst &in, const TargetRegs &t) {
  return in.op == Op::Call || in.op == Op::MemCall || in.op == Op::Ret || (t.fmaIsCall && in.op == Op::FMA);
}

namespace {

// The values an instruction reads where it is emitted: its operands, the
// operands of a compare fused into it, and the pieces of calls, returns,
// switches and edges.
template <class F> void forUses(const Func &f, const Alloc &a, const Inst &in, F use) {
  auto one = [&](VId v) {
    if (v == None) return;
    const Val &x = f.vals[v];
    if (x.def != ~0u && a.fused[x.def]) {
      const Inst &d = f.insts[x.def];
      for (VId o : {d.a, d.b, d.c})
        if (o != None) use(o);
      return;
    }
    use(v);
  };
  one(in.a);
  one(in.b);
  one(in.c);
  auto edge = [&](uint32_t e) {
    const Edge &ed = f.edges[e];
    for (uint32_t i = ed.argBegin; i < ed.argEnd; i++) one(f.edgeArgs[i]);
  };
  switch (in.op) {
  case Op::Call: {
    const CallRec &c = f.calls[in.extra];
    for (VId v : c.args) one(v);
    one(c.callee);
    break;
  }
  case Op::Ret:
    for (VId v : f.rets[in.extra].pieces) one(v);
    break;
  case Op::Br: edge(in.extra); break;
  case Op::CondBr:
    edge(in.extra);
    edge(in.aux2);
    break;
  case Op::Switch:
    for (uint32_t e : f.switches[in.extra].edges) edge(e);
    break;
  default: break;
  }
}

// Results of an instruction.
template <class F> void forDefs(const Func &f, const Inst &in, F def) {
  if (in.dst != None) def(in.dst);
  if (in.op == Op::CmpXchg) def(VId(in.extra));
  if (in.op == Op::Call)
    for (VId v : f.calls[in.extra].results) def(v);
}

} // namespace

void allocate(const Func &f, const TargetRegs &t, Alloc &a) {
  const size_t nv = f.vals.size();
  a.loc.assign(nv, Loc());
  a.fused.assign(f.insts.size(), 0);
  // A compare whose only use is the conditional branch of its block is
  // emitted at the branch (flags, or cbz/cbnz against zero).
  for (const BB &b : f.blocks) {
    if (b.end == b.begin) continue;
    const Inst &term = f.insts[b.end - 1];
    if (term.op != Op::CondBr) continue;
    const Val &c = f.vals[term.a];
    if (c.def == ~0u || c.uses != 1 || c.def < b.begin || c.def >= b.end) continue;
    if (f.insts[c.def].op == Op::ICmp) a.fused[c.def] = 1;
  }
  // Remade at each use.
  for (const Inst &in : f.insts)
    if (!in.dead && (in.op == Op::Const || in.op == Op::Frame)) a.loc[in.dst].k = Loc::Remat;

  // Positions and intervals.
  const unsigned nb = unsigned(f.blocks.size());
  std::vector<uint32_t> start(nv, UINT32_MAX), end(nv, 0);
  std::vector<uint32_t> defBlock(nv, ~0u);
  std::vector<std::pair<uint32_t, uint32_t>> span(nb);  // block start, terminator position
  std::vector<uint32_t> clobber;                        // positions of calls
  std::vector<std::pair<VId, uint32_t>> far;            // (value, block) uses in another block
  auto wanted = [&](VId v) { return a.loc[v].k == Loc::None; };
  // A call followed by unreachable (a panic) does not return, so nothing it
  // clobbers is read again: values live over it need no callee saved
  // register. (A recovered panic leaves through rt_setjmp, and functions
  // that call it keep every value in memory.)
  auto noReturn = [&](const BB &b, uint32_t i) {
    if (f.insts[i].op != Op::Call) return false;
    for (uint32_t j = i + 1; j < b.end; j++)
      if (!f.insts[j].dead) return f.insts[j].op == Op::Unreachable;
    return false;
  };
  auto touch = [&](VId v, uint32_t p) {
    start[v] = std::min(start[v], p);
    end[v] = std::max(end[v], p);
  };
  // Numbering order: a block that only panics (one predecessor, a call and
  // unreachable at its end) comes right after its predecessor, although it
  // is emitted at the end. The values it passes to the panic then keep
  // their short intervals instead of reaching over the rest of the
  // function and its calls. Any order is sound: intervals also cover the
  // block boundaries where the dataflow finds a value live.
  std::vector<unsigned> seq;
  {
    std::vector<char> cold(nb, 0);
    std::vector<llvm::SmallVector<unsigned, 1>> after(nb);
    for (unsigned bi = 1; bi < nb; bi++) {
      const BB &b = f.blocks[bi];
      if (b.preds.size() != 1 || b.preds[0] == bi || b.end == b.begin || f.insts[b.end - 1].op != Op::Unreachable)
        continue;
      uint32_t j = b.end - 1;  // the unreachable; the call is the live instruction before it
      while (j > b.begin && f.insts[j - 1].dead) j--;
      if (j == b.begin || f.insts[j - 1].op != Op::Call) continue;
      cold[bi] = 1;
      after[b.preds[0]].push_back(bi);
    }
    for (unsigned bi = 0; bi < nb; bi++) {
      if (cold[bi]) continue;
      seq.push_back(bi);
      for (unsigned c : after[bi]) seq.push_back(c);
    }
  }
  uint32_t p = 0;
  for (unsigned bi : seq) {
    const BB &b = f.blocks[bi];
    uint32_t s = p++;
    span[bi].first = s;
    for (VId v : b.params) {
      touch(v, s);
      defBlock[v] = bi;
    }
    for (uint32_t i = b.begin; i < b.end; i++) {
      const Inst &in = f.insts[i];
      if (in.dead || a.fused[i]) continue;
      uint32_t at = p++;
      if (isCall(in, t) && !noReturn(b, i)) clobber.push_back(at);
      forUses(f, a, in, [&](VId v) {
        if (!wanted(v)) return;
        touch(v, at);
        if (defBlock[v] != bi) far.push_back({v, bi});
      });
      forDefs(f, in, [&](VId v) {
        if (!wanted(v)) return;
        touch(v, at);
        defBlock[v] = bi;
      });
    }
    span[bi].second = p - 1;
  }
  // Block parameters are written at the end of every predecessor.
  for (unsigned bi = 0; bi < nb; bi++)
    for (VId v : f.blocks[bi].params)
      for (uint32_t pr : f.blocks[bi].preds) touch(v, span[pr].second);
  // Liveness of the values used in another block than their own.
  if (!far.empty()) {
    std::vector<uint32_t> gid(nv, ~0u);
    std::vector<VId> gval;
    for (auto [v, b] : far)
      if (gid[v] == ~0u) {
        gid[v] = uint32_t(gval.size());
        gval.push_back(v);
      }
    const unsigned n = unsigned(gval.size());
    std::vector<llvm::BitVector> gen(nb, llvm::BitVector(n)), in(nb, llvm::BitVector(n)), out(nb, llvm::BitVector(n));
    std::vector<std::vector<uint32_t>> kill(nb);
    for (uint32_t g = 0; g < n; g++)
      if (defBlock[gval[g]] != ~0u) kill[defBlock[gval[g]]].push_back(g);
    for (auto [v, b] : far) gen[b].set(gid[v]);
    in = gen;
    // Successors from the terminators.
    std::vector<llvm::SmallVector<uint32_t, 2>> succ(nb);
    for (unsigned bi = 0; bi < nb; bi++) {
      const BB &b = f.blocks[bi];
      if (b.end == b.begin) continue;
      const Inst &term = f.insts[b.end - 1];
      auto e = [&](uint32_t x) { succ[bi].push_back(f.edges[x].block); };
      if (term.op == Op::Br) e(term.extra);
      if (term.op == Op::CondBr) {
        e(term.extra);
        e(term.aux2);
      }
      if (term.op == Op::Switch)
        for (uint32_t x : f.switches[term.extra].edges) e(x);
    }
    llvm::BitVector o(n);
    for (bool changed = true; changed;) {
      changed = false;
      for (unsigned bi = nb; bi-- > 0;) {
        o.reset();
        for (uint32_t s : succ[bi]) o |= in[s];
        if (o == out[bi]) continue;
        out[bi] = o;
        for (uint32_t g : kill[bi]) o.reset(g);
        o |= gen[bi];
        in[bi] = o;
        changed = true;
      }
    }
    for (unsigned bi = 0; bi < nb; bi++) {
      for (int g = in[bi].find_first(); g >= 0; g = in[bi].find_next(g)) touch(gval[g], span[bi].first);
      for (int g = out[bi].find_first(); g >= 0; g = out[bi].find_next(g)) touch(gval[g], span[bi].second);
    }
  }

  // MARMOT_TIER0_TIEPARAM=1: loop counters (below). Block parameters by value.
  static const bool tieParam = [] {
    const char *e = getenv("MARMOT_TIER0_TIEPARAM");
    return e && *e == '1';
  }();
  std::vector<uint32_t> paramOf, paramIdx;
  std::vector<char> pinned;
  if (tieParam) {
    pinned.assign(nv, 0);
    paramOf.assign(nv, ~0u);
    paramIdx.assign(nv, 0);
    for (uint32_t b = 0; b < nb; b++)
      for (uint32_t i = 0; i < f.blocks[b].params.size(); i++) {
        paramOf[f.blocks[b].params[i]] = b;
        paramIdx[f.blocks[b].params[i]] = i;
      }
  }
  // Linear scan per register class.
  std::vector<VId> order;
  for (VId v = 0; v < nv; v++)
    if (wanted(v) && start[v] != UINT32_MAX) order.push_back(v);
  std::stable_sort(order.begin(), order.end(), [&](VId x, VId y) { return start[x] < start[y]; });
  auto crosses = [&](VId v) {
    auto it = std::upper_bound(clobber.begin(), clobber.end(), start[v]);
    return it != clobber.end() && *it < end[v];
  };
  std::vector<VId> spilled;
  auto scan = [&](bool fp) {
    const RegClass &rc = fp ? t.fpr : t.gpr;
    uint64_t freeCaller = 0, freeCallee = 0;
    for (unsigned r : rc.caller) freeCaller |= uint64_t(1) << r;
    for (unsigned r : rc.callee) freeCallee |= uint64_t(1) << r;
    uint64_t calleeMask = freeCallee;
    std::vector<VId> active;
    VId cur = None;
    auto pick = [&](uint64_t &set, const std::vector<unsigned> &pref) -> int {
      if (cur < a.hint.size() && a.hint[cur] >= 0 && (set >> unsigned(a.hint[cur]) & 1)) {
        set &= ~(uint64_t(1) << unsigned(a.hint[cur]));
        return a.hint[cur];
      }
      for (unsigned r : pref)
        if (set >> r & 1) {
          set &= ~(uint64_t(1) << r);
          return int(r);
        }
      return -1;
    };
    for (VId v : order) {
      if ((f.vals[v].cls != Cls::Int) != fp) continue;
      if (f.hasSetjmp) {  // a longjmp restores the callee saved registers: memory only
        spilled.push_back(v);
        continue;
      }
      for (size_t k = 0; k < active.size();) {
        VId x = active[k];
        if (end[x] < start[v]) {
          unsigned r = a.loc[x].reg;
          if (calleeMask >> r & 1)
            freeCallee |= uint64_t(1) << r;
          else
            freeCaller |= uint64_t(1) << r;
          active[k] = active.back();
          active.pop_back();
        } else {
          k++;
        }
      }
      bool cross = crosses(v);
      cur = v;
      // A loop counter: v = x op y computed in the block of x, a block
      // parameter used only here, and every edge into that block either
      // passes v back as x or comes from a block before v. x is otherwise live to the back edge only
      // because the edge writes it, and the value written is v: v in x's
      // register makes the copy before the operation and the move on the
      // back edge disappear.
      if (tieParam && !fp && f.vals[v].def != ~0u) {
        const Inst &d = f.insts[f.vals[v].def];
        VId x = d.a;
        bool two = d.op == Op::Add || d.op == Op::Sub || d.op == Op::And || d.op == Op::Or || d.op == Op::Xor ||
                   d.op == Op::Mul || d.op == Op::Shl || d.op == Op::LShr || d.op == Op::AShr;
        if (two && d.dst == v && x != None && x < nv && x != d.b && paramOf[x] != ~0u && f.vals[x].uses == 1 &&
            a.loc[x].k == Loc::Reg && f.vals[x].cls == Cls::Int && !(cross && !(calleeMask >> a.loc[x].reg & 1))) {
          uint32_t h = paramOf[x], idx = paramIdx[x];
          // v must be computed in x's own block: then each computation of v
          // follows a fresh x. A use of x further on (in a loop body after
          // the block, or for a function argument, x in the entry block) can
          // run many times for one x, and v would overwrite x on the first.
          bool ok = h != 0 && defBlock[v] == h;
          for (uint32_t pr : f.blocks[h].preds) {
            const BB &pb = f.blocks[pr];
            if (pb.end == pb.begin) continue;
            const Inst &term = f.insts[pb.end - 1];
            auto chk = [&](uint32_t e) {
              const Edge &ed = f.edges[e];
              if (ed.block != h) return;
              if (f.edgeArgs[ed.argBegin + idx] == v) return;
              if (span[pr].second < start[v]) return;  // an edge from before v (the loop entry)
              ok = false;
            };
            if (term.op == Op::Br) chk(term.extra);
            else if (term.op == Op::CondBr) {
              chk(term.extra);
              chk(term.aux2);
            } else if (term.op == Op::Switch) {
              for (uint32_t e : f.switches[term.extra].edges) chk(e);
            }
          }
          auto it = std::find(active.begin(), active.end(), x);
          if (ok && it != active.end()) {
            *it = active.back();
            active.pop_back();
            a.loc[v].k = Loc::Reg;
            a.loc[v].reg = a.loc[x].reg;
            active.push_back(v);
            // v now also holds x's register for x's write on the back
            // edge: never give it up to a later interval.
            pinned[v] = 1;
            continue;
          }
        }
      }
      // A two-address result in the register of its first operand, which
      // ends here: no copy before the operation (and none at a loop's back
      // edge when the operand is the loop counter).
      if (t.tieFirst && !fp && f.vals[v].def != ~0u) {
        const Inst &d = f.insts[f.vals[v].def];
        bool two = d.op == Op::Add || d.op == Op::Sub || d.op == Op::And || d.op == Op::Or || d.op == Op::Xor ||
                   d.op == Op::Mul || d.op == Op::Shl || d.op == Op::LShr || d.op == Op::AShr;
        VId x = d.a;
        if (two && d.dst == v && x != None && x < nv && a.loc[x].k == Loc::Reg && end[x] == start[v] &&
            f.vals[x].cls == Cls::Int && !(cross && !(calleeMask >> a.loc[x].reg & 1))) {
          auto it = std::find(active.begin(), active.end(), x);
          if (it != active.end()) {
            *it = active.back();
            active.pop_back();
            a.loc[v].k = Loc::Reg;
            a.loc[v].reg = a.loc[x].reg;
            active.push_back(v);
            continue;
          }
        }
      }
      int r = cross ? -1 : pick(freeCaller, rc.caller);
      if (r < 0) r = pick(freeCallee, rc.callee);
      if (r >= 0) {
        a.loc[v].k = Loc::Reg;
        a.loc[v].reg = uint8_t(r);
        active.push_back(v);
        continue;
      }
      // Full: the interval that ends last (and may take v's kind of
      // register) gives its register up.
      int fk = -1;
      for (size_t k = 0; k < active.size(); k++) {
        VId x = active[k];
        if (cross && !(calleeMask >> a.loc[x].reg & 1)) continue;
        if (!pinned.empty() && pinned[x]) continue;
        if (fk < 0 || end[x] > end[active[size_t(fk)]]) fk = int(k);
      }
      if (fk >= 0 && end[active[size_t(fk)]] > end[v]) {
        VId x = active[size_t(fk)];
        a.loc[v] = a.loc[x];
        a.loc[x] = Loc();
        spilled.push_back(x);
        active[size_t(fk)] = v;
      } else {
        spilled.push_back(v);
      }
    }
  };
  scan(false);
  scan(true);
  for (VId v = 0; v < nv; v++)
    if (a.loc[v].k == Loc::Reg) {
      unsigned r = a.loc[v].reg;
      bool fp = f.vals[v].cls != Cls::Int;
      const RegClass &rc = fp ? t.fpr : t.gpr;
      if (std::find(rc.callee.begin(), rc.callee.end(), r) != rc.callee.end())
        (fp ? a.calleeFpr : a.calleeGpr) |= 1u << r;
    }
  // Spill slots, shared by intervals that do not overlap.
  std::stable_sort(spilled.begin(), spilled.end(), [&](VId x, VId y) { return start[x] < start[y]; });
  std::vector<uint32_t> slotEnd;  // per slot, the end of its current interval
  for (VId v : spilled) {
    uint32_t s = UINT32_MAX;
    for (uint32_t k = 0; k < slotEnd.size(); k++)
      if (slotEnd[k] < start[v]) {
        s = k;
        break;
      }
    if (s == UINT32_MAX) {
      s = uint32_t(slotEnd.size());
      slotEnd.push_back(0);
    }
    slotEnd[s] = end[v];
    a.loc[v].k = Loc::Slot;
    a.loc[v].slot = s;
  }
  a.slots = uint32_t(slotEnd.size());
}

} // namespace mir
} // namespace tier0
} // namespace marmot
