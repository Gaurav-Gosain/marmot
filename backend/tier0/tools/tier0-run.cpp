// tier0-run: compiles a lowered module (the output of marmot-backend
// --emit=mlir) with tier0 into an object, and reports time, counts and the
// reasons functions fell back to LLVM.
//
//   tier0-run in.mlir out.o [rest.mlir]
//
// rest.mlir receives what is left for LLVM (the fallback functions, the rest
// declared), to compile with marmot-backend and link next to out.o.
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
  if (argc != 3 && argc != 4) {
    llvm::errs() << "usage: tier0-run in.mlir out.o [rest.mlir]\n";
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
  if (!marmot::tier0::emit(*m, unit, argv[2], frag, fallback, reasons, st, err)) {
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
  if (argc == 4) {
    std::error_code ec;
    llvm::raw_fd_ostream os(argv[3], ec);
    if (ec) {
      llvm::errs() << "cannot write " << argv[3] << "\n";
      return 1;
    }
    m->print(os);
  }
  return 0;
}
