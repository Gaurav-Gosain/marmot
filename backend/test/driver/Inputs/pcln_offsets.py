# Prints offsets from the image base of SYMBOL + each delta in a Mach-O binary.
import subprocess, sys, os
nm = os.path.join(os.environ.get("LLVM_PREFIX", "/opt/homebrew/opt/llvm@22"), "bin", "llvm-nm")
out = subprocess.run([nm, sys.argv[1]], capture_output=True, text=True).stdout
addr = int([l for l in out.splitlines() if l.split()[-1].lstrip("_") == sys.argv[2]][0].split()[0], 16)
print(" ".join(hex(addr - 0x100000000 + int(d)) for d in sys.argv[3:]))
