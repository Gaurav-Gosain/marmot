cmake_minimum_required(VERSION 3.20)
project(marmot-backend LANGUAGES C CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

# MARMOT_STATIC with the official LLVM release: its LLVMSupport links
# zstd::libzstd_static, and a host with only the shared libzstd has no such
# target (the configure failed). Then the target is the shared library.
if(MARMOT_STATIC AND NOT TARGET zstd::libzstd_static)
  find_library(MARMOT_ZSTD_STATIC NAMES libzstd.a)
  find_library(MARMOT_ZSTD_SHARED NAMES zstd)
  if(NOT MARMOT_ZSTD_STATIC AND MARMOT_ZSTD_SHARED)
    add_library(zstd::libzstd_static UNKNOWN IMPORTED)
    set_target_properties(zstd::libzstd_static PROPERTIES IMPORTED_LOCATION "${MARMOT_ZSTD_SHARED}")
  endif()
endif()

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

# The wasm32 target (-target wasip1/wasm) only when this LLVM has it.
if("WebAssembly" IN_LIST LLVM_TARGETS_TO_BUILD)
  add_compile_definitions(MARMOT_HAVE_WASM=1)
  set(MARMOT_WASM_LIBS LLVMWebAssemblyCodeGen LLVMWebAssemblyAsmParser LLVMWebAssemblyDesc LLVMWebAssemblyInfo)
else()
  message(STATUS "LLVM has no WebAssembly target: -target wasip1/wasm is not supported by this build")
  set(MARMOT_WASM_LIBS)
endif()

# The native targets only when this LLVM has them (a WebAssembly-only LLVM
# builds the browser backend).
if("AArch64" IN_LIST LLVM_TARGETS_TO_BUILD)
  add_compile_definitions(MARMOT_HAVE_AARCH64=1)
  set(MARMOT_AARCH64_LIBS LLVMAArch64CodeGen LLVMAArch64AsmParser LLVMAArch64Desc LLVMAArch64Info)
else()
  message(STATUS "LLVM has no AArch64 target: arm64 targets are not supported by this build")
  set(MARMOT_AARCH64_LIBS)
endif()
if("X86" IN_LIST LLVM_TARGETS_TO_BUILD)
  add_compile_definitions(MARMOT_HAVE_X86=1)
  set(MARMOT_X86_LIBS LLVMX86CodeGen LLVMX86AsmParser LLVMX86Desc LLVMX86Info)
else()
  message(STATUS "LLVM has no X86 target: amd64 targets are not supported by this build")
  set(MARMOT_X86_LIBS)
endif()

# GPU offload (-gpu, PTX made in-process) only when this LLVM has NVPTX.
if("NVPTX" IN_LIST LLVM_TARGETS_TO_BUILD)
  add_compile_definitions(MARMOT_HAVE_NVPTX=1)
  set(MARMOT_NVPTX_LIBS LLVMNVPTXCodeGen LLVMNVPTXDesc LLVMNVPTXInfo)
else()
  message(STATUS "LLVM has no NVPTX target: -gpu is not supported by this build")
  set(MARMOT_NVPTX_LIBS)
endif()

# MARMOT_STATIC=ON links the static component libraries instead of the shared
# libLLVM/libMLIR. Slower to link, but the binary has no dylib dependencies.
option(MARMOT_STATIC "Link LLVM and MLIR statically" OFF)
if(MARMOT_STATIC)
  set(MARMOT_LLVM_LIBS
    MLIRLLVMDialect MLIRLLVMToLLVMIRTranslation MLIRBuiltinToLLVMIRTranslation
    MLIRTargetLLVMIRExport MLIRParser MLIRBytecodeReader MLIRBytecodeWriter MLIRPass
    MLIRTransforms MLIRIR MLIRSupport
    ${MARMOT_AARCH64_LIBS}
    ${MARMOT_X86_LIBS}
    ${MARMOT_WASM_LIBS}
    ${MARMOT_NVPTX_LIBS}
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
# MARMOT_PLAY_TOOLS=ON (Emscripten only): marmot-backend is one wasm module
# that also runs wasm-ld (argv[1] "wasm-ld"), sharing one copy of LLVM; the
# other tools are not built. MARMOT_LLD_INCLUDE_DIRS: lld's include
# directory and the LLVM build's tools/lld/include; MARMOT_LLD_LIB_DIR: the
# directory with liblldWasm.a and liblldCommon.a.
option(MARMOT_PLAY_TOOLS "Emscripten: marmot-backend with wasm-ld in one module" OFF)
if(MARMOT_PLAY_TOOLS)
  if(NOT EMSCRIPTEN)
    message(FATAL_ERROR "MARMOT_PLAY_TOOLS needs an Emscripten build")
  endif()
  set(MARMOT_LLD_INCLUDE_DIRS "" CACHE STRING "lld include directories (source and build)")
  set(MARMOT_LLD_LIB_DIR "${LLVM_LIBRARY_DIR}" CACHE PATH "directory with liblldWasm.a and liblldCommon.a")
endif()

add_subdirectory(tools/marmot-backend)
if(NOT MARMOT_PLAY_TOOLS)
  add_subdirectory(tools/marmot-opt)
  add_subdirectory(tools/marmot-pcln)
endif()
