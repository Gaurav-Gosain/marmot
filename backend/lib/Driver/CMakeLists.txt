add_library(MarmotPcln STATIC Pcln.cpp)
# No LLVM libraries of its own: marmot-pcln links it with static ones, the
# driver with the shared ones (two copies of LLVMSupport in one binary split
# the target registry).

add_library(MarmotDriver STATIC
  Pipeline.cpp
  RuntimeBitcode.cpp
  Targets.cpp
  Watchdog.cpp
  WholeProgram.cpp
  Gpu.cpp
)
target_link_libraries(MarmotDriver PUBLIC MarmotGoDialect MarmotLoops MarmotPcln MarmotTier0 ${MARMOT_LLVM_LIBS})
