add_library(MarmotGoDialect STATIC
  GoDialect.cpp
  GoToLLVM.cpp
  Escape.cpp
  AggregateCopies.cpp
  BCE.cpp
  Devirt.cpp
  OpenDefers.cpp
  Strings.cpp
)
add_dependencies(MarmotGoDialect MarmotGoOpsIncGen MarmotGoPassesIncGen)
target_link_libraries(MarmotGoDialect PUBLIC ${MARMOT_LLVM_LIBS})
