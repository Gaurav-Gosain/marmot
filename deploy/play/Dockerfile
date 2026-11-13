# The playground's compile service: marmot with its backend, runtime and LLVM 22, serving
# POST /compile (marmot play -service). It compiles and never runs programs.
#
#   deploy/play/build.sh [IMAGE]       builds it from the committed tree (linux/amd64)
#
# LLVM comes from apt.llvm.org (every target, WebAssembly included); Go from go.dev.

ARG DEBIAN=trixie
FROM debian:${DEBIAN}-slim AS toolchain
ARG DEBIAN
ARG LLVM=22
ARG GO_VERSION=1.27.4
ARG TARGETARCH=amd64
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl gnupg \
	&& curl -fsSL https://apt.llvm.org/llvm-snapshot.gpg.key | gpg --dearmor -o /usr/share/keyrings/llvm.gpg \
	&& echo "deb [signed-by=/usr/share/keyrings/llvm.gpg] http://apt.llvm.org/${DEBIAN}/ llvm-toolchain-${DEBIAN}-${LLVM} main" > /etc/apt/sources.list.d/llvm.list \
	&& apt-get update && apt-get install -y --no-install-recommends \
		clang-${LLVM} lld-${LLVM} llvm-${LLVM} libc6-dev libgcc-14-dev make \
	&& rm -rf /var/lib/apt/lists/*
RUN curl -fsSL https://go.dev/dl/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz | tar -C /usr/local -xz
ENV PATH=/usr/local/go/bin:/usr/lib/llvm-${LLVM}/bin:$PATH \
	LLVM_PREFIX=/usr/lib/llvm-${LLVM} LLVM_BIN=/usr/lib/llvm-${LLVM}/bin GOTOOLCHAIN=local

FROM toolchain AS build
ARG LLVM=22
ARG JOBS=6
RUN apt-get update && apt-get install -y --no-install-recommends \
		llvm-${LLVM}-dev libmlir-${LLVM}-dev mlir-${LLVM}-tools cmake ninja-build git \
	&& rm -rf /var/lib/apt/lists/*
# LLVM's cmake package names these libraries in its link interfaces.
RUN apt-get update && apt-get install -y --no-install-recommends zlib1g-dev libzstd-dev libcurl4-openssl-dev libedit-dev \
	&& rm -rf /var/lib/apt/lists/*
COPY . /marmot
WORKDIR /marmot
RUN go build -o build/bin/marmot ./cmd/marmot
RUN BACKEND_BIN_DIR=/marmot/build/bin ./backend/build.sh -DCMAKE_C_COMPILER=clang -DCMAKE_CXX_COMPILER=clang++ \
	&& rm -rf build/backend
RUN make -s -C runtime/core LLVM=/usr/lib/llvm-${LLVM} -j${JOBS}

FROM toolchain
ARG LLVM=22
# libMLIR for the backend; nothing else of the build stage.
RUN apt-get update && apt-get install -y --no-install-recommends libmlir-${LLVM} \
	&& rm -rf /var/lib/apt/lists/* && useradd -u 10001 -m play
COPY --from=build /marmot/build/bin /marmot/build/bin
COPY --from=build /marmot/build/lib /marmot/build/lib
COPY --from=build /marmot/lib /marmot/lib
COPY --from=build /marmot/deploy/play/warm.sh /marmot/warm.sh
ENV MARMOT_ROOT=/marmot HOME=/home/play GOCACHE=/home/play/.cache/go GOPROXY=off GOFLAGS=-mod=mod \
	MARMOT_CACHE_MAX_MB=4096
# marmot writes its merged GOROOT and cache under build/.
RUN mkdir -p /marmot/build/cache && chown play /marmot/build && chown -R play /marmot/build/cache
USER play
# The standard library compiled once for every mode and stage, so requests start warm.
RUN sh /marmot/warm.sh
EXPOSE 8080
ENTRYPOINT ["/marmot/build/bin/marmot", "play", "-service", "-addr", "0.0.0.0:8080"]
