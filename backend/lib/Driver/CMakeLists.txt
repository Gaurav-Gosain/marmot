add_library(MarmotDriver STATIC
  Pipeline.cpp
  RuntimeBitcode.cpp
  Targets.cpp
)
target_link_libraries(MarmotDriver PUBLIC ${MARMOT_LLVM_LIBS})
