// Jam.cpp: loop-jam. Unroll-and-jam of chunk loops made by loop-parallelize.
//
// A chunk loop's iterations are independent (Rule P), so running JamCount of
// them side by side in one copy of the inner loop is legal; LLVM's own
// legality check (isSafeToUnrollAndJam) is still applied. The jammed inner
// loop holds JamCount independent recurrences, which the SLP vectorizer packs
// into vector lanes. Each original iteration keeps its own operations in
// their original order, so floating point results do not change.
//
// LLVM's LoopUnrollAndJamPass is not in the default pipeline and its pragma
// path needs a constant trip multiple; this pass calls the utility directly.

#include "marmot/Loops/Loops.h"
#include "Internal.h"

#include "llvm/Analysis/AliasAnalysis.h"
#include "llvm/Analysis/AssumptionCache.h"
#include "llvm/Analysis/DependenceAnalysis.h"
#include "llvm/Analysis/LoopInfo.h"
#include "llvm/Analysis/OptimizationRemarkEmitter.h"
#include "llvm/Analysis/ScalarEvolution.h"
#include "llvm/Analysis/TargetTransformInfo.h"
#include "llvm/IR/Dominators.h"
#include "llvm/IR/PassManager.h"
#include "llvm/Support/raw_ostream.h"
#include "llvm/Transforms/Scalar/LoopPassManager.h"
#include "llvm/Transforms/Scalar/SimpleLoopUnswitch.h"
#include "llvm/Transforms/Scalar/SimplifyCFG.h"
#include "llvm/Transforms/Utils/LoopSimplify.h"
#include "llvm/Transforms/Utils/LoopUtils.h"
#include "llvm/Transforms/Utils/UnrollLoop.h"

using namespace llvm;

namespace marmot {
namespace loops {
namespace {

constexpr const char *JamMD = "marmot.jam";

class JamPass : public PassInfoMixin<JamPass> {
public:
  explicit JamPass(Options o) : opts(o) {}
  PreservedAnalyses run(Function &F, FunctionAnalysisManager &FAM) {
    if (!F.hasFnAttribute("marmot-par-body"))
      return PreservedAnalyses::all();
    // An inner loop with an unknown trip count sits behind a "trip count > 0"
    // guard, which unroll-and-jam cannot handle. The guard is invariant in
    // the chunk loop, so unswitch it: one copy of the chunk loop has the
    // inner loop unguarded.
    {
      FunctionPassManager fpm;
      fpm.addPass(createFunctionToLoopPassAdaptor(SimpleLoopUnswitchPass(/*NonTrivial=*/true),
                                                  /*UseMemorySSA=*/true));
      fpm.addPass(SimplifyCFGPass());
      fpm.run(F, FAM);
    }
    auto &LI = FAM.getResult<LoopAnalysis>(F);
    auto &SE = FAM.getResult<ScalarEvolutionAnalysis>(F);
    auto &DT = FAM.getResult<DominatorTreeAnalysis>(F);
    auto &AC = FAM.getResult<AssumptionAnalysis>(F);
    auto &TTI = FAM.getResult<TargetIRAnalysis>(F);
    auto &AA = FAM.getResult<AAManager>(F);
    OptimizationRemarkEmitter ORE(&F);
    bool changed = false;
    SmallVector<Loop *, 4> todo;
    for (Loop *L : LI)
      if (L->getLoopLatch() && L->getLoopLatch()->getTerminator()->getMetadata(JamMD))
        todo.push_back(L);
    for (Loop *L : todo) {
      L->getLoopLatch()->getTerminator()->setMetadata(JamMD, nullptr);
      auto note = [&](const Twine &msg) {
        if (opts.remarks)
          errs() << "loops: " << F.getName() << ": jam: " << msg << "\n";
      };
      changed |= simplifyLoop(L, &DT, &LI, &SE, &AC, nullptr, false);
      if (L->getSubLoops().size() != 1) {
        note("not one inner loop");
        continue;
      }
      changed |= formLCSSARecursively(*L, DT, &LI, &SE);
      DependenceInfo DI(&F, &AA, &SE, &LI);
      if (!isSafeToUnrollAndJam(L, SE, DT, DI, LI)) {
        note("not safe per isSafeToUnrollAndJam");
        continue;
      }
      Loop *epilogue = nullptr;
      LoopUnrollResult r = UnrollAndJamLoop(L, JamCount, /*TripCount=*/0, /*TripMultiple=*/1,
                                            /*UnrollRemainder=*/false, &LI, &SE, &DT, &AC,
                                            &TTI, &ORE, &epilogue);
      if (r == LoopUnrollResult::Unmodified) {
        note("UnrollAndJamLoop did nothing");
        continue;
      }
      note("jammed by " + Twine(JamCount));
      changed = true;
    }
    if (!changed)
      return PreservedAnalyses::all();
    return PreservedAnalyses::none();
  }

private:
  Options opts;
};

} // namespace

void addJam(FunctionPassManager &fpm, const Options &opts) { fpm.addPass(JamPass(opts)); }

} // namespace loops
} // namespace marmot
