# tier0: the direct code generator (see DESIGN.md). Builds on its own
# (cmake -S backend/tier0 -B <dir> -DMLIR_DIR=... -DLLVM_DIR=...) as a library
# and the tier0-run tool, or as a subdirectory of the backend build.
cmake_minimum_required(VERSION 3.20)
project(marmot-tier0 C CXX)

if(NOT TARGET MLIRLLVMDialect)
  find_package(MLIR REQUIRED CONFIG)
  list(APPEND CMAKE_MODULE_PATH "${MLIR_CMAKE_DIR}" "${LLVM_CMAKE_DIR}")
  include_directories(${LLVM_INCLUDE_DIRS} ${MLIR_INCLUDE_DIRS})
  set(CMAKE_CXX_STANDARD 17)
endif()

add_library(MarmotTier0 STATIC MachO.cpp ELF.cpp Codegen.cpp)
# Unsupported constructs unwind out of the code generator with an exception
# (caught inside it, never across LLVM frames).
target_compile_options(MarmotTier0 PRIVATE -fexceptions -fno-rtti)
# The backend includes it as "tier0/Tier0.h".
target_include_directories(MarmotTier0 PUBLIC ${CMAKE_CURRENT_SOURCE_DIR}/..)
if(DEFINED MARMOT_LLVM_LIBS)
  # Inside the backend build: the same LLVM and MLIR libraries as the backend.
  target_link_libraries(MarmotTier0 PUBLIC ${MARMOT_LLVM_LIBS})
else()
  target_link_libraries(MarmotTier0 PUBLIC MLIRLLVMDialect MLIRIR)
  add_executable(tier0-run tools/tier0-run.cpp)
  target_compile_options(tier0-run PRIVATE -fno-rtti)
  target_link_libraries(tier0-run PRIVATE MarmotTier0 MLIRParser MLIRDLTIDialect MLIRLLVMDialect)
endif()
