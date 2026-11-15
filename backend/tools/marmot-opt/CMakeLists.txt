add_executable(marmot-opt marmot-opt.cpp)
target_link_libraries(marmot-opt PRIVATE MarmotGoDialect ${MARMOT_LLVM_LIBS})
# MlirOptMain: in the shared libMLIR, a component library of its own when static.
if(MARMOT_STATIC)
  target_link_libraries(marmot-opt PRIVATE MLIROptLib)
endif()
