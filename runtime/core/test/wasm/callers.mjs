// Reference WASI host for wasm tracebacks: node callers.mjs prog.wasm [args...]
//
// A module linked with MARMOT_WASM_HOSTCALLERS=1 (wasm-callers.o) imports two
// functions from "marmot":
//
//   callers(buf, max) -> n: the module byte offsets of the wasm frames on the
//     stack, innermost first, as u32 values at buf; at most max; the host's
//     own frames left out. V8 reports them in Error().stack as
//     "wasm-function[N]:0xOFFSET".
//   pcln(ptr, cap) -> len: the length of the module's ".marmot_pcln" custom
//     section; the section is copied to ptr only when cap >= len.
//
// The runtime calls pcln once, with cap 0 and then with the length, and
// callers for each walk (runtime.Callers, runtime.Caller, panic tracebacks).
// A module without these imports runs as with any WASI host.
import { readFileSync } from 'node:fs';
import { WASI } from 'node:wasi';

const file = process.argv[2];
const mod = new WebAssembly.Module(readFileSync(file));
const wasi = new WASI({ version: 'preview1', args: process.argv.slice(2), env: { MARMOT_OUTPUT: 'plain' }, returnOnExit: true });
let inst;
Error.stackTraceLimit = 256;

const marmot = {
  callers(buf, max) {
    const out = [];
    for (const line of new Error().stack.split('\n')) {
      const m = line.match(/wasm-function\[\d+\]:0x([0-9a-f]+)/);
      if (m) out.push(parseInt(m[1], 16));
    }
    const n = Math.min(max, out.length);
    new Uint32Array(inst.exports.memory.buffer, buf, n).set(out.slice(0, n));
    return n;
  },
  pcln(ptr, cap) {
    const s = WebAssembly.Module.customSections(mod, '.marmot_pcln');
    if (!s.length) return 0;
    const b = new Uint8Array(s[0]);
    if (cap >= b.length) new Uint8Array(inst.exports.memory.buffer, ptr, b.length).set(b);
    return b.length;
  },
};

const imports = { wasi_snapshot_preview1: wasi.wasiImport };
if (WebAssembly.Module.imports(mod).some((i) => i.module === 'marmot')) imports.marmot = marmot;
inst = new WebAssembly.Instance(mod, imports);
process.exitCode = wasi.start(inst);
