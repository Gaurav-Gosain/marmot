// tier0-run: compiles a lowered module (the output of marmot-backend
// --emit=mlir) with tier0 into an object, and reports time, counts and the
// reasons functions fell back to LLVM.
//
//   tier0-run [-x64] in.mlir out.o [rest.mlir [frag]]
//
// -x64 writes an x86-64 ELF object instead of an arm64 Mach-O one.
//
// rest.mlir receives what is left for LLVM (the fallback functions, the rest
// declared), to compile with marmot-backend and link next to out.o. frag
// receives the pc table fragment of the tier0 functions.
#include "../Tier0.h"

#include "mlir/Dialect/DLTI/DLTI.h"
#include "mlir/Dialect/LLVMIR/LLVMDialect.h"
#include "mlir/IR/BuiltinOps.h"
#include "mlir/IR/MLIRContext.h"
#include "mlir/Parser/Parser.h"
#include "llvm/Support/SourceMgr.h"
#include "llvm/Support/raw_ostream.h"

#include <chrono>
#include <functional>
#include <map>

int main(int argc, char **argv) {
  marmot::tier0::Target target = marmot::tier0::Target::Arm64MachO;
  if (argc > 1 && std::string(argv[1]) == "-x64") {
    target = marmot::tier0::Target::X64Elf;
    argv++;
    argc--;
  }
  if (argc < 3 || argc > 5) {
    llvm::errs() << "usage: tier0-run [-x64] in.mlir out.o [rest.mlir [frag]]\n";
    return 2;
  }
  mlir::MLIRContext ctx;
  ctx.allowUnregisteredDialects();
  ctx.loadDialect<mlir::LLVM::LLVMDialect, mlir::DLTIDialect>();
  auto t0 = std::chrono::steady_clock::now();
  mlir::OwningOpRef<mlir::ModuleOp> m = mlir::parseSourceFile<mlir::ModuleOp>(argv[1], &ctx);
  if (!m) return 1;
  auto t1 = std::chrono::steady_clock::now();
  std::string frag, err;
  std::vector<std::string> fallback, reasons;
  marmot::tier0::Stats st;
  std::string unit = std::to_string(std::hash<std::string>{}(argv[2]) % 1000000007);
  if (!marmot::tier0::emit(*m, unit, argv[2], frag, fallback, reasons, st, err, target)) {
    llvm::errs() << "tier0: " << err << "\n";
    return 1;
  }
  auto t2 = std::chrono::steady_clock::now();
  auto ms = [](auto a, auto b) { return std::chrono::duration<double, std::milli>(b - a).count(); };
  llvm::outs() << "parse " << ms(t0, t1) << " ms, tier0 " << ms(t1, t2) << " ms, " << st.functions
               << " functions, " << st.fallbacks << " fallbacks, " << st.globals << " globals, "
               << st.codeBytes << " code bytes\n";
  std::map<std::string, unsigned> why;
  for (auto &r : reasons) why[r]++;
  for (auto &[r, n] : why) llvm::outs() << "  fallback " << n << ": " << r << "\n";
  for (int i = 3; i < argc; i++) {
    std::error_code ec;
    llvm::raw_fd_ostream os(argv[i], ec);
    if (ec) {
      llvm::errs() << "cannot write " << argv[i] << "\n";
      return 1;
    }
    if (i == 3)
      m->print(os);
    else
      os << frag;
  }
  return 0;
}
