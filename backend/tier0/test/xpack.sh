# x86-64 tests of tier0, built on any host and run on x86-64 Linux:
#   sh xpack.sh <tier0-run> <llvm bin dir> <out dir> [abi seeds]
# then copy <out dir> to the Linux machine and run sh <out dir>/run.sh there
# (it needs cc). Same tests as run.sh: intrinsics, i128 and random signatures
# with tier0 and LLVM calling each other, compared to an all LLVM build.
set -e
T=$(cd "$(dirname "$1")" && pwd)/$(basename "$1"); L=$2; O=$3; SEEDS=${4:-"1 2 3 4 5"}
src=$(cd "$(dirname "$0")" && pwd)
rm -rf "$O" && mkdir -p "$O"
for t in intrinsics wide; do "$T" -x64 "$src/$t.mlir" "$O/$t.o" > /dev/null; cp "$src/${t}_main.c" "$O/"; done
sed "s/SECTION/marmot_tlinks/" "$src/tlinks.mlir" > "$O/tlinks.mlir"
"$T" -x64 "$O/tlinks.mlir" "$O/tlinks.o" > /dev/null && rm "$O/tlinks.mlir"; cp "$src/tlinks_main.c" "$O/"
for s in $SEEDS; do
  d="$O/abi$s"; mkdir -p "$d"
  (cd "$d" && python3 "$src/abi_gen.py" $s 80)
  for m in a b; do
    "$L/mlir-translate" --mlir-to-llvmir "$d/$m.mlir" -o "$d/$m.ll"
    "$L/clang" --target=x86_64-unknown-linux-gnu -O0 -c -w "$d/$m.ll" -o "$d/$m.llvm.o"
  done
  "$T" -x64 "$d/a.mlir" "$d/a.t0.o" > /dev/null
  rm "$d"/*.mlir "$d"/*.ll
done
cat > "$O/run.sh" <<'EOS'
cd "$(dirname "$0")"
for t in intrinsics wide; do cc -O1 -w ${t}_main.c $t.o -lm -o $t && ./$t > /dev/null && echo "$t: ok" || echo "$t: FAIL"; done
cc -O1 -w tlinks_main.c tlinks.o -Wl,--gc-sections -o tlinks && ./tlinks && echo "tlinks: ok" || echo "tlinks: FAIL"
if command -v ld.lld > /dev/null; then cc -O1 -w -fuse-ld=lld tlinks_main.c tlinks.o -Wl,--gc-sections -o tlinks && ./tlinks && echo "tlinks lld: ok" || echo "tlinks lld: FAIL"; fi
for d in abi*; do
  (cd $d && cc -O1 -w main.c a.llvm.o b.llvm.o -o ref && cc -O1 -w main.c a.t0.o b.llvm.o -o t0 && ./ref > ref.txt && ./t0 > t0.txt && cmp -s ref.txt t0.txt && echo "$d: ok" || { echo "$d: differs"; diff ref.txt t0.txt | head -5; })
done
EOS
