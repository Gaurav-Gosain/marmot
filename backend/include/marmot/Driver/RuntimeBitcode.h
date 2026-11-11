// RuntimeBitcode.h: preparing and linking runtime core bitcode.
#ifndef MARMOT_DRIVER_RUNTIMEBITCODE_H
#define MARMOT_DRIVER_RUNTIMEBITCODE_H

#include "llvm/ADT/StringSet.h"
#include <memory>
#include <mutex>
#include <string>

namespace llvm {
class MemoryBuffer;
class Module;
} // namespace llvm

namespace marmot {

// The runtime bitcode rewritten for import, plus the names of the functions
// whose bodies may be imported.
struct RuntimeLib {
  std::unique_ptr<llvm::MemoryBuffer> bitcode;
  llvm::StringSet<> importable;
  // Symbols the runtime core uses but does not define (Go code it calls,
  // such as marmot_main): roots of the whole-program mode.
  llvm::StringSet<> referenced;
};

// The runtime bitcode, prepared on first use (a batch whose objects all come
// from the cache never parses it). Thread safe.
class LazyRuntimeLib {
public:
  explicit LazyRuntimeLib(std::string path) : path(std::move(path)) {}
  // Null with err set if preparing failed.
  const RuntimeLib *get(std::string &err) const;
  // Hash of the file's bytes, for cache keys (cheap: no parsing).
  const std::string &fileHash() const;

private:
  std::string path;
  mutable std::once_flag once, hashOnce;
  mutable RuntimeLib lib;
  mutable std::string error, hash;
  mutable bool ok = false;
};

// Loads rtcore.bc and rewrites it once into an import library:
//  - external function definitions that are safe to copy are kept; after
//    linking into a package module they become available_externally, so the
//    optimizer can inline them while the real symbol still comes from
//    librtcore.a at link time (the IR linker does not import
//    available_externally bodies, so the linkage change happens after);
//  - all other functions become declarations;
//  - all global variable definitions become external declarations, so no
//    runtime state is ever duplicated into a package object.
// A function is safe to copy when neither it nor the internal functions it
// reaches reference an internal global variable.
// Returns false with err set on failure.
bool prepareRuntimeBitcode(const std::string &path, RuntimeLib &lib,
                           std::string &err);

// Links the needed parts of the prepared runtime bitcode into m.
bool linkRuntimeBitcode(llvm::Module &m, const RuntimeLib &rt, std::string &err);

} // namespace marmot

#endif
