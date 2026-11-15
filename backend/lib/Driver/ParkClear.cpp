// Zeroes dead spill slots at park calls (MARMOT_PARKCLEAR=1).
//
// A goroutine parked in a blocking runtime call (a select, a channel
// operation, a sleep) keeps its frames on the stack, and the collector scans
// them conservatively. A register allocator spill slot whose value is dead
// still holds its last pointer, so a large buffer the program has dropped
// (a slice set to nil after the spill) stays alive as long as the goroutine
// waits. This machine pass runs after stack slot coloring, while frame
// indices are still symbolic: before each call to a park entry point it
// stores zero into every spill slot that is dead there.
//
// A slot is dead at a call when no path from the call reads it before
// writing it in full. Liveness is a backward dataflow over the function's
// blocks: a recognised full store to the slot defines it, and any other
// instruction that names the slot (a load, a partial store, anything not
// recognised) uses it, so an unknown access keeps the slot alive. A function
// that calls setjmp (defer frames) can resume after the setjmp from any
// later call, by longjmp from a panic: the slots live just after each setjmp
// call stay live at every park call. A function with a returns_twice call
// that cannot be found, or with landing pads, is left as it is.

#include "llvm/CodeGen/MachineFrameInfo.h"
#include "llvm/CodeGen/MachineFunction.h"
#include "llvm/CodeGen/MachineFunctionPass.h"
#include "llvm/CodeGen/MachineInstrBuilder.h"
#include "llvm/CodeGen/Passes.h"
#include "llvm/CodeGen/TargetInstrInfo.h"
#include "llvm/CodeGen/TargetPassConfig.h"
#include "llvm/CodeGen/TargetRegisterInfo.h"
#include "llvm/CodeGen/TargetSubtargetInfo.h"
#include "llvm/ADT/BitVector.h"
#include "llvm/ADT/PostOrderIterator.h"
#include "llvm/IR/Function.h"
#include "llvm/Target/RegisterTargetPassConfigCallback.h"
#include "llvm/Target/TargetMachine.h"

#include <cstdlib>
#include <cstring>

using namespace llvm;

namespace {

// The runtime's park entry points as compiled code calls them (a package
// local symbol may carry a .g<hash> suffix).
bool isParkCall(StringRef name) {
  if (name.size() > 10 && name[name.size() - 10] == '.' && name[name.size() - 9] == 'g')
    name = name.drop_back(10);
  static const char *const parks[] = {
      "runtime.selectgo",    "runtime.chanrecv",        "runtime.chanrecvBuf",
      "runtime.chansend",    "runtime.chansendBuf",     "runtime.block",
      "time.Sleep",          "sync.(*WaitGroup).Wait",  "sync.(*Cond).Wait",
  };
  for (const char *p : parks)
    if (name == p)
      return true;
  return false;
}

const GlobalValue *callee(const MachineInstr &mi) {
  for (const MachineOperand &op : mi.operands())
    if (op.isGlobal())
      return op.getGlobal();
  return nullptr;
}

struct ParkClear : MachineFunctionPass {
  static char ID;
  ParkClear() : MachineFunctionPass(ID) {}
  StringRef getPassName() const override { return "marmot park slot clear"; }

  // The store-zero instruction of this target, found by name once (the
  // opcode enums are target private).
  unsigned storeOp = 0, zeroReg = 0;
  bool x86 = false;
  bool targetKnown(const MachineFunction &mf) {
    if (storeOp)
      return true;
    const TargetInstrInfo *tii = mf.getSubtarget().getInstrInfo();
    const TargetRegisterInfo *tri = mf.getSubtarget().getRegisterInfo();
    Triple::ArchType arch = mf.getTarget().getTargetTriple().getArch();
    const char *want = nullptr;
    if (arch == Triple::aarch64) {
      want = "STRXui";
      for (unsigned r = 1; r < tri->getNumRegs(); ++r)
        if (StringRef(tri->getName(r)) == "XZR")
          zeroReg = r;
      if (!zeroReg)
        return false;
    } else if (arch == Triple::x86_64) {
      want = "MOV64mi32";
      x86 = true;
    } else {
      return false;
    }
    for (unsigned op = 0; op < tii->getNumOpcodes(); ++op)
      if (StringRef(tii->getName(op)) == want)
        storeOp = op;
    return storeOp != 0;
  }

  bool runOnMachineFunction(MachineFunction &mf) override {
    MachineFrameInfo &mfi = mf.getFrameInfo();
    std::vector<MachineInstr *> parks, setjmps;
    bool ehPads = false;
    for (MachineBasicBlock &mbb : mf) {
      ehPads |= mbb.isEHPad();
      for (MachineInstr &mi : mbb) {
        if (!mi.isCall())
          continue;
        const GlobalValue *g = callee(mi);
        if (!g)
          continue;
        if (isParkCall(g->getName()))
          parks.push_back(&mi);
        if (auto *f = dyn_cast<Function>(g); f && f->hasFnAttribute(Attribute::ReturnsTwice))
          setjmps.push_back(&mi);
      }
    }
    if (parks.empty() || ehPads || !targetKnown(mf))
      return false;
    if (mf.exposesReturnsTwice() && setjmps.empty())
      return false;
    // The spill slots, numbered densely.
    std::vector<int> slots;
    DenseMap<int, unsigned> index;
    for (int fi = 0; fi < mfi.getObjectIndexEnd(); ++fi)
      if (mfi.isSpillSlotObjectIndex(fi) && !mfi.isDeadObjectIndex(fi) && mfi.getObjectSize(fi) == 8) {
        index[fi] = slots.size();
        slots.push_back(fi);
      }
    if (slots.empty())
      return false;
    const TargetInstrInfo *tii = mf.getSubtarget().getInstrInfo();
    // Per instruction: the slot it fully stores (-1 for none), the slots it
    // uses otherwise.
    auto effect = [&](const MachineInstr &mi, BitVector &def, BitVector &use) {
      def.reset();
      use.reset();
      int sfi = -1;
      if (tii->isStoreToStackSlot(mi, sfi) && index.count(sfi) && mi.hasOneMemOperand() &&
          (*mi.memoperands_begin())->getSize().hasValue() &&
          (*mi.memoperands_begin())->getSize().getValue() == 8) {
        def.set(index[sfi]);
        // Other frame index operands, if any, are uses.
        for (const MachineOperand &op : mi.operands())
          if (op.isFI() && op.getIndex() != sfi && index.count(op.getIndex()))
            use.set(index[op.getIndex()]);
        return;
      }
      for (const MachineOperand &op : mi.operands())
        if (op.isFI() && index.count(op.getIndex()))
          use.set(index[op.getIndex()]);
    };
    unsigned n = slots.size();
    DenseMap<const MachineBasicBlock *, BitVector> liveIn, liveOut;
    for (MachineBasicBlock &mbb : mf) {
      liveIn[&mbb] = BitVector(n);
      liveOut[&mbb] = BitVector(n);
    }
    BitVector def(n), use(n);
    auto transfer = [&](const MachineBasicBlock &mbb) {
      BitVector live = liveOut[&mbb];
      for (auto it = mbb.rbegin(); it != mbb.rend(); ++it) {
        effect(*it, def, use);
        live.reset(def);
        live |= use;
      }
      return live;
    };
    for (bool changed = true; changed;) {
      changed = false;
      for (MachineBasicBlock *mbb : post_order(&mf)) {
        BitVector out(n);
        for (MachineBasicBlock *s : mbb->successors())
          out |= liveIn[s];
        liveOut[mbb] = out;
        BitVector in = transfer(*mbb);
        if (in != liveIn[mbb]) {
          liveIn[mbb] = in;
          changed = true;
        }
      }
    }
    // Live just before each instruction of interest: walk its block back.
    auto liveBefore = [&](MachineInstr *at, bool after) {
      MachineBasicBlock &mbb = *at->getParent();
      BitVector live = liveOut[&mbb];
      for (auto it = mbb.rbegin(); it != mbb.rend(); ++it) {
        if (&*it == at && after)
          return live;
        effect(*it, def, use);
        live.reset(def);
        live |= use;
        if (&*it == at)
          return live;
      }
      return live;
    };
    BitVector keep(n);
    for (MachineInstr *sj : setjmps)
      keep |= liveBefore(sj, true);
    bool changedAny = false;
    for (MachineInstr *park : parks) {
      BitVector live = liveBefore(park, false);
      live |= keep;
      for (unsigned i = 0; i < n; ++i) {
        if (live.test(i))
          continue;
        int fi = slots[i];
        MachineMemOperand *mmo = mf.getMachineMemOperand(MachinePointerInfo::getFixedStack(mf, fi),
                                                         MachineMemOperand::MOStore, 8,
                                                         mfi.getObjectAlign(fi));
        MachineBasicBlock &mbb = *park->getParent();
        DebugLoc dl = park->getDebugLoc();
        if (x86)
          BuildMI(mbb, park, dl, tii->get(storeOp))
              .addFrameIndex(fi).addImm(1).addReg(0).addImm(0).addReg(0)
              .addImm(0)
              .addMemOperand(mmo);
        else
          BuildMI(mbb, park, dl, tii->get(storeOp))
              .addReg(zeroReg).addFrameIndex(fi).addImm(0)
              .addMemOperand(mmo);
        changedAny = true;
      }
    }
    return changedAny;
  }
};

char ParkClear::ID = 0;

bool parkClearOn() {
  static const bool on = [] {
    const char *e = getenv("MARMOT_PARKCLEAR");
    return e && *e == '1';
  }();
  return on;
}

} // namespace

// Registers the callback (once for every target machine of the process; it
// adds the pass only when the switch is on). Pipeline.cpp calls this before
// it builds a code generation pipeline. Not a global object: LLVM keeps the
// callbacks in a global vector with a dynamic initializer, and with static
// LLVM libraries that vector can be constructed after a global of ours,
// which drops the registration (MARMOT_PARKCLEAR=1 did nothing).
bool marmotParkClearLinked() {
  static RegisterTargetPassConfigCallback callback(
      [](TargetMachine &, PassManagerBase &, TargetPassConfig *pc) {
        if (parkClearOn())
          pc->insertPass(&StackSlotColoringID, IdentifyingPassPtr(new ParkClear()));
      });
  return parkClearOn();
}
