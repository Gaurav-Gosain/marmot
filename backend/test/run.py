#!/usr/bin/env python3
"""Minimal lit-style runner for backend tests.

Each test file holds one or more `// RUN: <shell command>` lines (a trailing
backslash continues a RUN line). Substitutions:
  %s               the test file
  %t               a per-test scratch path (under build/backend-test/work)
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
WORK = os.path.join(ROOT, "build", "backend-test", "work")
# The tools under test: the private build of backend/test.sh, or build/bin.
TOOLS = os.environ.get("BACKEND_BIN_DIR") or os.path.join(ROOT, "build", "backend-test", "bin")
if not os.path.exists(os.path.join(TOOLS, "marmot-backend")):
    TOOLS = os.path.join(ROOT, "build", "bin")
TOOLS = os.path.abspath(TOOLS)


def marmot_root():
    """A marmot root whose build/bin has the backend tools under test, so a
    private backend build is tested through the installed marmot driver
    before it is installed. Everything else links to ROOT."""
    if TOOLS == os.path.join(ROOT, "build", "bin"):
        return ROOT
    r = os.path.join(ROOT, "build", "backend-test", "root")
    os.makedirs(os.path.join(r, "build", "bin"), exist_ok=True)
    for name in ("lib", "internal", "go.mod"):
        link = os.path.join(r, name)
        if not os.path.lexists(link):
            os.symlink(os.path.join(ROOT, name), link)
    link = os.path.join(r, "build", "lib")
    if not os.path.lexists(link):
        os.symlink(os.path.join(ROOT, "build", "lib"), link)
    for tool, src in (("marmot", os.path.join(ROOT, "build", "bin", "marmot")),
                      ("marmot-backend", os.path.join(TOOLS, "marmot-backend")),
                      ("marmot-pcln", os.path.join(TOOLS, "marmot-pcln")),
                      ("marmot-opt", os.path.join(TOOLS, "marmot-opt"))):
        link = os.path.join(r, "build", "bin", tool)
        if os.path.lexists(link):
            os.remove(link)
        os.symlink(src, link)
    return r


MARMOT_ROOT_DIR = marmot_root()
TIMEOUT = 300  # seconds per RUN line (marmot build -nocache waits for build slots under load)
JOBS = 4

def llvm_tool(name):
    """An LLVM tool: from the install, else (for the test utilities an
    install does not copy, FileCheck and not) from the build directory
    beside it or LLVM_UTILS, else from PATH. None when there is none."""
    import shutil
    for d in (BIN, os.path.join(LLVM, "..", "build", "bin"), os.environ.get("LLVM_UTILS", "")):
        p = os.path.join(d, name) if d else ""
        if p and os.path.exists(p):
            return os.path.abspath(p)
    return shutil.which(name)


# Tools a test needs that this machine lacks: the tests that use them are
# skipped with the tool named, not failed.
MISSING = {}


def tool_path(sub, name):
    p = llvm_tool(name)
    if p is None:
        MISSING[sub] = name
        return os.path.join(BIN, name)
    return p


SUBST = [
    ("%backend", os.path.join(TOOLS, "marmot-backend")),
    ("%marmot-opt", os.path.join(TOOLS, "marmot-opt")),
    # marmot through the backend under test (after %marmot-opt, which it
    # prefixes); use it instead of a path to build/bin/marmot.
    ("%marmot", "env MARMOT_ROOT=" + MARMOT_ROOT_DIR + " " +
     os.path.join(MARMOT_ROOT_DIR, "build", "bin", "marmot")),
    ("%clang", tool_path("%clang", "clang")),
    ("%opt", tool_path("%opt", "opt")),
    ("%FileCheck", tool_path("%FileCheck", "FileCheck")),
    ("%not", tool_path("%not", "not")),
    ("%mlir-translate", tool_path("%mlir-translate", "mlir-translate")),
    ("%llvm-dis", tool_path("%llvm-dis", "llvm-dis")),
    ("%llvm-nm", tool_path("%llvm-nm", "llvm-nm")),
    ("%mlir-opt", tool_path("%mlir-opt", "mlir-opt")),
    ("%llvm-readobj", tool_path("%llvm-readobj", "llvm-readobj")),
    ("%llvm-profdata", tool_path("%llvm-profdata", "llvm-profdata")),
    ("%llvm-dwarfdump", tool_path("%llvm-dwarfdump", "llvm-dwarfdump")),
    ("%llvm-objcopy", tool_path("%llvm-objcopy", "llvm-objcopy")),
    ("%dsymutil", tool_path("%dsymutil", "dsymutil")),
    ("%pcln", os.path.join(TOOLS, "marmot-pcln")),
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


_wasm = None


def have_wasm():
    global _wasm
    if _wasm is None:
        try:
            out = subprocess.run([os.path.join(BIN, "llvm-config"), "--targets-built"],
                                 capture_output=True, text=True).stdout
            _wasm = "WebAssembly" in out.split()
        except OSError:
            _wasm = False
    return _wasm


def run_test(path):
    src = open(path).read()
    m = re.search(r"//\s*UNSUPPORTED:\s*(.*)", src)
    if m and platform.machine() in [a.strip() for a in m.group(1).split(",")]:
        return path, "SKIP", ""
    # REQUIRES: wasm skips a test when this LLVM has no WebAssembly target.
    if re.search(r"//\s*REQUIRES:\s*wasm", src) and not have_wasm():
        return path, "SKIP", ""
    need = sorted({MISSING[k] for k in MISSING if k in src})
    if need:
        return path, "SKIP", "needs " + ", ".join(need) + " (not in the LLVM install, its build directory, LLVM_UTILS or PATH)"
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
        cmd = cmd.replace("%{basename_t}", os.path.basename(tmp))
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
    fails = skips = 0
    with concurrent.futures.ThreadPoolExecutor(max_workers=JOBS) as ex:
        for path, status, log in ex.map(run_test, tests):
            rel = os.path.relpath(path, ROOT)
            if status == "FAIL":
                fails += 1
                print(f"FAIL {rel}\n{log}")
            elif status == "SKIP":
                skips += 1
                print(f"SKIP {rel}" + (f": {log}" if log else ""))
            elif verbose:
                print(f"{status} {rel}")
    print(f"{len(tests) - fails - skips}/{len(tests)} passed" + (f", {skips} skipped" if skips else ""))
    return 1 if fails else 0


if __name__ == "__main__":
    sys.exit(main())
