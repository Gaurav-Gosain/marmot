if(MARMOT_PLAY_TOOLS)
  add_executable(marmot-backend marmot-backend.cpp play-tools.cpp)
  target_compile_definitions(marmot-backend PRIVATE MARMOT_PLAY_TOOLS=1)
  target_include_directories(marmot-backend PRIVATE ${MARMOT_LLD_INCLUDE_DIRS})
  target_link_libraries(marmot-backend PRIVATE MarmotDriver ${MARMOT_LLVM_LIBS}
    ${MARMOT_LLD_LIB_DIR}/liblldWasm.a ${MARMOT_LLD_LIB_DIR}/liblldCommon.a
    LLVMLTO LLVMOption)
else()
  add_executable(marmot-backend marmot-backend.cpp)
  target_link_libraries(marmot-backend PRIVATE MarmotDriver ${MARMOT_LLVM_LIBS})
endif()
# The object cache keys on the build id (WholeProgram.cpp backendId).
if(CMAKE_SYSTEM_NAME STREQUAL "Linux")
  target_link_options(marmot-backend PRIVATE -Wl,--build-id=sha1)
endif()
