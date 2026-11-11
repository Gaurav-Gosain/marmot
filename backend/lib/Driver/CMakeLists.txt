add_library(MarmotDriver STATIC
  Pipeline.cpp
  RuntimeBitcode.cpp
  Targets.cpp
  Watchdog.cpp
)
target_link_libraries(MarmotDriver PUBLIC MarmotGoDialect ${MARMOT_LLVM_LIBS})
