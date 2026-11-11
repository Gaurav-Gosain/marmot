#!/usr/bin/env python3
"""Minimal lit-style runner for backend tests.

Each test file holds one or more `// RUN: <shell command>` lines (a trailing
backslash continues a RUN line). Substitutions:
  %s               the test file
  %t               a per-test scratch path (under .work/backend-test)
  %S               the directory of the test file
  %backend         build/bin/marmot-backend
  %clang, %opt, %FileCheck, %not, %mlir-translate, %llvm-dis
                   tools from the LLVM install
A test passes when every RUN line exits with status 0. `// UNSUPPORTED: <arch>`
skips a test on that host machine (uname -m).
"""
import os, platform, re, signal, subprocess, sys, concurrent.futures

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
LLVM = os.environ.get("LLVM_PREFIX", "/opt/homebrew/opt/llvm@22")
BIN = os.path.join(LLVM, "bin")
WORK = os.path.join(ROOT, ".work", "backend-test")
TIMEOUT = 120  # seconds per RUN line (SPEC rule 10)
JOBS = 4

SUBST = [
    ("%backend", os.path.join(ROOT, "build", "bin", "marmot-backend")),
    ("%marmot-opt", os.path.join(ROOT, "build", "bin", "marmot-opt")),
    ("%clang", os.path.join(BIN, "clang")),
    ("%opt", os.path.join(BIN, "opt")),
    ("%FileCheck", os.path.join(BIN, "FileCheck")),
    ("%not", os.path.join(BIN, "not")),
    ("%mlir-translate", os.path.join(BIN, "mlir-translate")),
    ("%llvm-dis", os.path.join(BIN, "llvm-dis")),
    ("%llvm-nm", os.path.join(BIN, "llvm-nm")),
    ("%mlir-opt", os.path.join(BIN, "mlir-opt")),
    ("%llvm-readobj", os.path.join(BIN, "llvm-readobj")),
    ("%llvm-profdata", os.path.join(BIN, "llvm-profdata")),
]


def run_lines(path):
    lines, cur = [], None
    for raw in open(path):
        m = re.search(r"//\s*RUN:\s?(.*)$", raw)
        if not m:
            continue
        text = m.group(1).rstrip()
        cur = text if cur is None else cur + " " + text
        if cur.endswith("\\"):
            cur = cur[:-1]
            continue
        lines.append(cur)
        cur = None
    return lines


def run_test(path):
    src = open(path).read()
    m = re.search(r"//\s*UNSUPPORTED:\s*(.*)", src)
    if m and platform.machine() in [a.strip() for a in m.group(1).split(",")]:
        return path, "SKIP", ""
    rel = os.path.relpath(path, os.path.join(ROOT, "backend", "test"))
    tmp = os.path.join(WORK, rel)
    os.makedirs(os.path.dirname(tmp), exist_ok=True)
    cmds = run_lines(path)
    if not cmds:
        return path, "FAIL", "no RUN lines"
    log = []
    for cmd in cmds:
        for k, v in SUBST:
            cmd = cmd.replace(k, v)
        cmd = cmd.replace("%s", path).replace("%S", os.path.dirname(path)).replace("%t", tmp)
        # Own process group, killed as a whole on timeout.
        p = subprocess.Popen(cmd, shell=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                             text=True, cwd=ROOT, start_new_session=True)
        try:
            out, err = p.communicate(timeout=TIMEOUT)
        except subprocess.TimeoutExpired:
            os.killpg(p.pid, signal.SIGKILL)
            p.communicate()
            log.append("$ " + cmd + f"\nTIMEOUT after {TIMEOUT}s")
            return path, "FAIL", "\n".join(log)
        log.append("$ " + cmd + "\n" + out + err)
        if p.returncode != 0:
            return path, "FAIL", "\n".join(log)
    return path, "PASS", ""


def main():
    args = [a for a in sys.argv[1:] if not a.startswith("-")]
    verbose = "-v" in sys.argv
    tests = []
    roots = args or [os.path.join(ROOT, "backend", "test")]
    for r in roots:
        if os.path.isfile(r):
            tests.append(os.path.abspath(r))
            continue
        for d, dirs, files in os.walk(r):
            dirs[:] = [x for x in dirs if x != "Inputs"]
            tests += [os.path.join(d, f) for f in files if f.endswith((".mlir", ".ll"))]
    tests.sort()
    fails = 0
    with concurrent.futures.ThreadPoolExecutor(max_workers=JOBS) as ex:
        for path, status, log in ex.map(run_test, tests):
            rel = os.path.relpath(path, ROOT)
            if status == "FAIL":
                fails += 1
                print(f"FAIL {rel}\n{log}")
            elif verbose or status == "SKIP":
                print(f"{status} {rel}")
    print(f"{len(tests) - fails}/{len(tests)} passed")
    return 1 if fails else 0


if __name__ == "__main__":
    sys.exit(main())
