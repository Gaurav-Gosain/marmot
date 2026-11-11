// Targets.h: lazy, per-architecture LLVM target initialization.
#ifndef MARMOT_DRIVER_TARGETS_H
#define MARMOT_DRIVER_TARGETS_H

#include "llvm/TargetParser/Triple.h"
#include <string>

namespace marmot {

// Initializes the LLVM target for triple's architecture once. Only the
// architectures listed in Targets.cpp are supported. Returns false and sets
// err for others.
bool initTargetFor(const llvm::Triple &triple, std::string &err);

// The host triple in the form marmot uses.
llvm::Triple hostTriple();

} // namespace marmot

#endif
