// Pipeline.h: one MLIR module in, one output file out.
#ifndef MARMOT_DRIVER_PIPELINE_H
#define MARMOT_DRIVER_PIPELINE_H

#include "marmot/Loops/Loops.h"

#include <memory>
#include <string>

namespace llvm {
class LLVMContext;
class Module;
class TargetMachine;
class raw_ostream;
} // namespace llvm

namespace marmot {

struct RuntimeLib;
class LazyRuntimeLib;

enum class Emit { Obj, Asm, LLVM, BC, MLIR };

// Optimization level as given by -O: '0'..'3', 's', 'z'.
struct CompileOptions {
  char optLevel = '2';
  Emit emit = Emit::Obj;
  std::string triple; // empty means host
  std::string cpu;    // empty means host CPU for host triple, generic otherwise
  std::string features;
  bool verifyEach = false; // verify after every MLIR pass
  bool timePhases = false;
  bool singleThreaded = false; // disable MLIR context threading (batch mode)
  unsigned parseThreads = 1;   // threads to parse a large module with (parseFile, tier0)
  bool initOptNone = true;     // compile package init functions with optnone
  bool wpoImport = true;       // -wpo: import small functions across modules
  // Dev batch: split a module above this many instructions into groups
  // compiled in parallel (0: never).
  unsigned splitInstrs = 0;
  // Batch: write a pc table fragment OUT.pcln next to each object.
  bool pclnFragments = false;
  bool mergeFunctions = false; // MergeFunctions at -Os/-Oz (off: recover frames)
  bool stringsNoCopy = false; // go-strings rule S3 (off until the matrix passes with it)
  bool wpoPruneTypes = true;   // -wpo: drop reflection tables nobody can read
  bool wpoClosedWorld = true;  // -wpo: devirtualized indirect calls lose their fallback
  loops::Options loops;        // loop optimizer (owner: loops), -O2 and -O3
  std::string pgoUse;          // -pgo=FILE: indexed profile (llvm-profdata merge output)
  std::string pgoGen;          // -pgo-gen[=pattern]: instrument; raw profile file pattern
  // Debug info from the MLIR op locations: 0 none, 1 line tables (default),
  // 2 full. Nothing is emitted for a module without file locations.
  int debugLevel = 1;
  // With debugLevel > 0, Go code keeps sibling calls only when this is set.
  bool keepTailCalls = false;
  // No tail merging in code generation, so every call site keeps its own
  // inlined frames (runtime.Caller through inlined calls).
  bool keepCallSites = false;
  // The code-changing options of the run as given (marmot-backend), part of
  // every cache key.
  std::string optionKey;
  bool tier0 = false;          // batch: tier0 objects, LLVM -O0 for what it leaves
  // tier0 batch: spool the modules it compiles here for background tier 1
  // (runTier1) and use the -O2 objects tier 1 cached.
  std::string tier1Spool;
  bool prepareOnly = false;    // batch: fill the cache (phase 1 for -wpo), write no outputs
  std::string gpuArch;         // -gpu=sm_86: PTX for "marmot-gpu-body" loops
  bool devirt = true;          // go-devirt at -O1 and up
  bool strings = true;         // go-strings at -O1 and up
  bool escape = true;          // go-escape at -O1 and up
  bool boolPack = true;        // go-bool-pack at -O1 and up
  bool bce = true;             // go-bce at -O1 and up
  bool openDefers = true;      // go-open-defers at -O1 and up
  // Prepared runtime bitcode (see RuntimeBitcode.h), shared by all jobs.
  const LazyRuntimeLib *rtBitcode = nullptr;
};

struct PhaseTimes {
  double parse = 0, mlirPasses = 0, translate = 0, link = 0, linkRt = 0, opt = 0,
         codegen = 0, write = 0;
  double total() const {
    return parse + mlirPasses + translate + link + linkRt + opt + codegen + write;
  }
  void add(const PhaseTimes &o);
  void print(llvm::raw_ostream &os, const std::string &label) const;
};

// Compiles input (MLIR text or bytecode) to output. Errors go to errs with
// the input name. Returns true on success.
bool compileFile(const std::string &input, const std::string &output,
                 const CompileOptions &opts, PhaseTimes &times,
                 llvm::raw_ostream &errs);

// A parsed MLIR module with its own context.
struct ParsedModule;
struct ParsedModuleDeleter {
  void operator()(ParsedModule *p) const;
};
using ParsedModulePtr = std::unique_ptr<ParsedModule, ParsedModuleDeleter>;
// Parse (and verify) input. Null with a message on errs on failure.
ParsedModulePtr parseFile(const std::string &input, const CompileOptions &opts,
                          llvm::raw_ostream &errs);
// Verify a module parseFile left unverified (tier0 batches); a no-op when
// it was verified. False with the diagnostics on errs.
bool verifyParsed(ParsedModule &pm, const std::string &input, llvm::raw_ostream &errs);
// The module operation, as an opaque pointer for passes (mlir::ModuleOp).
void *parsedModuleOp(ParsedModule &pm);
// The go passes, lowering and translation of a parsed module.
std::unique_ptr<llvm::Module> lowerAndTranslate(ParsedModule &pm, const std::string &input,
                                                const CompileOptions &opts,
                                                llvm::TargetMachine &tm,
                                                llvm::LLVMContext &lctx, PhaseTimes &times,
                                                llvm::raw_ostream &errs,
                                                const std::string *mlirOutput = nullptr);

// The MLIR half of lowerAndTranslate: go passes and lowering to the llvm
// dialect, then the target's data layout and triple on the module. False with
// a message on errs on failure.
bool lowerModule(ParsedModule &pm, const std::string &input, const CompileOptions &opts,
                 llvm::TargetMachine &tm, PhaseTimes &times, llvm::raw_ostream &errs);
// The LLVM half: translation of a lowered module (frees the MLIR module).
std::unique_ptr<llvm::Module> translateLowered(ParsedModule &pm, const std::string &input,
                                               llvm::TargetMachine &tm, llvm::LLVMContext &lctx,
                                               PhaseTimes &times, llvm::raw_ostream &errs);

// The two halves of compileFile, for the whole-program mode.
std::unique_ptr<llvm::TargetMachine> createTargetMachine(const CompileOptions &opts,
                                                         std::string &err);
// Parse, go passes, lowering, translation. With mlirOutput set, writes the
// lowered MLIR there and returns null.
std::unique_ptr<llvm::Module> translateFile(const std::string &input,
                                            const CompileOptions &opts,
                                            llvm::TargetMachine &tm,
                                            llvm::LLVMContext &lctx, PhaseTimes &times,
                                            llvm::raw_ostream &errs,
                                            const std::string *mlirOutput = nullptr);
// Runtime bitcode import, function attributes, LLVM pipeline, emission.
bool optimizeAndEmit(llvm::Module &lmod, llvm::TargetMachine &tm, const std::string &input,
                     const std::string &output, const CompileOptions &opts,
                     PhaseTimes &times, llvm::raw_ostream &errs);

// Validates the -O value. Returns false for an unknown level.
bool parseOptLevel(const std::string &s, char &level);

} // namespace marmot

#endif
