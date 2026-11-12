# MarmotLoops: the loop optimizer (owner: loops). Builds inside the backend
# project, or standalone (for the opt plugin):
#   cmake -S backend/lib/Loops -B .work/loops/build -G Ninja \
#     -DLLVM_DIR=/opt/homebrew/opt/llvm@22/lib/cmake/llvm
if(CMAKE_SOURCE_DIR STREQUAL CMAKE_CURRENT_SOURCE_DIR)
  cmake_minimum_required(VERSION 3.20)
  project(marmot-loops LANGUAGES C CXX)
  set(CMAKE_CXX_STANDARD 17)
  set(CMAKE_CXX_STANDARD_REQUIRED ON)
  find_package(LLVM REQUIRED CONFIG)
  include_directories(${LLVM_INCLUDE_DIRS})
  add_definitions(${LLVM_DEFINITIONS})
  if(NOT LLVM_ENABLE_RTTI)
    add_compile_options(-fno-rtti)
  endif()
  set(MARMOT_LOOPS_STANDALONE ON)
endif()

add_library(MarmotLoops STATIC
  Affine.cpp
  Versioning.cpp
  Idioms.cpp
  Interchange.cpp
  Jam.cpp
  MaskedJam.cpp
  Multiversion.cpp
  Parallelize.cpp
  Register.cpp
  Rules.cpp
  Tile.cpp
)
target_include_directories(MarmotLoops PUBLIC ${CMAKE_CURRENT_SOURCE_DIR}/include)
if(NOT MARMOT_LOOPS_STANDALONE)
  # MLIR passes: only inside the backend build.
  target_sources(MarmotLoops PRIVATE TypeAlias.cpp)
endif()
set_target_properties(MarmotLoops PROPERTIES POSITION_INDEPENDENT_CODE ON)

if(MARMOT_LOOPS_STANDALONE)
  # Pass plugin for opt. Symbols resolve against the opt binary's libLLVM.
  add_library(MarmotLoopsPlugin MODULE Plugin.cpp)
  target_link_libraries(MarmotLoopsPlugin PRIVATE MarmotLoops)
  if(APPLE)
    set_target_properties(MarmotLoopsPlugin PROPERTIES LINK_FLAGS "-undefined dynamic_lookup")
  endif()
endif()
