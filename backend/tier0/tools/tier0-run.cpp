// tier0-run: compiles a lowered module (the output of marmot-backend
// --emit=mlir) with tier0 into an object, and reports time, counts and the
// reasons functions fell back to LLVM.
//
//   tier0-run [-x64] in.mlir out.o [rest.mlir [frag]]
//
// -x64 writes an x86-64 ELF object instead of an arm64 Mach-O one.
// TIER0_THREADS=N compiles the functions on N threads.
//
//   tier0-run -read in.mlir...
//
// reads each file with the MLIR parser and with the tier0 reader and checks
// that both build the same module (generic form with locations); prints the
// times, or the first difference.
//
// rest.mlir receives what is left for LLVM (the fallback functions, the rest
// declared), to compile with marmot-backend and link next to out.o. frag
// receives the pc table fragment of the tier0 functions.
#include "../Reader.h"
#include "../Tier0.h"

#include "mlir/Dialect/DLTI/DLTI.h"
#include "mlir/Dialect/LLVMIR/LLVMDialect.h"
#include "mlir/IR/BuiltinOps.h"
#include "mlir/IR/MLIRContext.h"
#include "mlir/IR/OperationSupport.h"
#include "llvm/Support/MemoryBuffer.h"
#include "mlir/Parser/Parser.h"
#include "llvm/Support/SourceMgr.h"
#include "llvm/Support/raw_ostream.h"

#include <chrono>
#include <functional>
#include <map>

static std::string printed(mlir::ModuleOp m) {
  std::string s;
  llvm::raw_string_ostream os(s);
  m->print(os, mlir::OpPrintingFlags().printGenericOpForm().enableDebugInfo().assumeVerified());
  return s;
}

static int readCheck(int argc, char **argv) {
  int bad = 0;
  double tp = 0, tr = 0;
  auto ms = [](auto a, auto b) { return std::chrono::duration<double, std::milli>(b - a).count(); };
  for (int i = 2; i < argc; i++) {
    auto buf = llvm::MemoryBuffer::getFile(argv[i]);
    if (!buf) {
      llvm::errs() << argv[i] << ": cannot read\n";
      return 2;
    }
    mlir::MLIRContext c1, c2;
    c1.allowUnregisteredDialects();
    c2.allowUnregisteredDialects();
    c1.loadDialect<mlir::LLVM::LLVMDialect>();
    c2.loadDialect<mlir::LLVM::LLVMDialect>();
    llvm::SourceMgr sm;
    sm.AddNewSourceBuffer(llvm::MemoryBuffer::getMemBuffer((*buf)->getBuffer(), argv[i], false), llvm::SMLoc());
    auto t0 = std::chrono::steady_clock::now();
    auto a = mlir::parseSourceFile<mlir::ModuleOp>(sm, mlir::ParserConfig(&c1, false));
    auto t1 = std::chrono::steady_clock::now();
    std::string why;
    unsigned rt = getenv("READER_THREADS") ? unsigned(atoi(getenv("READER_THREADS"))) : 1;
    auto r = marmot::tier0::readModule(&c2, (*buf)->getBuffer(), argv[i], why, rt);
    auto t2 = std::chrono::steady_clock::now();
    tp += ms(t0, t1);
    tr += ms(t1, t2);
    if (!a) {
      llvm::outs() << argv[i] << ": parser failed\n";
      continue;
    }
    if (!r) {
      llvm::outs() << argv[i] << ": reader gave up: " << why << "\n";
      continue;
    }
    std::string pa = printed(*a), pr = printed(*r);
    if (pa != pr) {
      size_t k = 0;
      while (k < pa.size() && k < pr.size() && pa[k] == pr[k]) k++;
      size_t ls = pa.rfind('\n', k);
      ls = ls == std::string::npos ? 0 : ls + 1;
      llvm::outs() << argv[i] << ": DIFFERENT\n  parser: " << pa.substr(ls, pa.find('\n', k) - ls).substr(0, 400)
                   << "\n  reader: " << pr.substr(ls, pr.find('\n', k) - ls).substr(0, 400) << "\n";
      bad++;
      continue;
    }
  }
  llvm::outs() << "parse " << tp << " ms, read " << tr << " ms, " << bad << " different\n";
  return bad ? 1 : 0;
}

int main(int argc, char **argv) {
  if (argc > 2 && std::string(argv[1]) == "-read") return readCheck(argc, argv);
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
  unsigned threads = getenv("TIER0_THREADS") ? unsigned(atoi(getenv("TIER0_THREADS"))) : 1;
  if (!marmot::tier0::emit(*m, unit, argv[2], frag, fallback, reasons, st, err, target, threads)) {
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
