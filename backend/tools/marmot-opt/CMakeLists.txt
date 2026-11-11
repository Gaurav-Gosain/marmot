add_executable(marmot-opt marmot-opt.cpp)
target_link_libraries(marmot-opt PRIVATE MarmotGoDialect ${MARMOT_LLVM_LIBS})
