#!/bin/sh
# warm.sh: compile a small program in every mode, to every stage and for wasm, so that the
# standard library the playground programs use is in the image's build cache.
set -eu
d=$(mktemp -d)
cd "$d"
printf 'module play\n\ngo 1.24\n' > go.mod
cat > main.go <<'GO'
package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

func main() {
	var wg sync.WaitGroup
	ch := make(chan int, 4)
	for i := range 4 {
		wg.Go(func() { ch <- i * i })
	}
	wg.Wait()
	close(ch)
	var xs []int
	for x := range ch {
		xs = append(xs, x)
	}
	slices.Sort(xs)
	sort.Ints(xs)
	fmt.Println(xs, math.Sqrt(2), strings.Repeat("ab", 2), strconv.Itoa(7), time.Second, errors.New("e"))
	fmt.Fprintln(os.Stderr, "done")
}
GO
m=/marmot/build/bin/marmot
for mode in "" "-O 2" "-release"; do
	# shellcheck disable=SC2086 # the mode is zero or more flags
	$m build $mode -o prog .
	$m build $mode -dialect go -emit mlir -o out.mlir .
	$m build $mode -emit llvm -o out.ll .
	$m build $mode -emit obj -o out.o .
	# Run on /play: the program as wasip1/wasm, which the page runs in the browser.
	$m build $mode -target wasip1/wasm -o prog.wasm .
done
./prog
cd /
rm -rf "$d"
