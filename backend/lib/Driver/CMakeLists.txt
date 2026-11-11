add_library(MarmotPcln STATIC Pcln.cpp)
target_link_libraries(MarmotPcln PUBLIC ${MARMOT_LLVM_LIBS})

add_library(MarmotDriver STATIC
  Pipeline.cpp
  RuntimeBitcode.cpp
  Targets.cpp
  Watchdog.cpp
  WholeProgram.cpp
  Gpu.cpp
)
target_link_libraries(MarmotDriver PUBLIC MarmotGoDialect MarmotLoops MarmotPcln ${MARMOT_LLVM_LIBS})
