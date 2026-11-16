// Run for an edited program, in the browser (or Node): the front end (marmot-play.wasm -run)
// turns the source into the program's own module, the backend (play tools) compiles it to an
// object, wasm-ld links it with the prebuilt standard library and runtime, and binaryen's
// Asyncify pass makes goroutine switches work. The result runs in run-worker.js.
// With switches: 'jspi' the build links the JSPI runtime instead and runs binaryen's
// spill-pointers pass (about 7 times faster than Asyncify, and half the size); the result
// needs an engine with JSPI (wasi.js jspiSupported). The standard library is the same.
//
//   const b = new Builder(assets, { cache, mode, fs });
//   const r = await b.build(src, { onProgress, switches });  // {ok, wasm, diagnostics, error, times}
//
// assets holds loaders, each called at most once, on the first build that needs it:
//   front(): WebAssembly.Module of marmot-play.wasm   bundle(): Uint8Array of its tar (not
//   needed when fs, a MemFS with the tar loaded, is given)
//   tools(): the createTools factory of play-tools.mjs   toolsModule(): its WebAssembly.Module
//   std(), rt(), rtBitcode(): Uint8Array of std-<mode>.a, librtcore.a, rtcore.bc
//   rtJSPI(), rtBitcodeJSPI(): the same for the JSPI runtime (only for switches: 'jspi')
//   binaryen(): the binaryen.js module
//   prefetch(name) (optional): download an asset without evaluating it (into the HTTP cache)
//   version: a string that changes with any asset (part of the cache key)
// cache (optional): {get(key) -> Uint8Array|undefined, put(key, bytes)}, see browserCache.
import { runWasi } from './wasi.js';
import { MemFS } from './memfs.js';

// The backend arguments of the driver for a wasip1/wasm dev build (marmot build -target
// wasip1/wasm -x), minus the batch, job and cache options: the same as for the prebuilt std.
export const backendArgs = ['-O', '2', '--emit=obj', '--rt-bitcode=/rtcore.bc', '--target=wasip1/wasm',
  '-fast-isel=false', '-split-large=20000', '-tier0', '-keep-call-sites'];

export class Builder {
  constructor(assets, { cache, mode = 'dev', fs } = {}) {
    this.assets = assets;
    this.cache = cache;
    this.mode = mode;
    this.fs = fs;
    this.memo = new Map();
  }

  // load: an asset, loaded once.
  load(name) {
    if (!this.memo.has(name)) this.memo.set(name, Promise.resolve().then(() => this.assets[name]()));
    return this.memo.get(name);
  }

  async build(src, { onProgress = () => {}, switches = 'asyncify' } = {}) {
    const jspi = switches === 'jspi';
    const times = {};
    const step = async (stage, fn) => {
      onProgress({ stage });
      const t0 = performance.now();
      try { return await fn(); } finally { times[stage] = Math.round(performance.now() - t0); }
    };
    const key = await sha256(this.assets.version + '\0' + this.mode + '\0' + switches + '\0' + src);
    if (this.cache) {
      const hit = await this.cache.get(key);
      if (hit) return { ok: true, wasm: hit, diagnostics: [], times, cached: true };
    }
    const front = await step('compile', () => this.front(src));
    if (!front.ok || !front.runMlir) return { ok: false, diagnostics: front.diagnostics || [], error: front.error, times };
    // the link and switch steps' assets load while the backend runs (binaryen.js is 13 MB).
    // Not earlier: Firefox stalled with every asset loading at once beside the front end.
    // binaryen.js is only downloaded here (assets.prefetch): evaluating it beside a start of
    // the play tools stalled the worker in Chrome and in Firefox.
    for (const a of ['std', jspi ? 'rtJSPI' : 'rt']) this.load(a).catch(() => {});
    this.assets.prefetch?.('binaryen')?.catch(() => {});
    const obj = await step('codegen', async () => {
      const r = await this.tool([...backendArgs, '-o', '/u.o', '/u.mlir'],
        { '/u.mlir': front.runMlir, '/rtcore.bc': await this.load(jspi ? 'rtBitcodeJSPI' : 'rtBitcode') }, '/u.o');
      return r;
    });
    if (!obj.out) return { ok: false, diagnostics: [], error: 'codegen failed (exit ' + obj.code + '):\n' + obj.err, times };
    const linked = await step('link', async () => this.tool(['wasm-ld', '-o', '/p.wasm', ...(jspi ? ['--export=marmot_gorun'] : []), '/u.o', '/std.a', '/rt.a'],
      { '/u.o': obj.out, '/std.a': await this.load('std'), '/rt.a': await this.load(jspi ? 'rtJSPI' : 'rt') }, '/p.wasm'));
    if (!linked.out) return { ok: false, diagnostics: [], error: 'link failed (exit ' + linked.code + '):\n' + linked.err, times };
    const wasm = jspi
      ? await step('spill', async () => spillPointers(await this.load('binaryen'), linked.out))
      : await step('asyncify', async () => asyncify(await this.load('binaryen'), linked.out));
    if (this.cache) await this.cache.put(key, wasm);
    return { ok: true, wasm, diagnostics: front.diagnostics || [], times };
  }

  // front: marmot-play -run on the source; the parsed reply.
  async front(src) {
    if (!this.fs) {
      const [bundle] = await Promise.all([this.load('bundle'), this.load('front')]);
      this.fs = new MemFS();
      this.fs.loadTar(bundle, '/');
    }
    this.fs.writeFile('/play/main.go', src);
    let out = '', err = '';
    const code = await runWasi(await this.load('front'), {
      fs: this.fs, preopens: ['/'],
      args: ['marmot-play', '-mode', this.mode, '-run', '/play/main.go'],
      env: { HOME: '/tmp', PWD: '/play', TMPDIR: '/tmp', MARMOT_ROOT: '/root', MARMOT_MERGED_GOROOT: '/goroot' },
      onStdout: (t) => (out += t), onStderr: (t) => (err += t),
    });
    try {
      return JSON.parse(out);
    } catch {
      return { ok: false, error: 'the compiler stopped (exit ' + code + '):\n' + err.slice(-2000) };
    }
  }

  // tool: one run of play tools (a fresh instance: the runtime exits after main) with the
  // given files; the output file, the exit code and the end of stderr. When the instance
  // does not start (instantiate fails, for example when the browser has no memory left for
  // it), the code is -1: Emscripten's createTools never settles after a failed
  // instantiateWasm, so the failure is raced against it (the build waited forever before).
  async tool(args, files, output) {
    const [createTools, mod] = await Promise.all([this.load('tools'), this.load('toolsModule')]);
    let err = '';
    let failed;
    const failure = new Promise((_, reject) => (failed = reject));
    let t;
    try {
      t = await Promise.race([
        createTools({
          instantiateWasm: (imports, ok) => { WebAssembly.instantiate(mod, imports).then((i) => ok(i, mod), failed); return {}; },
          print: () => {}, printErr: (s) => (err += s + '\n'),
        }),
        failure,
      ]);
    } catch (e) {
      return { code: -1, err: 'the build tools did not start: ' + String(e?.message ?? e) + '\nClose other tabs, then run again.', out: null };
    }
    for (const [p, d] of Object.entries(files)) t.FS.writeFile(p, d);
    let code = 0;
    try { code = t.callMain(args); } catch (e) { code = e && e.status !== undefined ? e.status : String(e); }
    let out = null;
    if (code === 0) try { out = t.FS.readFile(output); } catch {}
    return { code, err: err.slice(-4000), out };
  }
}

// asyncify: the runtime's Asyncify pass (internal/driver/wasm.go asyncifyFlags), no optimization.
export function asyncify(binaryen, bytes) {
  const m = binaryen.readBinary(bytes);
  try {
    binaryen.setPassArgument('asyncify-imports', 'asyncify.start_unwind');
    binaryen.setPassArgument('asyncify-removelist', 'ctx_run');
    binaryen.setOptimizeLevel(0);
    // names stay (as wasm-opt -g in the driver): stack traces of traps name the functions
    binaryen.setDebugInfo(true);
    m.runPasses(['asyncify']);
    return m.emitBinary();
  } finally {
    m.dispose();
  }
}

// spillPointers: the JSPI runtime's pass (runtime/core Makefile JSPI_FLAGS): every pointer
// live across a call is stored to the shadow stack, where the collector scans it (the
// locals of a suspended engine stack are out of its reach). No optimization.
export function spillPointers(binaryen, bytes) {
  const m = binaryen.readBinary(bytes);
  try {
    binaryen.setOptimizeLevel(0);
    binaryen.setDebugInfo(true);
    m.runPasses(['spill-pointers']);
    return m.emitBinary();
  } finally {
    m.dispose();
  }
}

// browserCache: a cache of built programs in the Cache API, by content hash.
export function browserCache(name = 'marmot-play-run') {
  const url = (key) => new URL('/__run/' + key, self.location.origin).href;
  return {
    async get(key) {
      const r = await (await caches.open(name)).match(url(key));
      return r ? new Uint8Array(await r.arrayBuffer()) : undefined;
    },
    async put(key, bytes) {
      await (await caches.open(name)).put(url(key), new Response(bytes, { headers: { 'content-type': 'application/wasm' } }));
    },
  };
}

async function sha256(s) {
  const d = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(s));
  return [...new Uint8Array(d)].map((b) => b.toString(16).padStart(2, '0')).join('');
}
