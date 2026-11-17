#!/usr/bin/env python3
# env_switches.py ROOT BACKEND TMP: every environment variable read in
# backend/lib, backend/tools and backend/tier0 is in exactly one class of
# backend/include/marmot/Driver/EnvSwitches.def, every listed name is still
# read, fingerprint() takes its list from the file, and setting each KEY name
# makes a warm cache miss.
import os, re, subprocess, sys

root, backend, tmp = sys.argv[1:4]
src = os.path.join(root, "backend")
deff = os.path.join(src, "include/marmot/Driver/EnvSwitches.def")
classes = {}
bad = []
for line in open(deff):
    m = re.match(r"MARMOT_ENV_(KEY|OWN|NOKEY)\((\w+)\)", line)
    if m:
        if m.group(2) in classes:
            bad.append("listed twice: " + m.group(2))
        classes[m.group(2)] = m.group(1)

read = {}
pat = [re.compile(r'getenv\(\s*"([A-Za-z0-9_]+)"'), re.compile(r'envOn\(\s*"([A-Za-z0-9_]+)"'),
       re.compile(r'"(MARMOT_[A-Z0-9_]+)"')]
for d in ("lib", "tools", "tier0"):
    for dp, _, fs in os.walk(os.path.join(src, d)):
        for f in fs:
            if not f.endswith((".cpp", ".h", ".c")):
                continue
            p = os.path.join(dp, f)
            for n, line in enumerate(open(p, errors="replace"), 1):
                code = line.split("//")[0] if line.lstrip().startswith("//") else line
                for r in pat:
                    for name in r.findall(code):
                        read.setdefault(name, "%s:%d" % (os.path.relpath(p, root), n))
for name, where in sorted(read.items()):
    if name not in classes:
        bad.append("not in EnvSwitches.def: %s (%s)" % (name, where))
for name in sorted(classes):
    if name not in read:
        bad.append("listed but not read: " + name)
wp = open(os.path.join(src, "lib/Driver/WholeProgram.cpp")).read()
if '#include "marmot/Driver/EnvSwitches.def"' not in wp:
    bad.append("fingerprint() does not include EnvSwitches.def")

mod = os.path.join(tmp + ".d", "a.mlir")
os.makedirs(os.path.dirname(mod), exist_ok=True)
open(mod, "w").write("module {\n  llvm.func @f(%x: i64) -> i64 {\n    llvm.return %x : i64\n  }\n}\n")
jobs = tmp + ".jobs"
open(jobs, "w").write("%s %s.a.o\n" % (mod, tmp))
cache = tmp + ".cache"
subprocess.run(["rm", "-rf", cache])
def compiled(env):
    e = dict(os.environ)
    for k in classes:
        e.pop(k, None)
    e.update(env)
    out = subprocess.run([backend, "-O2", "-batch", jobs, "-cache-dir", cache, "-time"], env=e,
                         stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True).stdout
    m = re.search(r"compiled (\d+) of", out)
    return int(m.group(1)) if m else -1
compiled({})
if compiled({}) != 0:
    bad.append("a warm cache misses with no switch set")
for name in sorted(k for k, c in classes.items() if c == "KEY"):
    if compiled({name: "1"}) != 1:
        bad.append("setting %s does not change the object key" % name)
for b in bad:
    print("env-switches: " + b)
print("env-switches: %d names, %d problems" % (len(read), len(bad)))
sys.exit(1 if bad else 0)
