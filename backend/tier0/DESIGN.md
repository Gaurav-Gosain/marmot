# tier0: a direct code generator for dev builds

tier0 turns the backend's lowered module (the llvm dialect, after the go passes, go-to-llvm,
aggregate copies and the debug scope pass) straight into a relocatable object, without LLVM IR,
the LLVM optimizer or LLVM instruction selection. It exists for build time: the edit loop in dev
mode must be on par with `go build`, and LLVM at its fastest setting may not get there for cold
builds. Release builds and the background tier 1 keep using LLVM.

## Goals

- Throughput: at least 10 times faster than LLVM -O2 per function, measured from the lowered
  module to object bytes (the baseline to beat is LLVM at -O0 with FastISel or GlobalISel on the
  same input).
- Code: no worse than about 2 times slower than LLVM -O2 on the benchmark programs; correctness
  first, code quality second.
- Drop-in objects: same symbol names, linkage, sections and C calling convention as LLVM's
  objects, so tier0 and LLVM objects link together and with the runtime library, and every
  function can fall back to LLVM on its own.
- arm64 (Mach-O) first, then x86-64 (ELF, Mach-O).

## Interface

```
namespace marmot::tier0 {
// Emits the functions of module into a relocatable object at outObject and their pc table
// fragment (format of Pcln.h) into fragment. Functions it cannot compile are listed in
// fallback; the backend compiles those with LLVM into a second object. Returns false only
// on an internal error (err set).
bool emit(mlir::ModuleOp module, const std::string &outObject, std::string &fragment,
          std::vector<std::string> &fallback, std::string &err);
}
```

Selected with `--tier0` (implies -O 0); the backend's option key covers it, so cached objects
of the two tiers never mix.

## What the input looks like

One `builtin.module` of llvm dialect operations, close to LLVM IR:

- `llvm.func` with block arguments for phis, `llvm.mlir.global` (constants, zero initialized
  data, initializers built from constant expressions), `llvm.mlir.addressof`.
- Integer and float arithmetic, comparisons, `select`, casts (zext, sext, trunc, bitcast,
  ptrtoint, inttoptr, int/float conversions), `getelementptr` (constant and variable indices
  over structs and arrays), `load`, `store`, `alloca`.
- Control flow: `br`, `cond_br`, `switch`, `return`, `unreachable`.
- Calls: direct, indirect, to runtime functions (`rt_*`) and to other packages.
- Aggregates: first class struct values in arguments, results, `extractvalue`, `insertvalue`.
- Intrinsics: memcpy, memmove, memset, fma, and through `llvm.call_intrinsic` ctpop, ctlz,
  cttz, bswap, bitreverse, fshl, fshr, the saturating float to i64 conversions and
  frameaddress(0).
- i128 as a local value (16-byte slot, low word first): zext, sext, trunc, constants, add,
  sub, mul, and, or, xor and shifts, the shape math/bits produces. i128 in calls, memory
  or comparisons falls back.
- Atomics: `atomicrmw`, `cmpxchg`, `fence`, atomic loads and stores.
- Locations on operations, turned into line tables by the debug scope pass.

Anything else makes the function fall back to LLVM.

## Rules the code must follow (matching the LLVM path)

- A frame record (frame pointer and return address pair) in every function that is not a leaf:
  the runtime walks frames and counts them for recover.
- Never merge or fold functions, never tail call (stack traces and recover depend on frames).
- Calls stay calls: no inlining of runtime functions (the LLVM path's available_externally
  copies of rtcore.bc are an optimization only).
- `marmot_gpu_for` calls are lowered to false (CPU path).
- Package init functions need no special treatment (tier0 does not optimize).
- The garbage collector scans goroutine stacks and the registers saved at a stop
  conservatively, so any register assignment is safe for the GC; there are no stack maps.

## Design

Single pass per function, no IR of its own:

1. Scan the function once: number the values, record the last use of each value in block
   order, collect block arguments and the edges that feed them, and size the frame (allocas,
   spill slots, outgoing argument area).
2. Register assignment, in two steps:
   - first version: every value has a stack slot; within a block, values stay in a small set
     of scratch registers while they are live and not clobbered by a call (a baseline JIT
     approach). Simple, robust, and enough to pass the conformance tiers.
   - second version: linear scan over the block order with the recorded last uses, callee
     saved registers for values live across calls, spill on pressure.
3. Instruction selection by direct emission: one function per operation kind writes machine
   words through small encoders (arm64: fixed 32-bit instructions; x86-64: a minimal encoder for
   the instructions used). Constants fold into immediates where the encoding allows.
4. Block arguments: parallel moves on each edge (with a cycle breaker through a scratch
   register), critical edges split by emitting the moves in a stub.
5. Calls follow the platform C convention as LLVM lowers it, including how LLVM passes first
   class aggregates (each field in its own register, in order, falling back to the stack the way
   LLVM's calling convention does) and multi-register struct returns. On Darwin arm64, pieces
   past x7 / v7 go on the stack packed at their natural size and alignment; a top-level array
   of all i64 / ptr, all f32 or all f64 is a block, all in registers or all on the stack with
   the rest of its register class marked used. A result with more than 8 pieces of either
   class is returned through a pointer in x8. test/abi_gen.py checks this against LLVM with
   random signatures in both call directions.
6. Object writer: Mach-O MH_OBJECT (arm64: `__TEXT,__text`, `__DATA,__data`, `__DATA,__const`,
   zero fill, relocations BRANCH26, PAGE21/PAGEOFF12, GOT_LOAD_* for imports, UNSIGNED for data
   pointers), then ELF (R_AARCH64_*, R_X86_64_*). Symbols with the same names and linkage as
   LLVM's objects. Compact unwind entries are not needed (no exceptions) but are cheap to add if
   the linker or a profiler wants them.
7. Line information: the pc table fragment is written directly (pc offsets per function from
   the operation locations), so no DWARF is produced or parsed in dev mode. DWARF line tables
   can be added later for debuggers.

## Fallback and rollout

Every function either compiles completely in tier0 or is listed for LLVM. Coverage grows by
operation kind; the conformance suite runs with `--tier0` and every failure is either a fixed
bug or a function that falls back. A debug switch forces fallback for named functions so a
miscompile can be bisected to one function.

## Milestones

1. Measurement of LLVM at its fastest on the same inputs (fmt hello, a tests/world module, the
   compiler itself), per phase, and of tier0's throughput on its first subset: the numbers to
   beat.
2. hello and the core conformance tier through tier0 (arm64, Mach-O), the rest falling back.
3. All conformance tiers through tier0 with no fallback on the benchmark programs; code speed
   measured against -O2 and -O0.
4. ELF and x86-64.

## Testing

`sh test/run.sh <tier0-run> <llvm bin dir>` runs the intrinsic, i128 and random ABI tests.

- Unit tests per operation kind: small modules compiled by tier0 and by LLVM, linked into one
  program, results compared (this also checks the calling convention in both directions).
- The conformance suite with `--tier0`, and differential runs against the LLVM objects.
- Object files checked with the system tools (otool, nm, objdump) and linked with the normal
  link step.
