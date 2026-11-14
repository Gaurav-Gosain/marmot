# runtime/core: the C runtime core. Builds build/lib/librtcore.a and build/lib/rtcore.bc.
#
#   make                 library and bitcode
#   make test            build and run test/*.c against the library
#   make test-asan       tests with AddressSanitizer and UBSan
#   make test-tsan       tests with ThreadSanitizer
#   make bench           build and run bench/*.c
#   make check-targets   compile check for linux/amd64, linux/arm64, darwin/amd64
#   make GOOS=wasip1 GOARCH=wasm [test]   WASI preview 1 (tests run under Node)

UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Darwin)
LLVM    ?= /opt/homebrew/opt/llvm@22
else
LLVM    ?= /usr
endif
CC      := $(LLVM)/bin/clang
AR      := $(LLVM)/bin/llvm-ar
LINK    := $(LLVM)/bin/llvm-link

ROOT    := $(abspath ../..)

# Cross builds: make GOOS=linux GOARCH=arm64 [SYSROOT_DIR=...] puts the
# library in build/lib/linux-arm64. Without GOOS/GOARCH the host build goes
# to build/lib (what the driver uses by default).
#
# LIBC=musl (with GOOS=linux) builds for musl instead of glibc, for static
# binaries (marmot build -static): build/lib/linux-ARCH-musl, against the
# sysroot that marmot sysroot add -libc musl linux/ARCH creates in
# build/cache/sysroot/linux-ARCH-musl (or SYSROOT_DIR).
GOOS    ?=
GOARCH  ?=
LIBC    ?=
ifeq ($(LIBC),musl)
ifneq ($(GOOS),linux)
$(error LIBC=musl needs GOOS=linux)
endif
SYSROOT_DIR ?= $(ROOT)/build/cache/sysroot/linux-$(GOARCH)-musl
endif
ifneq ($(GOOS)$(GOARCH),)
SUFFIX  := /$(GOOS)-$(GOARCH)$(if $(filter musl,$(LIBC)),-musl,)
LIBCTRIPLE := $(if $(filter musl,$(LIBC)),musl,gnu)
TRIPLE_linux_amd64  := x86_64-linux-$(LIBCTRIPLE)
TRIPLE_linux_arm64  := aarch64-linux-$(LIBCTRIPLE)
TRIPLE_darwin_arm64 := arm64-apple-macosx11.0
TRIPLE_darwin_amd64 := x86_64-apple-macosx11.0
TRIPLE_wasip1_wasm  := wasm32-unknown-wasip1
TRIPLE  := $(TRIPLE_$(GOOS)_$(GOARCH))
TARGET_FLAGS := --target=$(TRIPLE) $(if $(SYSROOT_DIR),--sysroot=$(SYSROOT_DIR),)
endif

# wasip1/wasm: no libc (os_wasi.c has the few C functions the compiler
# calls); programs are linked with wasm-ld and then rewritten by wasm-opt's
# Asyncify pass for goroutine switching (src/arch_wasm32.c). These are the
# flags of that pass.
WASM    := $(filter wasm,$(GOARCH))
ifneq ($(WASM),)
TARGET_FLAGS += -ffreestanding -nostdlibinc
endif
ifeq ($(UNAME_S),Darwin)
WASMLD  ?= /opt/homebrew/opt/lld@22/bin/wasm-ld
else
WASMLD  ?= $(LLVM)/bin/wasm-ld
endif
WASMOPT ?= wasm-opt
NODE    ?= node
# Asyncify runs before any optimization (wasm-opt runs passes in order):
# the optimizer may inline ctx_run, which must stay out of the rewrite.
# clang's own wasm-opt run after linking is turned off (--no-wasm-opt).
# Code that calls setjmp (Go functions with defer frames) is compiled with
# WASM_SJLJ_FLAGS.
WASM_SJLJ_FLAGS := -mexception-handling -mllvm -wasm-enable-sjlj -mllvm -wasm-use-legacy-eh=true
ASYNCIFY_FLAGS := --asyncify --pass-arg=asyncify-imports@asyncify.start_unwind \
	--pass-arg=asyncify-removelist@ctx_run
OUT     := $(ROOT)/build/lib$(SUFFIX)
OBJ     := $(ROOT)/build/rtcore$(SUFFIX)
# build/lib only ever holds a committed state: with uncommitted changes
# under runtime/core (in a git checkout), build into .work/rtcore instead;
# commit, then make again to install.
DIRTY   := $(shell git -C $(ROOT) status --porcelain -- runtime/core 2>/dev/null | head -1)
ifneq ($(DIRTY),)
OUT     := $(ROOT)/.work/rtcore/lib$(SUFFIX)
OBJ     := $(ROOT)/.work/rtcore/obj$(SUFFIX)
$(info runtime/core has uncommitted changes: building into .work/rtcore, not build/lib)
endif

ifeq ($(UNAME_S),Darwin)
SDK     := $(shell xcrun --show-sdk-path 2>/dev/null)
SYSROOT := $(if $(SDK),-isysroot $(SDK),)
endif
ifneq ($(WASM),)
SYSROOT :=
endif

CFLAGS_COMMON := $(TARGET_FLAGS) -std=c11 -Wall -Wextra -Wno-unused-parameter -Iinclude -Isrc $(SYSROOT) \
	-fno-exceptions -fno-unwind-tables -fno-asynchronous-unwind-tables \
	-ffunction-sections -fdata-sections -fno-stack-protector -fno-common \
	-fvisibility=hidden -D_DARWIN_C_SOURCE -D_GNU_SOURCE
CFLAGS  ?= -O2
# Frame pointers: the wakeup trace (trace.c) walks the frame chain from
# runtime functions into the program.
ifeq ($(WASM),)
CFLAGS_COMMON += -fno-omit-frame-pointer
endif
ALL_CFLAGS := $(CFLAGS_COMMON) $(CFLAGS)

C_SRCS  := $(wildcard src/*.c) $(if $(WASM),,$(wildcard gpu/*.c))
# Cold code (startup, signals, timers, I/O, OS glue) is built for size.
COLD    := cpu entry io libcall netpoll os_darwin os_linux pcln gcpar poison signal timer util par gpu watchdog
COLD_OBJS := $(patsubst %,$(OBJ)/%.o,$(COLD))
COLD_BCS  := $(patsubst %,$(OBJ)/%.bc,$(COLD))
S_SRCS  := $(if $(WASM),,$(wildcard src/*.S))
C_OBJS  := $(patsubst %.c,$(OBJ)/%.o,$(notdir $(C_SRCS)))
S_OBJS  := $(patsubst src/%.S,$(OBJ)/%.o,$(S_SRCS))
BCS     := $(patsubst %.c,$(OBJ)/%.bc,$(notdir $(C_SRCS)))
HDRS    := $(wildcard src/*.h) include/rt.h

.PHONY: all test test-asan test-tsan bench clean check-targets

all: $(OUT)/librtcore.a $(OUT)/rtcore.bc

vpath %.c src gpu

$(OBJ)/%.o: %.c $(HDRS) | $(OBJ)
	$(CC) $(ALL_CFLAGS) $(if $(filter $@,$(COLD_OBJS)),-Oz) -c $< -o $@

$(OBJ)/%.o: src/%.S | $(OBJ)
	$(CC) $(TARGET_FLAGS) $(SYSROOT) -c $< -o $@

$(OBJ)/%.bc: %.c $(HDRS) | $(OBJ)
	$(CC) $(ALL_CFLAGS) $(if $(filter $@,$(COLD_BCS)),-Oz) -emit-llvm -c $< -o $@

$(OUT)/librtcore.a: $(C_OBJS) $(S_OBJS) | $(OUT)
	rm -f $@
	$(AR) rcs $@ $^

$(OUT)/rtcore.bc: $(BCS) | $(OUT)
	$(LINK) $^ -o $@

$(OBJ) $(OUT):
	mkdir -p $@

# ---- tests ----

TESTS ?= $(wildcard test/*.c)
TEST_SRCS := $(TESTS)

ifneq ($(WASM),)
# Tests marked "// wasm: skip" (threads, signals, processes, libc) are left
# out; test/wasm has the libc headers the others include.
test: all
	@WASM=1 OUT=$(OBJ)/test LIB=$(OUT)/librtcore.a LDLIBS= CC="$(CC)" \
		CFLAGS="$(TARGET_FLAGS) $(WASM_SJLJ_FLAGS) -O1 -g -Iinclude -Itest/wasm -nostdlib -fuse-ld=$(WASMLD) --no-wasm-opt" \
		POSTLINK="$(WASMOPT) $(ASYNCIFY_FLAGS) -O" RUN="$(NODE) $(ROOT)/tests/wasm/run.mjs" \
		sh test/run.sh $(TEST_SRCS)
else
test: all
	@OUT=$(OBJ)/test LIB=$(OUT)/librtcore.a CC="$(CC)" CFLAGS="-O1 -g $(SYSROOT) -Iinclude" \
		sh test/run.sh $(TEST_SRCS)
endif

# Sanitizer builds compile the runtime sources directly into each test.
SAN_SRC := $(C_SRCS) $(S_SRCS)

test-asan:
	@OUT=$(OBJ)/test-asan LIB="$(abspath $(SAN_SRC))" CC="$(CC)" \
		CFLAGS="-O1 -g $(CFLAGS_COMMON) -DRT_SANITIZE=1 -fsanitize=address,undefined -fno-sanitize=alignment -fno-omit-frame-pointer" \
		sh test/run.sh $(TEST_SRCS)

test-tsan:
	@TSAN=1 OUT=$(OBJ)/test-tsan LIB="$(abspath $(SAN_SRC))" CC="$(CC)" \
		CFLAGS="-O1 -g $(CFLAGS_COMMON) -DRT_SANITIZE=1 -fsanitize=thread" \
		sh test/run.sh $(TEST_SRCS)

# ---- benchmarks ----

bench: all
	@mkdir -p $(OBJ)/bench
	@for f in bench/*.c; do \
		n=$$(basename $$f .c); \
		$(CC) -O2 $(SYSROOT) -Iinclude $$f $(OUT)/librtcore.a -o $(OBJ)/bench/$$n || exit 1; \
		echo "== $$n"; $(OBJ)/bench/$$n || exit 1; \
	done

# ---- cross compile checks (untested targets) ----

# Compile every source for another target (needs that target's C headers;
# darwin targets use the macOS SDK). Example: make check-targets TARGETS=x86_64-apple-macosx
TARGETS ?= x86_64-apple-macosx
check-targets:
	@for t in $(TARGETS); do \
		echo "== $$t"; \
		for f in $(C_SRCS) $(S_SRCS); do \
			$(CC) --target=$$t $(CFLAGS_COMMON) -O2 -c $$f -o /dev/null || exit 1; \
		done; \
	done

clean:
	rm -rf $(OBJ) $(OUT)/librtcore.a $(OUT)/rtcore.bc
