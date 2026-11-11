# runtime/core: the C runtime core. Builds build/lib/librtcore.a and build/lib/rtcore.bc.
#
#   make                 library and bitcode
#   make test            build and run test/*.c against the library
#   make test-asan       tests with AddressSanitizer and UBSan
#   make test-tsan       tests with ThreadSanitizer
#   make bench           build and run bench/*.c
#   make check-targets   compile check for linux/amd64, linux/arm64, darwin/amd64

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
GOOS    ?=
GOARCH  ?=
ifneq ($(GOOS)$(GOARCH),)
SUFFIX  := /$(GOOS)-$(GOARCH)
TRIPLE_linux_amd64  := x86_64-linux-gnu
TRIPLE_linux_arm64  := aarch64-linux-gnu
TRIPLE_darwin_arm64 := arm64-apple-macosx11.0
TRIPLE_darwin_amd64 := x86_64-apple-macosx11.0
TRIPLE  := $(TRIPLE_$(GOOS)_$(GOARCH))
TARGET_FLAGS := --target=$(TRIPLE) $(if $(SYSROOT_DIR),--sysroot=$(SYSROOT_DIR),)
endif
OUT     := $(ROOT)/build/lib$(SUFFIX)
OBJ     := $(ROOT)/build/rtcore$(SUFFIX)

ifeq ($(UNAME_S),Darwin)
SDK     := $(shell xcrun --show-sdk-path 2>/dev/null)
SYSROOT := $(if $(SDK),-isysroot $(SDK),)
endif

CFLAGS_COMMON := $(TARGET_FLAGS) -std=c11 -Wall -Wextra -Wno-unused-parameter -Iinclude -Isrc $(SYSROOT) \
	-fno-exceptions -fno-unwind-tables -fno-asynchronous-unwind-tables \
	-ffunction-sections -fdata-sections -fno-stack-protector -fno-common \
	-fvisibility=hidden -D_DARWIN_C_SOURCE -D_GNU_SOURCE
CFLAGS  ?= -O2
ALL_CFLAGS := $(CFLAGS_COMMON) $(CFLAGS)

C_SRCS  := $(wildcard src/*.c)
# Cold code (startup, signals, timers, I/O, OS glue) is built for size.
COLD    := entry io libcall netpoll os_darwin os_linux signal timer util par
COLD_OBJS := $(patsubst %,$(OBJ)/%.o,$(COLD))
COLD_BCS  := $(patsubst %,$(OBJ)/%.bc,$(COLD))
S_SRCS  := $(wildcard src/*.S)
C_OBJS  := $(patsubst src/%.c,$(OBJ)/%.o,$(C_SRCS))
S_OBJS  := $(patsubst src/%.S,$(OBJ)/%.o,$(S_SRCS))
BCS     := $(patsubst src/%.c,$(OBJ)/%.bc,$(C_SRCS))
HDRS    := $(wildcard src/*.h) include/rt.h

.PHONY: all test test-asan test-tsan bench clean check-targets

all: $(OUT)/librtcore.a $(OUT)/rtcore.bc

$(OBJ)/%.o: src/%.c $(HDRS) | $(OBJ)
	$(CC) $(ALL_CFLAGS) $(if $(filter $@,$(COLD_OBJS)),-Oz) -c $< -o $@

$(OBJ)/%.o: src/%.S | $(OBJ)
	$(CC) $(TARGET_FLAGS) $(SYSROOT) -c $< -o $@

$(OBJ)/%.bc: src/%.c $(HDRS) | $(OBJ)
	$(CC) $(ALL_CFLAGS) $(if $(filter $@,$(COLD_BCS)),-Oz) -emit-llvm -c $< -o $@

$(OUT)/librtcore.a: $(C_OBJS) $(S_OBJS) | $(OUT)
	rm -f $@
	$(AR) rcs $@ $^

$(OUT)/rtcore.bc: $(BCS) | $(OUT)
	$(LINK) $^ -o $@

$(OBJ) $(OUT):
	mkdir -p $@

# ---- tests ----

TEST_SRCS := $(wildcard test/*.c)

test: all
	@OUT=$(OBJ)/test LIB=$(OUT)/librtcore.a CC="$(CC)" CFLAGS="-O1 -g $(SYSROOT) -Iinclude" \
		sh test/run.sh $(TEST_SRCS)

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
