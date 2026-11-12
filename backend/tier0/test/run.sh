# Link-and-run tests of tier0 against LLVM-compiled C.
#   sh run.sh <tier0-run> <llvm bin dir> [abi seeds]
# The ABI test builds random signatures twice: tier0 and LLVM code calling
# each other, and all LLVM; the outputs must be identical.
set -e
T0=$(cd "$(dirname "$1")" && pwd)/$(basename "$1"); L=$2; SEEDS=${3:-"1 2 3 4 5"}
src=$(cd "$(dirname "$0")" && pwd)
w=$(mktemp -d); trap 'rm -rf "$w"' EXIT; cd "$w"
for t in intrinsics wide; do
  "$T0" "$src/$t.mlir" $t.o > /dev/null
  "$L/clang" -O1 -w "$src/${t}_main.c" $t.o -o $t && ./$t > /dev/null && echo "$t: ok"
done
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
