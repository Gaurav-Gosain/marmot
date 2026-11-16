// jspi.mjs: runs a runtime test linked with the wasip1-wasm-jspi library
// under Node's WASI, with the JSPI host side of goroutine switching
// (src/arch_wasm32.c, RT_WASM_JSPI).
//
//   node test/wasm/jspi.mjs PROG.wasm [ARGS...]
//
// _start and marmot_gorun are wrapped with WebAssembly.promising,
// marmot.switch with WebAssembly.Suspending. proc_exit on any stack ends the
// run with its status; a trap prints the error and exits with status 2.
import { readFile } from "node:fs/promises";
import { basename } from "node:path";

process.removeAllListeners("warning");
const { WASI } = await import("node:wasi");

const [prog, ...args] = process.argv.slice(2);
const wasi = new WASI({ version: "preview1", args: [basename(prog), ...args], env: process.env, returnOnExit: true });

class Exit {
  constructor(code) { this.code = code; }
}
let finish;
const done = new Promise((r) => (finish = r));
const fail = (e) => {
  if (e instanceof Exit) return finish(e.code);
  process.stderr.write(`wasm: ${e && e.stack ? e.stack : e}\n`);
  finish(2);
};

// Stacks suspended in marmot.switch, by context address: the resolve of the
// promise each one waits on.
const waiting = new Map();
let gorun;
const marmot = {
  switch: new WebAssembly.Suspending((from, to, fresh) => {
    const p = new Promise((r) => waiting.set(from, r));
    // Start to, or resume it (its promise settles once this stack has
    // suspended). A fresh context may reuse an exited goroutine's address:
    // its old stack is dropped. A new stack starts from a microtask, on
    // top of no wasm frames.
    const r = waiting.get(to);
    waiting.delete(to);
    if (fresh) queueMicrotask(() => gorun(to).catch(fail));
    else r();
    return p;
  }),
};

const mod = await WebAssembly.compile(await readFile(prog));
const imports = {
  wasi_snapshot_preview1: {
    ...wasi.wasiImport,
    proc_exit(code) {
      throw new Exit(code);
    },
  },
  marmot,
};
const inst = await WebAssembly.instantiate(mod, imports);
wasi.initialize({ exports: { memory: inst.exports.memory } });
gorun = WebAssembly.promising(inst.exports.marmot_gorun);
WebAssembly.promising(inst.exports._start)().then(() => finish(0), fail);
process.exit(await done);
