// A WASI preview 1 host for one Go program in a browser (or Node): stdout and stderr go to
// callbacks as they are written, no files (no preopened directories), no network; time,
// sleep and random work. Enough for programs built for wasip1/wasm, by marmot or gc.
// runWasi(bytes, { args, env, onStdout(text), onStderr(text) }) resolves to the exit code.
// The docs site's /play runs programs with this same file (and run-worker.js), and
// tests/wasm/run.mjs uses it with MARMOT_WASM_HOST=browser, so the conformance programs
// check it.

const ESUCCESS = 0, EBADF = 8, EINVAL = 28, ENOSYS = 52;

class Exit extends Error {
  constructor(code) { super('exit ' + code); this.code = code; }
}

export async function runWasi(bytes, opt = {}) {
  const args = opt.args || ['main.wasm'];
  const env = Object.entries(opt.env || {}).map(([k, v]) => `${k}=${v}`);
  const enc = new TextEncoder();
  const dec = [new TextDecoder(), new TextDecoder()];
  let memory;
  const view = () => new DataView(memory.buffer);
  const u8 = () => new Uint8Array(memory.buffer);
  const out = [null, opt.onStdout || (() => {}), opt.onStderr || (() => {})];
  const now = () => BigInt(Math.round(performance.now() * 1e6));
  const realNow = () => BigInt(Date.now()) * 1000000n;

  // strings: a list of NUL terminated buffers, pointers at ptrs, bytes at buf
  const putStrings = (list, ptrs, buf) => {
    const v = view(), m = u8();
    for (const s of list) {
      v.setUint32(ptrs, buf, true); ptrs += 4;
      const b = enc.encode(s + '\0'); m.set(b, buf); buf += b.length;
    }
    return ESUCCESS;
  };
  const sizes = (list, pn, pb) => {
    const v = view();
    v.setUint32(pn, list.length, true);
    v.setUint32(pb, list.reduce((n, s) => n + enc.encode(s).length + 1, 0), true);
    return ESUCCESS;
  };
  const sleepUntil = (deadline) => {
    // No timers can run inside a synchronous host call: wait in place. In a Web Worker this
    // only blocks the program's own thread.
    if (typeof SharedArrayBuffer !== 'undefined' && typeof Atomics !== 'undefined' && Atomics.wait) {
      const ms = Number(deadline - now()) / 1e6;
      if (ms > 0) {
        try { Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, ms); return; } catch {}
      }
    }
    while (now() < deadline) {}
  };

  const wasi = {
    args_get: (p, b) => putStrings(args, p, b),
    args_sizes_get: (pn, pb) => sizes(args, pn, pb),
    environ_get: (p, b) => putStrings(env, p, b),
    environ_sizes_get: (pn, pb) => sizes(env, pn, pb),
    clock_res_get: (id, p) => { view().setBigUint64(p, 1000n, true); return ESUCCESS; },
    clock_time_get: (id, prec, p) => { view().setBigUint64(p, id === 0 ? realNow() : now(), true); return ESUCCESS; },
    fd_write: (fd, iovs, n, pw) => {
      if (fd !== 1 && fd !== 2) return EBADF;
      const v = view(), m = u8();
      let total = 0, text = '';
      for (let i = 0; i < n; i++) {
        const ptr = v.getUint32(iovs + 8 * i, true), len = v.getUint32(iovs + 8 * i + 4, true);
        text += dec[fd - 1].decode(m.subarray(ptr, ptr + len), { stream: true });
        total += len;
      }
      if (text) out[fd](text);
      v.setUint32(pw, total, true);
      return ESUCCESS;
    },
    fd_read: (fd, iovs, n, pr) => { if (fd !== 0) return EBADF; view().setUint32(pr, 0, true); return ESUCCESS; },
    fd_close: () => ESUCCESS,
    fd_sync: () => ESUCCESS,
    fd_seek: () => ESUCCESS,
    fd_fdstat_get: (fd, p) => {
      if (fd > 2) return EBADF;
      const v = view();
      v.setUint8(p, fd === 0 ? 2 : 2); // character device
      v.setUint16(p + 2, 0, true);
      v.setBigUint64(p + 8, 0xffffffffffffffffn, true);
      v.setBigUint64(p + 16, 0xffffffffffffffffn, true);
      return ESUCCESS;
    },
    fd_fdstat_set_flags: () => ESUCCESS,
    fd_prestat_get: () => EBADF, // no preopened directories: no files
    fd_prestat_dir_name: () => EBADF,
    fd_filestat_get: (fd) => (fd > 2 ? EBADF : ENOSYS),
    path_open: () => ENOSYS,
    path_filestat_get: () => ENOSYS,
    random_get: (p, n) => { crypto.getRandomValues(u8().subarray(p, p + n)); return ESUCCESS; },
    sched_yield: () => ESUCCESS,
    proc_exit: (code) => { throw new Exit(code); },
    proc_raise: () => ENOSYS,
    sock_accept: () => ENOSYS, sock_recv: () => ENOSYS, sock_send: () => ENOSYS, sock_shutdown: () => ENOSYS,
    // poll_oneoff: clock subscriptions sleep; anything on fds is ready at once.
    poll_oneoff: (inp, outp, nsubs, pn) => {
      const v = view();
      let deadline = null;
      for (let i = 0; i < nsubs; i++) {
        const s = inp + 48 * i;
        if (v.getUint8(s + 8) === 0) { // clock
          const t = v.getBigUint64(s + 24, true), abs = v.getUint16(s + 40, true) & 1;
          const d = abs ? t - (v.getUint32(s + 16, true) === 0 ? realNow() - now() : 0n) : now() + t;
          if (deadline === null || d < deadline) deadline = d;
        }
      }
      if (deadline !== null) sleepUntil(deadline);
      for (let i = 0; i < nsubs; i++) {
        const s = inp + 48 * i, e = outp + 32 * i;
        v.setBigUint64(e, v.getBigUint64(s, true), true);
        v.setUint16(e + 8, 0, true);
        v.setUint8(e + 10, v.getUint8(s + 8));
      }
      v.setUint32(pn, nsubs, true);
      return ESUCCESS;
    },
  };
  const imports = { wasi_snapshot_preview1: new Proxy(wasi, {
    get: (t, k) => t[k] || (() => ENOSYS), // a call not listed: not supported
  }) };
  const { instance } = await WebAssembly.instantiate(bytes, imports);
  memory = instance.exports.memory;
  try {
    instance.exports._start();
    return 0;
  } catch (e) {
    if (e instanceof Exit) return e.code;
    throw e;
  }
}
