// An in-memory file system for wasi.js: directories and files in a tree, loaded from a tar
// (the playground's GOROOT and caches) and writable (temp dirs, outputs, caches). Times are
// kept as given by the tar, so a program that keys caches on size and modification time
// sees the same values it saw when the bundle was made.
//
//   const fs = new MemFS();
//   fs.loadTar(bytes, '/');            // ustar entries under the given directory
//   fs.writeFile('/play/main.go', s);  // string or Uint8Array
//   runWasi(wasm, { fs, preopens: ['/'] , ... })

const enc = new TextEncoder();

export class Node {
  constructor(dir, mtime) {
    this.dir = dir;
    this.kids = dir ? new Map() : null;
    this.data = dir ? null : new Uint8Array(0);
    this.size = 0;
    this.mtime = mtime ?? BigInt(Date.now()) * 1000000n; // nanoseconds
    this.ino = Node.next++;
  }
}
Node.next = 1n;

export class MemFS {
  constructor() {
    this.root = new Node(true);
  }

  // lookup walks a path from dir (a Node); null when a part is missing.
  lookup(path, dir = this.root) {
    let n = path.startsWith('/') ? this.root : dir;
    for (const part of path.split('/')) {
      if (part === '' || part === '.') continue;
      if (part === '..') {
        n = n.parent ?? n;
        continue;
      }
      if (!n.dir) return null;
      const k = n.kids.get(part);
      if (!k) return null;
      n = k;
    }
    return n;
  }

  // parentOf returns [directory Node, last name] for a path, or [null, ''].
  parentOf(path, dir = this.root) {
    const parts = path.split('/').filter((p) => p !== '' && p !== '.');
    const name = parts.pop() ?? '';
    const base = path.startsWith('/') ? '/' : '';
    const d = this.lookup(base + parts.join('/'), path.startsWith('/') ? this.root : dir);
    return d && d.dir ? [d, name] : [null, ''];
  }

  mkdirAll(path, mtime) {
    let n = this.root;
    for (const part of path.split('/')) {
      if (part === '' || part === '.') continue;
      let k = n.kids.get(part);
      if (!k) {
        k = new Node(true, mtime);
        k.parent = n;
        n.kids.set(part, k);
      }
      n = k;
    }
    return n;
  }

  writeFile(path, data, mtime) {
    const parts = path.split('/').filter((p) => p !== '' && p !== '.');
    const name = parts.pop();
    const dir = this.mkdirAll(parts.join('/'));
    const f = dir.kids.get(name) ?? new Node(false, mtime);
    f.parent = dir;
    const b = typeof data === 'string' ? enc.encode(data) : data;
    f.data = b;
    f.size = b.length;
    if (mtime !== undefined) f.mtime = mtime;
    dir.kids.set(name, f);
    return f;
  }

  readFile(path) {
    const n = this.lookup(path);
    return n && !n.dir ? n.data.subarray(0, n.size) : null;
  }

  // loadTar adds a ustar archive's directories and regular files under base. Symbolic and
  // hard links are not supported (make the tar with them dereferenced).
  loadTar(bytes, base = '/') {
    const b = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes);
    const str = (o, n) => {
      let e = o;
      while (e < o + n && b[e] !== 0) e++;
      return new TextDecoder().decode(b.subarray(o, e));
    };
    const oct = (o, n) => parseInt(str(o, n).trim() || '0', 8);
    let longName = null;
    for (let off = 0; off + 512 <= b.length; ) {
      if (b[off] === 0) break; // the end: two zero blocks
      let name = longName ?? str(off, 100);
      longName = null;
      const prefix = str(off + 345, 155);
      if (prefix && b[off + 257] === 0x75) name = `${prefix}/${name}`; // "ustar"
      const size = oct(off + 124, 12);
      const mtime = BigInt(oct(off + 136, 12)) * 1000000000n;
      const type = String.fromCharCode(b[off + 156] || 0x30);
      const body = off + 512;
      const path = `${base.replace(/\/$/, '')}/${name.replace(/^\.\//, '')}`;
      if (type === 'L') {
        longName = new TextDecoder().decode(b.subarray(body, body + size)).replace(/\0+$/, '');
      } else if (type === '5') {
        this.mkdirAll(path, mtime);
      } else if (type === '0' || type === '\0' || type === '7') {
        this.writeFile(path, b.slice(body, body + size), mtime);
      } else if (type === 'x' || type === 'g') {
        // pax headers: path= and mtime= override the next entry
        const text = new TextDecoder().decode(b.subarray(body, body + size));
        for (const line of text.split('\n')) {
          const m = line.match(/^\d+ path=(.*)$/);
          if (m && type === 'x') longName = m[1];
        }
      }
      off = body + Math.ceil(size / 512) * 512;
    }
  }
}

// wasiFiles returns the WASI preview 1 file calls over fs, with the directories of preopens
// (paths in fs) as fds 3, 4, ...; std holds the calls for fds 0 to 2 (stdin, stdout,
// stderr), which keep working as before. mem() returns the instance's memory.
export function wasiFiles(fs, preopens, std, mem) {
  const E = { OK: 0, BADF: 8, EXIST: 20, INVAL: 28, ISDIR: 31, NOENT: 44, NOTDIR: 54, NOTEMPTY: 55, SPIPE: 70, NOSYS: 52 };
  const view = () => new DataView(mem().buffer);
  const u8 = () => new Uint8Array(mem().buffer);
  const dec = new TextDecoder();
  const fds = new Map(); // fd -> { node, pos, append, pre (preopen path) }
  let nextFd = 3;
  for (const p of preopens) fds.set(nextFd++, { node: fs.mkdirAll(p), pos: 0, pre: p });
  const str = (p, n) => dec.decode(u8().subarray(p, p + n));
  const iovs = (p, n, f) => {
    const v = view();
    let total = 0;
    for (let i = 0; i < n; i++) {
      const ptr = v.getUint32(p + 8 * i, true), len = v.getUint32(p + 8 * i + 4, true);
      const done = f(ptr, len);
      total += done;
      if (done < len) break;
    }
    return total;
  };
  const stat = (n, p) => {
    const v = view();
    v.setBigUint64(p, 1n, true); // dev
    v.setBigUint64(p + 8, n.ino, true);
    v.setUint8(p + 16, n.dir ? 3 : 4); // directory, regular file
    v.setBigUint64(p + 24, 1n, true); // nlink
    v.setBigUint64(p + 32, BigInt(n.dir ? 4096 : n.size), true);
    v.setBigUint64(p + 40, n.mtime, true);
    v.setBigUint64(p + 48, n.mtime, true);
    v.setBigUint64(p + 56, n.mtime, true);
  };
  const grow = (n, size) => {
    if (n.data.length < size) {
      const d = new Uint8Array(Math.max(size, n.data.length * 2, 4096));
      d.set(n.data.subarray(0, n.size));
      n.data = d;
    }
  };
  const write = (f, at, ptr, len) => {
    const n = f.node;
    grow(n, at + len);
    n.data.set(u8().subarray(ptr, ptr + len), at);
    if (at + len > n.size) n.size = at + len;
    n.mtime = BigInt(Date.now()) * 1000000n;
    return len;
  };
  const read = (f, at, ptr, len) => {
    const n = f.node;
    const k = Math.max(0, Math.min(len, n.size - at));
    u8().set(n.data.subarray(at, at + k), ptr);
    return k;
  };
  const at = (fd, path, plen) => {
    const d = fds.get(fd);
    if (!d || !d.node.dir) return [null, null, ''];
    return [d.node, fs.lookup(str(path, plen), d.node), str(path, plen)];
  };
  return {
    fd_prestat_get: (fd, p) => {
      const f = fds.get(fd);
      if (!f || f.pre === undefined) return E.BADF;
      view().setUint8(p, 0);
      view().setUint32(p + 4, new TextEncoder().encode(f.pre).length, true);
      return E.OK;
    },
    fd_prestat_dir_name: (fd, p, n) => {
      const f = fds.get(fd);
      if (!f || f.pre === undefined) return E.BADF;
      u8().set(new TextEncoder().encode(f.pre).subarray(0, n), p);
      return E.OK;
    },
    path_open: (dirfd, dirflags, path, plen, oflags, rb, ri, fdflags, pfd) => {
      const [dir, found, name] = at(dirfd, path, plen);
      if (!dir) return E.BADF;
      let n = found;
      if (n && oflags & 4 && oflags & 1) return E.EXIST; // EXCL
      if (!n) {
        if (!(oflags & 1)) return E.NOENT; // no CREAT
        const [d] = fs.parentOf(name, dir);
        if (!d) return E.NOENT;
        n = new Node(false);
        n.parent = d;
        d.kids.set(name.split('/').filter(Boolean).pop(), n);
      }
      if (oflags & 2 && !n.dir) return E.NOTDIR;
      if (oflags & 8 && !n.dir) { n.size = 0; n.data = new Uint8Array(0); } // TRUNC
      const fd = nextFd++;
      fds.set(fd, { node: n, pos: 0, append: !!(fdflags & 1) });
      view().setUint32(pfd, fd, true);
      return E.OK;
    },
    fd_close: (fd) => (fd <= 2 ? E.OK : fds.delete(fd) ? E.OK : E.BADF),
    fd_read: (fd, p, n, pn) => {
      if (fd <= 2) return std.fd_read(fd, p, n, pn);
      const f = fds.get(fd);
      if (!f) return E.BADF;
      if (f.node.dir) return E.ISDIR;
      const got = iovs(p, n, (ptr, len) => { const k = read(f, f.pos, ptr, len); f.pos += k; return k; });
      view().setUint32(pn, got, true);
      return E.OK;
    },
    fd_pread: (fd, p, n, off, pn) => {
      const f = fds.get(fd);
      if (!f) return E.BADF;
      let o = Number(off);
      const got = iovs(p, n, (ptr, len) => { const k = read(f, o, ptr, len); o += k; return k; });
      view().setUint32(pn, got, true);
      return E.OK;
    },
    fd_write: (fd, p, n, pn) => {
      if (fd <= 2) return std.fd_write(fd, p, n, pn);
      const f = fds.get(fd);
      if (!f) return E.BADF;
      if (f.append) f.pos = f.node.size;
      const put = iovs(p, n, (ptr, len) => { const k = write(f, f.pos, ptr, len); f.pos += k; return k; });
      view().setUint32(pn, put, true);
      return E.OK;
    },
    fd_pwrite: (fd, p, n, off, pn) => {
      const f = fds.get(fd);
      if (!f) return E.BADF;
      let o = Number(off);
      const put = iovs(p, n, (ptr, len) => { const k = write(f, o, ptr, len); o += k; return k; });
      view().setUint32(pn, put, true);
      return E.OK;
    },
    fd_seek: (fd, off, whence, pn) => {
      if (fd <= 2) return E.SPIPE;
      const f = fds.get(fd);
      if (!f) return E.BADF;
      const base = whence === 0 ? 0 : whence === 1 ? f.pos : f.node.size;
      const pos = base + Number(off);
      if (pos < 0) return E.INVAL;
      f.pos = pos;
      view().setBigUint64(pn, BigInt(pos), true);
      return E.OK;
    },
    fd_tell: (fd, pn) => {
      const f = fds.get(fd);
      if (!f) return E.BADF;
      view().setBigUint64(pn, BigInt(f.pos), true);
      return E.OK;
    },
    fd_fdstat_get: (fd, p) => {
      if (fd <= 2) return std.fd_fdstat_get(fd, p);
      const f = fds.get(fd);
      if (!f) return E.BADF;
      const v = view();
      v.setUint8(p, f.node.dir ? 3 : 4);
      v.setUint16(p + 2, f.append ? 1 : 0, true);
      v.setBigUint64(p + 8, 0xffffffffffffffffn, true);
      v.setBigUint64(p + 16, 0xffffffffffffffffn, true);
      return E.OK;
    },
    fd_fdstat_set_flags: () => E.OK,
    fd_filestat_get: (fd, p) => {
      if (fd <= 2) {
        const v = view();
        for (let i = 0; i < 64; i += 8) v.setBigUint64(p + i, 0n, true);
        v.setUint8(p + 16, 2); // character device
        return E.OK;
      }
      const f = fds.get(fd);
      if (!f) return E.BADF;
      stat(f.node, p);
      return E.OK;
    },
    fd_filestat_set_size: (fd, size) => {
      const f = fds.get(fd);
      if (!f || f.node.dir) return E.BADF;
      const s = Number(size);
      grow(f.node, s);
      if (s > f.node.size) f.node.data.fill(0, f.node.size, s);
      f.node.size = s;
      return E.OK;
    },
    fd_filestat_set_times: () => E.OK,
    path_filestat_set_times: () => E.OK,
    fd_sync: () => E.OK,
    fd_datasync: () => E.OK,
    fd_advise: () => E.OK,
    fd_allocate: () => E.OK,
    path_filestat_get: (fd, flags, path, plen, p) => {
      const [dir, n] = at(fd, path, plen);
      if (!dir) return E.BADF;
      if (!n) return E.NOENT;
      stat(n, p);
      return E.OK;
    },
    fd_readdir: (fd, buf, blen, cookie, pused) => {
      const f = fds.get(fd);
      if (!f) return E.BADF;
      if (!f.node.dir) return E.NOTDIR;
      const names = ['.', '..', ...[...f.node.kids.keys()].sort()];
      const v = view(), m = u8();
      let used = 0;
      for (let i = Number(cookie); i < names.length && used < blen; i++) {
        const name = new TextEncoder().encode(names[i]);
        const k = i < 2 ? f.node : f.node.kids.get(names[i]);
        const ent = new Uint8Array(24 + name.length);
        const dv = new DataView(ent.buffer);
        dv.setBigUint64(0, BigInt(i + 1), true); // d_next
        dv.setBigUint64(8, k.ino, true);
        dv.setUint32(16, name.length, true);
        dv.setUint8(20, k.dir ? 3 : 4);
        ent.set(name, 24);
        const room = Math.min(ent.length, blen - used);
        m.set(ent.subarray(0, room), buf + used);
        used += room;
      }
      v.setUint32(pused, used, true);
      return E.OK;
    },
    path_create_directory: (fd, path, plen) => {
      const [dir, n, name] = at(fd, path, plen);
      if (!dir) return E.BADF;
      if (n) return E.EXIST;
      const [d, last] = fs.parentOf(name, dir);
      if (!d) return E.NOENT;
      const k = new Node(true);
      k.parent = d;
      d.kids.set(last, k);
      return E.OK;
    },
    path_remove_directory: (fd, path, plen) => {
      const [dir, n, name] = at(fd, path, plen);
      if (!dir) return E.BADF;
      if (!n) return E.NOENT;
      if (!n.dir) return E.NOTDIR;
      if (n.kids.size) return E.NOTEMPTY;
      const [d, last] = fs.parentOf(name, dir);
      d.kids.delete(last);
      return E.OK;
    },
    path_unlink_file: (fd, path, plen) => {
      const [dir, n, name] = at(fd, path, plen);
      if (!dir) return E.BADF;
      if (!n) return E.NOENT;
      if (n.dir) return E.ISDIR;
      const [d, last] = fs.parentOf(name, dir);
      d.kids.delete(last);
      return E.OK;
    },
    path_rename: (fd, op, ol, nfd, np, nl) => {
      const [odir, n, oname] = at(fd, op, ol);
      const [ndir, , nname] = at(nfd, np, nl);
      if (!odir || !ndir) return E.BADF;
      if (!n) return E.NOENT;
      const [od, olast] = fs.parentOf(oname, odir);
      const [nd, nlast] = fs.parentOf(nname, ndir);
      if (!nd) return E.NOENT;
      const there = nd.kids.get(nlast);
      if (there && there.dir && there.kids.size) return E.NOTEMPTY;
      od.kids.delete(olast);
      n.parent = nd;
      nd.kids.set(nlast, n);
      return E.OK;
    },
    path_readlink: () => E.INVAL, // nothing here is a symbolic link
    path_symlink: () => E.NOSYS,
    path_link: () => E.NOSYS,
  };
}
