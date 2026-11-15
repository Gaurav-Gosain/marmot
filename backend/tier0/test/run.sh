# Link-and-run tests of tier0 against LLVM-compiled C.
#   sh run.sh <tier0-run> <llvm bin dir> [abi seeds]
# The ABI test builds random signatures twice: tier0 and LLVM code calling
# each other, and all LLVM; the outputs must be identical.
set -e
T0=$(cd "$(dirname "$1")" && pwd)/$(basename "$1"); L=$2; SEEDS=${3:-"1 2 3 4 5"}
src=$(cd "$(dirname "$0")" && pwd)
w=$(mktemp -d); trap 'rm -rf "$w"' EXIT; cd "$w"
for t in intrinsics wide ext card; do
  "$T0" "$src/$t.mlir" $t.o > /dev/null
  "$L/clang" -O1 -w "$src/${t}_main.c" $t.o -o $t && ./$t > /dev/null && echo "$t: ok"
done
if [ "$(uname)" = Darwin ]; then sec=__DATA,__marmot_tlinks,regular,live_support; gc=-dead_strip; else sec=marmot_tlinks; gc=--gc-sections,-z,start-stop-gc; fi
sed "s/SECTION/$sec/" "$src/tlinks.mlir" > tlinks.mlir
"$T0" tlinks.mlir tlinks.o > /dev/null
if [ "$(uname)" = Darwin ]; then nd=__DATA,__noptrdata; nb=__DATA,__noptrbss,zerofill; nz=$nb; else nd=marmot_noptrdata; nb=.bss.marmot.noptr; nz=.bss.marmot.noptr.zero; fi
sed -e "s/DATASECT/$nd/" -e "s/BSSSECT/$nb/" -e "s/ZEROSECT/$nz/" "$src/noptr.mlir" > noptr.mlir
"$T0" noptr.mlir noptr.o > /dev/null
s=$("$L/llvm-objdump" -h noptr.o)
echo "$s" | grep -q noptrdata && echo "$s" | grep -q noptr || { echo "noptr: sections missing"; echo "$s"; exit 1; }
"$L/llvm-objdump" -t noptr.o | grep -E "np_zero" | grep -qE "noptr" || { echo "noptr: np_zero not in noptrbss"; "$L/llvm-objdump" -t noptr.o; exit 1; }
"$L/clang" -O1 -w "$src/noptr_main.c" noptr.o -o noptr && ./noptr && echo "noptr: ok"
if [ "$(uname)" = Darwin ]; then is=__TEXT,__StaticInit,regular,pure_instructions; else is=.text.startup.pkg.init; fi
sed "s/INITSECT/$is/" "$src/initsect.mlir" > initsect.mlir
for m in 0 1; do
  MARMOT_TIER0_MIR=$m "$T0" initsect.mlir initsect$m.o > /dev/null
  "$L/llvm-objdump" -t initsect$m.o | grep -E "(StaticInit|text.startup).*pkg.init" > /dev/null || { echo "initsect: pkg.init not in the init section"; "$L/llvm-objdump" -t initsect$m.o; exit 1; }
  "$L/clang" -O1 -w "$src/initsect_main.c" initsect$m.o -o initsect$m && ./initsect$m
done
echo "initsect: ok"
"$L/clang" -O1 -w "$src/tlinks_main.c" tlinks.o -Wl,$gc -o tlinks && ./tlinks && echo "tlinks: ok"
for s in $SEEDS; do
  python3 "$src/abi_gen.py" $s 80
  for m in a b; do
    "$L/mlir-translate" --mlir-to-llvmir $m.mlir -o $m.ll
    "$L/clang" -O0 -c -w $m.ll -o $m.llvm.o
  done
  "$T0" a.mlir a.t0.o > /dev/null
  "$L/clang" -O1 -w main.c a.llvm.o b.llvm.o -o ref
  "$L/clang" -O1 -w main.c a.t0.o b.llvm.o -o t0
  ./ref > ref.txt; ./t0 > t0.txt
  cmp -s ref.txt t0.txt && echo "abi seed $s: ok" || { echo "abi seed $s: differs"; diff ref.txt t0.txt | head; exit 1; }
done
"$L/clang++" -std=c++17 -O2 -I"$src/.." "$src/limm_test.cpp" -o limm && ./limm > /dev/null && echo "limm: ok"
