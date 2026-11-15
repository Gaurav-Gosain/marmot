// The Web Worker a program runs in: messages {bytes} in; {type: 'stdout'|'stderr', text},
// then {type: 'exit', code} or {type: 'error', message} out. The page ends a program that
// runs too long by terminating the worker.
import { runWasi } from './wasi.js';

self.onmessage = async (e) => {
  try {
    const code = await runWasi(e.data.bytes, {
      args: ['main.wasm'],
      // the plain panic report (what Go prints): no terminal codes, no hint about the flag
      env: { MARMOT_OUTPUT: 'plain' },
      onStdout: (text) => self.postMessage({ type: 'stdout', text }),
      onStderr: (text) => self.postMessage({ type: 'stderr', text }),
    });
    self.postMessage({ type: 'exit', code });
  } catch (err) {
    self.postMessage({ type: 'error', message: String(err && err.message ? err.message : err) });
  }
};
