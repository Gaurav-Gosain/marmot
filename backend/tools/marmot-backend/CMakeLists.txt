add_executable(marmot-backend marmot-backend.cpp)
target_link_libraries(marmot-backend PRIVATE MarmotDriver ${MARMOT_LLVM_LIBS})
