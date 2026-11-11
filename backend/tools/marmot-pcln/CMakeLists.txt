add_executable(marmot-pcln marmot-pcln.cpp)
target_link_libraries(marmot-pcln PRIVATE ${MARMOT_LLVM_LIBS})
