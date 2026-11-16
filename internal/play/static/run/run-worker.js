// The Web Worker a program runs in: messages {bytes} in; {type: 'stdout'|'stderr', text},
// then {type: 'exit', code} or {type: 'error', message, start} out (start: the program did
// not start, for example a JSPI build where the engine has no JSPI; nothing ran, so the
// page can run an Asyncify build instead). The page ends a program that runs too long by
// terminating the worker.
import { runWasi, StartError } from './wasi.js';

self.onmessage = async (e) => {
  try {
    const code = await runWasi(e.data.bytes, {
      args: ['main.wasm'],
      // the plain panic report (what Go prints): no terminal codes, no hint about the flag
      // 16 KiB goroutine stacks: 100k goroutines fit in wasm's 4 GiB (the default 64 KiB does not)
      env: { MARMOT_OUTPUT: 'plain', MARMOT_STACKSIZE: '16384' },
      onStdout: (text) => self.postMessage({ type: 'stdout', text }),
      onStderr: (text) => self.postMessage({ type: 'stderr', text }),
    });
    self.postMessage({ type: 'exit', code });
  } catch (err) {
    self.postMessage({ type: 'error', message: String(err && err.message ? err.message : err), start: err instanceof StartError });
  }
};
