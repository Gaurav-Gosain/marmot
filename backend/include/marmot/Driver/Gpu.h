// Gpu.h: GPU offload of parallel loop bodies (docs/gpu.md).
#ifndef MARMOT_DRIVER_GPU_H
#define MARMOT_DRIVER_GPU_H

#include <string>

namespace llvm {
class Module;
}

namespace marmot {

struct CompileOptions;

// Compiles every "marmot-gpu-body" function of m to PTX (when gpuArch is
// set), embeds it, and rewrites each marmot_gpu_for call to rt_gpu_for with
// the rt_parallel_for CPU path as fallback (or to rt_parallel_for alone when
// gpuArch is empty). Returns false with err on failure.
bool lowerGpuLoops(llvm::Module &m, const std::string &gpuArch, std::string &err);

} // namespace marmot

#endif
