// MARMOT_PLAY_TOOLS: the browser build's one module. "marmot-backend
// wasm-ld ARGS" runs lld's wasm linker in-process (one copy of LLVM for both
// tools); anything else is the backend.
#include "lld/Common/Driver.h"
#include "llvm/Support/raw_ostream.h"

#include <cstring>
#include <vector>

LLD_HAS_DRIVER(wasm)

int marmot_backend_main(int argc, char **argv);

int main(int argc, char **argv) {
  if (argc > 1 && std::strcmp(argv[1], "wasm-ld") == 0) {
    std::vector<const char *> args(argv + 1, argv + argc);
    lld::Result r = lld::lldMain(args, llvm::outs(), llvm::errs(), {{lld::Wasm, &lld::wasm::link}});
    return r.retCode;
  }
  return marmot_backend_main(argc, argv);
}
