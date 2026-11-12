# Random signature ABI test: abi_gen.py seed n writes a.mlir (functions t_*),
# b.mlir (l_*) and main.c. t_callerN calls l_calleeN and l_callerN calls t_calleeN.
import random, sys
seed = int(sys.argv[1]); N = int(sys.argv[2])
R = random.Random(seed)
SCAL = ['i1','i8','i16','i32','i64','!llvm.ptr','f32','f64']
def rtype(d=0):
    x = R.random()
    if d < 2 and x < 0.2:
        return '!llvm.struct<(' + ', '.join(rtype(d+1) for _ in range(R.randint(1,4))) + ')>'
    if d < 2 and x < 0.32:
        e = R.choice(['i64','f64','f32','!llvm.ptr','i32', rtype(d+1)])
        return f'!llvm.array<{R.randint(1,5)} x {e}>'
    return R.choice(SCAL)
def split_top(s):  # split comma list at depth 0
    out, d, cur = [], 0, ''
    for c in s:
        if c in '(<': d += 1
        if c in ')>': d -= 1
        if c == ',' and d == 0: out.append(cur.strip()); cur = ''
        else: cur += c
    if cur.strip(): out.append(cur.strip())
    return out
def pieces(t, path=()):
    if t.startswith('!llvm.struct<('):
        r = []
        for i, f in enumerate(split_top(t[len('!llvm.struct<('):-2])): r += pieces(f, path + (i,))
        return r
    if t.startswith('!llvm.array<'):
        n, e = t[len('!llvm.array<'):-1].split(' x ', 1)
        r = []
        for i in range(int(n)): r += pieces(e, path + (i,))
        return r
    return [(path, t)]
class F:
    def __init__(s): s.l = []; s.n = 0
    def v(s, txt):
        s.n += 1; s.l.append(f'    %v{s.n} = {txt}'); return f'%v{s.n}'
def to_i64(f, x, t):
    if t == '!llvm.ptr': return f.v(f'llvm.ptrtoint {x} : !llvm.ptr to i64')
    if t == 'f64': return f.v(f'llvm.bitcast {x} : f64 to i64')
    if t == 'f32':
        y = f.v(f'llvm.bitcast {x} : f32 to i32'); return f.v(f'llvm.zext {y} : i32 to i64')
    if t == 'i64': return x
    return f.v(f'llvm.zext {x} : {t} to i64')
def from_i64(f, h, t):
    if t == '!llvm.ptr': return f.v(f'llvm.inttoptr {h} : i64 to !llvm.ptr')
    if t == 'f64':
        m = f.v('llvm.mlir.constant(4503599627370495 : i64) : i64'); y = f.v(f'llvm.and {h}, {m} : i64')
        return f.v(f'llvm.bitcast {y} : i64 to f64')
    if t == 'f32':
        y = f.v(f'llvm.trunc {h} : i64 to i32'); m = f.v('llvm.mlir.constant(8388607 : i32) : i32')
        z = f.v(f'llvm.and {y}, {m} : i32'); return f.v(f'llvm.bitcast {z} : i32 to f32')
    if t == 'i64': return h
    return f.v(f'llvm.trunc {h} : i64 to {t}')
def hash_pieces(f, h, vals):
    for x, t in vals:
        y = to_i64(f, x, t)
        k = f.v('llvm.mlir.constant(1000003 : i64) : i64')
        h = f.v(f'llvm.mul {h}, {k} : i64'); h = f.v(f'llvm.xor {h}, {y} : i64')
    return h
def extract(f, x, t):
    if t.startswith('!llvm.struct') or t.startswith('!llvm.array'):
        return [(f.v(f'llvm.extractvalue {x}[{", ".join(map(str, p))}] : {t}'), pt) for p, pt in pieces(t)]
    return [(x, t)]
def build(f, t, mk):  # mk(piece type) -> value
    if t.startswith('!llvm.struct') or t.startswith('!llvm.array'):
        acc = f.v(f'llvm.mlir.undef : {t}')
        for p, pt in pieces(t):
            acc = f.v(f'llvm.insertvalue {mk(pt)}, {acc}[{", ".join(map(str, p))}] : {t}')
        return acc
    return mk(t)
sigs = []
for i in range(N):
    args = [rtype() for _ in range(R.randint(0, 14))]
    ret = R.choice(['i64', rtype(), rtype(), '!llvm.struct<(' + ', '.join(R.choice(SCAL) for _ in range(R.randint(5, 12))) + ')>'])
    sigs.append((args, ret))
def module(me, other):
    out = ['module {']
    for i, (args, ret) in enumerate(sigs):
        ps = ', '.join(f'%a{j}: {t}' for j, t in enumerate(args))
        out.append(f'  llvm.func @{other}_callee{i}({", ".join(args)}) -> {ret}')
        f = F()
        h = f.v('llvm.mlir.constant(7 : i64) : i64')
        vals = []
        for j, t in enumerate(args): vals += extract(f, f'%a{j}', t)
        h = hash_pieces(f, h, vals)
        cnt = [0]
        def mk(pt):
            cnt[0] += 1
            k = f.v(f'llvm.mlir.constant({cnt[0] * 7919} : i64) : i64')
            return from_i64(f, f.v(f'llvm.add {h}, {k} : i64'), pt)
        r = build(f, ret, mk)
        out.append(f'  llvm.func @{me}_callee{i}({ps}) -> {ret} {{'); out += f.l
        out.append(f'    llvm.return {r} : {ret}'); out.append('  }')
        f = F()
        def mkc(pt):
            c = R.getrandbits(63)
            return from_i64(f, f.v(f'llvm.mlir.constant({c} : i64) : i64'), pt)
        avs = [build(f, t, mkc) for t in args]
        r = f.v(f'llvm.call @{other}_callee{i}({", ".join(avs)}) : ({", ".join(args)}) -> {ret}')
        h0 = f.v('llvm.mlir.constant(11 : i64) : i64')
        h = hash_pieces(f, h0, extract(f, r, ret))
        out.append(f'  llvm.func @{me}_caller{i}() -> i64 {{'); out += f.l
        out.append(f'    llvm.return {h} : i64'); out.append('  }')
    out.append('}')
    return '\n'.join(out)
state = R.getstate()
open('a.mlir', 'w').write(module('t', 'l'))
R.setstate(state)
open('b.mlir', 'w').write(module('l', 't'))
with open('main.c', 'w') as m:
    m.write('#include <stdio.h>\n#include <stdint.h>\n')
    for i in range(N): m.write(f'uint64_t t_caller{i}(void); uint64_t l_caller{i}(void);\n')
    m.write('int main(void){\n')
    for i in range(N): m.write(f'  printf("%d %llx %llx\\n", {i}, (unsigned long long)t_caller{i}(), (unsigned long long)l_caller{i}());\n')
    m.write('  return 0; }\n')
