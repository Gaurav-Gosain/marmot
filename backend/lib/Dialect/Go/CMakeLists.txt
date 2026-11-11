add_library(MarmotGoDialect STATIC
  GoDialect.cpp
  GoToLLVM.cpp
)
add_dependencies(MarmotGoDialect MarmotGoOpsIncGen MarmotGoPassesIncGen)
target_link_libraries(MarmotGoDialect PUBLIC ${MARMOT_LLVM_LIBS})
