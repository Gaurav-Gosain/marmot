// Targets.cpp: lazy LLVM target initialization.
//
// ARCH FILE: this is the one place that names concrete architectures. Add a
// new architecture by adding a case below and its libraries to the static
// link list in CMakeLists.txt.
#include "marmot/Driver/Targets.h"

#include "llvm/TargetParser/Host.h"
#include <cstdlib>
#include <mutex>
#if defined(__APPLE__)
#include <sys/sysctl.h>
#endif

extern "C" {
void LLVMInitializeAArch64TargetInfo();
void LLVMInitializeAArch64Target();
void LLVMInitializeAArch64TargetMC();
void LLVMInitializeAArch64AsmPrinter();
void LLVMInitializeAArch64AsmParser();
void LLVMInitializeX86TargetInfo();
void LLVMInitializeX86Target();
void LLVMInitializeX86TargetMC();
void LLVMInitializeX86AsmPrinter();
void LLVMInitializeX86AsmParser();
}

namespace marmot {

bool initTargetFor(const llvm::Triple &triple, std::string &err) {
  static std::once_flag aarch64Once, x86Once;
  switch (triple.getArch()) {
  case llvm::Triple::aarch64:
    std::call_once(aarch64Once, [] {
      LLVMInitializeAArch64TargetInfo();
      LLVMInitializeAArch64Target();
      LLVMInitializeAArch64TargetMC();
      LLVMInitializeAArch64AsmPrinter();
      LLVMInitializeAArch64AsmParser();
    });
    return true;
  case llvm::Triple::x86_64:
    std::call_once(x86Once, [] {
      LLVMInitializeX86TargetInfo();
      LLVMInitializeX86Target();
      LLVMInitializeX86TargetMC();
      LLVMInitializeX86AsmPrinter();
      LLVMInitializeX86AsmParser();
    });
    return true;
  default:
    err = "unsupported target architecture: " + triple.getArchName().str();
    return false;
  }
}

// On macOS the default triple carries the Darwin kernel version, which maps
// to a macOS version that does not always match the real one. Use
// MACOSX_DEPLOYMENT_TARGET if set, else the running macOS product version,
// the same way clang does.
llvm::Triple hostTriple() {
  llvm::Triple t(llvm::Triple::normalize(llvm::sys::getDefaultTargetTriple()));
#if defined(__APPLE__)
  std::string ver;
  if (const char *env = std::getenv("MACOSX_DEPLOYMENT_TARGET"))
    ver = env;
  if (ver.empty()) {
    char buf[64];
    size_t len = sizeof(buf);
    if (sysctlbyname("kern.osproductversion", buf, &len, nullptr, 0) == 0)
      ver.assign(buf, strnlen(buf, len));
  }
  if (!ver.empty())
    t = llvm::Triple(t.getArchName().str() + "-apple-macosx" + ver);
#endif
  return t;
}

} // namespace marmot
