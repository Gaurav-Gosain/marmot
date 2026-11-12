// Internal.h: pass constructors shared inside MarmotLoops.
#ifndef MARMOT_LOOPS_INTERNAL_H
#define MARMOT_LOOPS_INTERNAL_H

#include "marmot/Loops/Loops.h"
#include "llvm/ADT/SmallVector.h"
#include <string>
#include "llvm/IR/PassManager.h"

namespace llvm {
class AAResults;
class DominatorTree;
class Function;
class Loop;
class LoopInfo;
class SCEV;
class SCEVPredicate;
class ScalarEvolution;
} // namespace llvm

namespace marmot {
namespace loops {

// One recurrence of an inner loop inside an affine expression: step * j for
// j in [0, count].
struct Term {
  const llvm::SCEV *step;
  const llvm::SCEV *count;
};

// Splits the integer expression S into coef*k + [lo, hi] for iteration k of
// L (Affine.cpp). Recurrences of loops inside L contribute their whole range
// (their trip count bound must be invariant in L); with terms set, each such
// recurrence is also listed. Returns false when S is not of that form.
// With latchOnly, inner trip counts are their latch exit counts (possibly
// under predicates, appended to preds) instead of the bound over all exits.
bool decomposeAffine(llvm::ScalarEvolution &SE, const llvm::SCEV *S, const llvm::Loop *L,
                     const llvm::SCEV *&coef, const llvm::SCEV *&lo, const llvm::SCEV *&hi,
                     llvm::SmallVectorImpl<Term> *terms, bool latchOnly = false,
                     llvm::SmallVectorImpl<const llvm::SCEVPredicate *> *preds = nullptr);

// Bounds check versioning (Versioning.cpp, Rule V). Returns true after
// versioning L; why gets a summary or the reason it did not.
bool versionBoundsChecks(llvm::Function &F, llvm::Loop *L, llvm::LoopInfo &LI,
                         llvm::ScalarEvolution &SE, llvm::DominatorTree &DT,
                         llvm::AAResults &AA, std::string &why);

// Loop idioms replaced by runtime kernels (Idioms.cpp, Rule D).
void addIdioms(llvm::ModulePassManager &mpm, const Options &opts);

// loop-parallelize (Parallelize.cpp).
void addParallelize(llvm::ModulePassManager &mpm, const Options &opts);
// reduction interchange (Interchange.cpp, Rule I).
void addInterchange(llvm::FunctionPassManager &fpm, const Options &opts);
// masked jam (MaskedJam.cpp, Rule M).
void addMaskedJam(llvm::FunctionPassManager &fpm, const Options &opts);
// tiling of chunk loops (Tile.cpp, Rule T2).
void addTile(llvm::FunctionPassManager &fpm, const Options &opts);
// Rules that -par uses only when their switch is on (Rules.cpp): the ones
// verify has not signed off yet.
enum class Rule { RtChecks, Privatize, BcvGrowth, BcvLoaded, ByteCount, Scan, Interchange, Jam, MaskedJam };
bool ruleOn(Rule r);
const char *ruleFlag(Rule r);

// Rule X: x86-64 versions of chunk functions, picked at run time (-loops-mv).
bool multiversion(llvm::Module &M, const Options &opts);
// loop-jam (Jam.cpp): unroll-and-jam of chunk loops by JamCount.
constexpr unsigned JamCount = 4;
void addJam(llvm::FunctionPassManager &fpm, const Options &opts);

} // namespace loops
} // namespace marmot

#endif
