// run.mjs: runs a WASI preview 1 program (wasip1/wasm) under Node's WASI.
//
//   node tests/wasm/run.mjs [--dir HOST[:GUEST]]... PROG.wasm [ARGS...]
//
// The program gets ARGS (with its own name first, as argv[0]), the
// environment, stdin, stdout and stderr, and the directories given with
// --dir (none by default). Its proc_exit status becomes the exit status;
// a trap prints the error and exits with status 2, as a Go runtime fatal
// error does. MARMOT_WASM_HOST=browser runs it under the playground's
// browser WASI host instead (internal/play/static/run/wasi.js).
import { readFile } from "node:fs/promises";
import { basename } from "node:path";

// Node warns that WASI is experimental; the program's stderr must be its own.
process.removeAllListeners("warning");
const { WASI } = await import("node:wasi");

const argv = process.argv.slice(2);
const preopens = {};
while (argv.length && argv[0] === "--dir") {
  argv.shift();
  const spec = argv.shift() ?? "";
  const i = spec.indexOf(":");
  const host = i < 0 ? spec : spec.slice(0, i);
  const guest = i < 0 ? spec : spec.slice(i + 1);
  preopens[guest] = host;
}
if (argv.length === 0) {
  process.stderr.write("usage: node run.mjs [--dir HOST[:GUEST]]... PROG.wasm [ARGS...]\n");
  process.exit(2);
}
const [prog, ...args] = argv;

const wasi = new WASI({
  version: "preview1",
  args: [basename(prog), ...args],
  env: process.env,
  preopens,
  returnOnExit: true,
});

let code;
if (process.env.MARMOT_WASM_HOST === "browser") {
  // The browser's WASI host (the playground's internal/play/static/run/wasi.js) instead of
  // Node's: no files, stdin at EOF; what a program run on /play sees.
  const { writeSync } = await import("node:fs");
  const { runWasi } = await import(new URL("../../internal/play/static/run/wasi.js", import.meta.url));
  try {
    code = await runWasi(await readFile(prog), {
      args: [basename(prog), ...args],
      env: Object.fromEntries(Object.entries(process.env).filter(([k]) => k.startsWith("MARMOT_") || k.startsWith("GO"))),
      onStdout: (t) => writeSync(1, t),
      onStderr: (t) => writeSync(2, t),
    });
  } catch (e) {
    process.stderr.write(`wasm: ${e && e.stack ? e.stack : e}\n`);
    code = 2;
  }
  process.exit(code ?? 0);
}
try {
  const module = await WebAssembly.compile(await readFile(prog));
  const instance = await WebAssembly.instantiate(module, wasi.getImportObject());
  code = wasi.start(instance);
} catch (e) {
  process.stderr.write(`wasm: ${e && e.stack ? e.stack : e}\n`);
  code = 2;
}
process.exitCode = code ?? 0;
