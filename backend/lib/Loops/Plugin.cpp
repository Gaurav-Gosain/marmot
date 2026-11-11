// Plugin.cpp: MarmotLoops as an LLVM pass plugin, for testing with
// `opt -load-pass-plugin=libMarmotLoopsPlugin.dylib -O2` without the backend.
// MARMOT_LOOPS_REMARKS=1 turns on remarks. The pass is also available as
// -passes=marmot-parallelize.
#include "marmot/Loops/Loops.h"
#include "Internal.h"

#include "llvm/Passes/PassBuilder.h"
#include "llvm/Plugins/PassPlugin.h"

#include <cstdlib>

using namespace llvm;

static marmot::loops::Options pluginOptions() {
  marmot::loops::Options o;
  o.remarks = std::getenv("MARMOT_LOOPS_REMARKS") != nullptr;
  o.parallel = std::getenv("MARMOT_LOOPS_NOPAR") == nullptr;
  o.gpu = std::getenv("MARMOT_LOOPS_GPU") != nullptr;
  return o;
}

extern "C" LLVM_ATTRIBUTE_WEAK PassPluginLibraryInfo llvmGetPassPluginInfo() {
  return {LLVM_PLUGIN_API_VERSION, "MarmotLoops", "0.1", [](PassBuilder &pb) {
            marmot::loops::registerLLVMPasses(pb, pluginOptions());
            pb.registerPipelineParsingCallback(
                [](StringRef name, ModulePassManager &mpm,
                   ArrayRef<PassBuilder::PipelineElement>) {
                  if (name != "marmot-parallelize")
                    return false;
                  marmot::loops::addParallelize(mpm, pluginOptions());
                  return true;
                });
          }};
}
