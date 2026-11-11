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

// Parses -target: an LLVM triple, or GOOS/GOARCH (linux/amd64, linux/arm64,
// darwin/arm64, darwin/amd64). Empty means the host.
llvm::Triple targetTriple(const std::string &target);

// The default CPU for a target: the host CPU when the target is the host
// (same arch and OS), else a portable baseline (x86-64 v1 like GOAMD64=v1,
// generic armv8-a for arm64; apple-m1 for darwin/arm64).
std::string defaultCPU(const llvm::Triple &triple, bool &isHost);

} // namespace marmot

#endif
