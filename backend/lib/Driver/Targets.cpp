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

llvm::Triple targetTriple(const std::string &target) {
  if (target.empty())
    return hostTriple();
  if (target.find('/') != std::string::npos) {
    std::string os = target.substr(0, target.find('/'));
    std::string arch = target.substr(target.find('/') + 1);
    std::string llarch = arch == "amd64" ? "x86_64" : arch == "arm64" ? "aarch64" : arch;
    if (os == "darwin") {
      llvm::Triple h = hostTriple();
      std::string ver = h.isMacOSX() ? h.getOSName().str() : "macosx11.0";
      return llvm::Triple((arch == "arm64" ? "arm64" : llarch) + "-apple-" + ver);
    }
    if (os == "linux")
      return llvm::Triple(llarch + "-unknown-linux-gnu");
    return llvm::Triple(llarch + "-unknown-" + os);
  }
  return llvm::Triple(llvm::Triple::normalize(target));
}

std::string defaultCPU(const llvm::Triple &triple, bool &isHost) {
  llvm::Triple host = hostTriple();
  isHost = triple.getArch() == host.getArch() && triple.getOS() == host.getOS();
  // x86-64 binaries are portable: baseline x86-64 (GOAMD64=v1, as gc's
  // default) even on the host; the driver passes -mcpu=x86-64-vN for
  // GOAMD64=vN. Hot loops get v3 clones chosen at run time (loops);
  // -mcpu=native tunes for the host.
  if (triple.getArch() == llvm::Triple::x86_64) {
    isHost = false;
    return "x86-64";
  }
  if (isHost)
    return llvm::sys::getHostCPUName().str();
  switch (triple.getArch()) {
  case llvm::Triple::aarch64:
    return triple.isOSDarwin() ? "apple-m1" : "generic";
  default:
    return "";
  }
}

} // namespace marmot
