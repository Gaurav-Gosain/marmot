cmake_minimum_required(VERSION 3.20)
project(marmot-backend LANGUAGES C CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

find_package(MLIR REQUIRED CONFIG)
message(STATUS "Using MLIRConfig.cmake in: ${MLIR_DIR}")
message(STATUS "Using LLVMConfig.cmake in: ${LLVM_DIR}")

list(APPEND CMAKE_MODULE_PATH "${MLIR_CMAKE_DIR}" "${LLVM_CMAKE_DIR}")
include(TableGen)
include(AddLLVM)
include(AddMLIR)

include_directories(${LLVM_INCLUDE_DIRS} ${MLIR_INCLUDE_DIRS})
include_directories(${PROJECT_SOURCE_DIR}/include ${PROJECT_BINARY_DIR}/include)
add_definitions(${LLVM_DEFINITIONS})
if(NOT LLVM_ENABLE_RTTI)
  add_compile_options(-fno-rtti)
endif()
if(NOT LLVM_ENABLE_EH)
  add_compile_options(-fno-exceptions)
endif()

# MARMOT_STATIC=ON links the static component libraries instead of the shared
# libLLVM/libMLIR. Slower to link, but the binary has no dylib dependencies.
option(MARMOT_STATIC "Link LLVM and MLIR statically" OFF)
if(MARMOT_STATIC)
  set(MARMOT_LLVM_LIBS
    MLIRLLVMDialect MLIRLLVMToLLVMIRTranslation MLIRBuiltinToLLVMIRTranslation
    MLIRTargetLLVMIRExport MLIRParser MLIRBytecodeReader MLIRBytecodeWriter MLIRPass
    MLIRTransforms MLIRIR MLIRSupport
    LLVMAArch64CodeGen LLVMAArch64AsmParser LLVMAArch64Desc LLVMAArch64Info
    LLVMX86CodeGen LLVMX86AsmParser LLVMX86Desc LLVMX86Info
    LLVMPasses LLVMLinker LLVMIRReader LLVMBitReader LLVMBitWriter LLVMipo
    LLVMTarget LLVMCodeGen LLVMMC LLVMSupport)
else()
  set(MARMOT_LLVM_LIBS MLIR LLVM)
endif()
# The pc table builder runs on every link: it links the few LLVM libraries
# it uses statically and loads no LLVM or MLIR dylib.
set(MARMOT_PCLN_LIBS LLVMDebugInfoDWARF LLVMObject LLVMSupport)

add_subdirectory(include/marmot)
add_subdirectory(tier0)
add_subdirectory(lib)
add_subdirectory(tools/marmot-backend)
add_subdirectory(tools/marmot-opt)
add_subdirectory(tools/marmot-pcln)
