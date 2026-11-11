package runtime

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"
	"unsafe"
)

// Host type descriptors for tests.

func basicType(size uintptr, kind uint8, ptrs bool, tflag uint8) *_type {
	t := &_type{size: size, kind: kind, align: uint8(min(size, 8)), tflag: tflag}
	if t.align == 0 {
		t.align = 1
	}
	if ptrs {
		t.ptrdata = ptrSize
	}
	return t
}

var (
	tInt    = basicType(8, kindInt, false, tflagComparable|tflagRegularMemory|tflagNamed)
	tInt32  = basicType(4, kindInt32, false, tflagComparable|tflagRegularMemory|tflagNamed)
	tByte   = basicType(1, kindUint8, false, tflagComparable|tflagRegularMemory|tflagNamed)
	tFloat  = basicType(8, kindFloat64, false, tflagComparable|tflagNamed)
	tString = &_type{size: 16, ptrdata: 8, kind: kindString, align: 8, tflag: tflagComparable | tflagNamed, str: "string"}
	tPtr    = basicType(8, kindPointer, true, tflagComparable|tflagRegularMemory|tflagDirectIface)
)

func init() {
	tInt.str = "int"
	tInt32.str = "int32"
	tByte.str = "uint8"
	tFloat.str = "float64"
}

func sizedType(size uintptr, ptrs bool) *_type {
	t := &_type{size: size, kind: kindArray, align: 1}
	if ptrs {
		t.ptrdata = ptrSize
		t.align = 8
	}
	return t
}

// appendCaps returns the capacities a real append produces when growing a
// slice of T one element at a time.
func appendCaps[T any](n int) []int {
	var s []T
	var caps []int
	last := -1
	var zero T
	for i := 0; i < n; i++ {
		s = append(s, zero)
		// Escape to the heap: the gc compiler gives non-escaping slices
		// a 32 byte stack buffer on the first append.
		escapeSink = s
		if cap(s) != last {
			last = cap(s)
			caps = append(caps, last)
		}
	}
	return caps
}

var escapeSink any

func growCaps(et *_type, n int) []int {
	var caps []int
	var p unsafe.Pointer
	l, c := 0, 0
	for i := 0; i < n; i++ {
		if l+1 > c {
			s := growslice(et, p, l, c, l+1)
			p, c = s.array, s.cap
			caps = append(caps, c)
		}
		l++
	}
	return caps
}

type (
	b3  [3]byte
	b5  [5]byte
	b12 [12]byte
	b24 [24]byte
	b40 [40]byte
	p2  struct{ a, b *int }
	p3  struct{ a, b, c *int }
	p5  [5]*int
)

func TestGrowsliceMatchesAppend(t *testing.T) {
	const n = 5000
	cases := []struct {
		name string
		et   *_type
		want []int
	}{
		{"byte", sizedType(1, false), appendCaps[byte](n)},
		{"int16", sizedType(2, false), appendCaps[int16](n)},
		{"int32", sizedType(4, false), appendCaps[int32](n)},
		{"int64", sizedType(8, false), appendCaps[int64](n)},
		{"[3]byte", sizedType(3, false), appendCaps[b3](n)},
		{"[5]byte", sizedType(5, false), appendCaps[b5](n)},
		{"[12]byte", sizedType(12, false), appendCaps[b12](n)},
		{"[24]byte", sizedType(24, false), appendCaps[b24](n)},
		{"[40]byte", sizedType(40, false), appendCaps[b40](n)},
		{"*int", sizedType(8, true), appendCaps[*int](n)},
		{"string", sizedType(16, true), appendCaps[string](n)},
		{"p3", sizedType(24, true), appendCaps[p3](n)},
		{"p5", sizedType(40, true), appendCaps[p5](n)},
	}
	for _, c := range cases {
		got := growCaps(c.et, n)
		if fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("%s:\n got %v\nwant %v", c.name, got, c.want)
		}
	}
}

func TestGrowsliceBulk(t *testing.T) {
	// append(s, many...) jumps straight to newLen and rounds up.
	for _, add := range []int{1, 3, 7, 100, 1000, 3000} {
		for _, start := range []int{0, 1, 5, 300, 1024} {
			s := make([]int64, start, start)
			escapeSink = s
			s = append(s, make([]int64, add)...)
			escapeSink = s
			old := make([]int64, start+1)
			g := growslice(sizedType(8, false), unsafe.Pointer(&old[0]), start, start, start+add)
			if g.cap != cap(s) || g.len != start+add {
				t.Errorf("start %d add %d: got len %d cap %d, want cap %d", start, add, g.len, g.cap, cap(s))
			}
		}
	}
}

func TestGrowsliceCopies(t *testing.T) {
	src := []int64{1, 2, 3}
	s := growslice(sizedType(8, false), unsafe.Pointer(&src[0]), 3, 3, 4)
	got := unsafe.Slice((*int64)(s.array), s.cap)
	if got[0] != 1 || got[1] != 2 || got[2] != 3 || got[3] != 0 || s.len != 4 {
		t.Errorf("bad copy: %v len %d", got, s.len)
	}
}

func catch(f func()) (v any) {
	defer func() { v = recover() }()
	f()
	return nil
}

func errMsg(v any) string {
	if e, ok := v.(error); ok {
		return e.Error()
	}
	return fmt.Sprint(v)
}

func TestMakesliceErrors(t *testing.T) {
	et := sizedType(8, false)
	n := -1
	big := 1 << 60
	wantLen := catch(func() { _ = make([]int64, n) })
	wantCap := catch(func() { _ = make([]int64, 1, big) })
	gotLen := catch(func() { makeslice(et, n, 1) })
	gotCap := catch(func() { makeslice(et, 1, big) })
	if errMsg(gotLen) != errMsg(wantLen) || errMsg(gotCap) != errMsg(wantCap) {
		t.Errorf("got %q %q want %q %q", errMsg(gotLen), errMsg(gotCap), errMsg(wantLen), errMsg(wantCap))
	}
	if _, ok := gotLen.(Error); !ok {
		t.Errorf("makeslice error is not a runtime.Error")
	}
}

func TestBoundsMessages(t *testing.T) {
	s := make([]int, 3, 5)
	str := "abc"
	var arr [4]int
	i, j, k := 0, 0, 0
	u := uint64(0)
	check := func(name string, real func(), code int, x int64, signed bool, y int) {
		t.Helper()
		want := errMsg(catch(real))
		got := errMsg(catch(func() { panicBounds(code, x, signed, y) }))
		if got != want {
			t.Errorf("%s: got %q want %q", name, got, want)
		}
	}
	i = 7
	check("index", func() { _ = s[i] }, boundsIndex, 7, true, 3)
	i = -2
	check("index neg", func() { _ = s[i] }, boundsIndex, -2, true, 3)
	u = 1<<64 - 1
	check("index unsigned", func() { _ = s[u] }, boundsIndex, int64(u), false, 3)
	j = 9
	check("slice acap", func() { _ = s[:j] }, boundsSliceAcap, 9, true, 5)
	check("slice alen", func() { _ = str[:j] }, boundsSliceAlen, 9, true, 3)
	i, j = 3, 2
	check("slice b", func() { _ = s[i:j] }, boundsSliceB, 3, true, 2)
	i = -1
	check("slice b neg", func() { _ = s[i:] }, boundsSliceB, -1, true, 3)
	k = 6
	check("slice3 acap", func() { _ = s[0:1:k] }, boundsSlice3Acap, 6, true, 5)
	j, k = 4, 3
	check("slice3 b", func() { _ = s[0:j:k] }, boundsSlice3B, 4, true, 3)
	i, j = 2, 1
	check("slice3 c", func() { _ = s[i:j:3] }, boundsSlice3C, 2, true, 1)
	k = 5
	check("array slice3 alen", func() { _ = arr[0:1:k] }, boundsSlice3Alen, 5, true, 4)
	check("convert", func() { _ = [4]int(s) }, boundsConvert, 3, true, 4)

	// The shorthand entry points.
	i = 5
	if got, want := errMsg(catch(func() { panicindex(5, 3) })), errMsg(catch(func() { _ = s[i] })); got != want {
		t.Errorf("panicindex: got %q want %q", got, want)
	}
	if got, want := errMsg(catch(func() { panicSliceConvert(4, 3) })), errMsg(catch(func() { _ = [4]int(s) })); got != want {
		t.Errorf("panicSliceConvert: got %q want %q", got, want)
	}
}

func TestOtherRuntimeErrors(t *testing.T) {
	zero := 0
	var m map[int]int
	var p *int
	cases := []struct {
		real, ours func()
	}{
		{func() { _ = 1 / zero }, panicdivide},
		{func() { m[1] = 2 }, panicnilmap},
		{func() { _ = *p }, panicmem},
		{func() { n := -1; _ = 1 << n }, panicshift},
		{func() { n := -1; _ = unsafe.Slice(&zero, n) }, panicunsafeslicelen},
		{func() { n := 1; _ = unsafe.Slice((*int)(nil), n) }, panicunsafeslicenilptr},
		{func() { n := -1; _ = unsafe.String(new(byte), n) }, panicunsafestringlen},
	}
	for i, c := range cases {
		want := errMsg(catch(c.real))
		got := errMsg(catch(c.ours))
		if got != want {
			t.Errorf("case %d: got %q want %q", i, got, want)
		}
	}
}

func TestStringFunctions(t *testing.T) {
	if got := concatstrings([]string{"a", "", "bc", "def"}); got != "abcdef" {
		t.Errorf("concatstrings: %q", got)
	}
	if got := concatstrings([]string{"", "x", ""}); got != "x" {
		t.Errorf("concatstrings single: %q", got)
	}
	if got := concatstrings(nil); got != "" {
		t.Errorf("concatstrings empty: %q", got)
	}
	if concatstring2("ab", "cd") != "abcd" || concatstring3("a", "b", "c") != "abc" {
		t.Errorf("concatstring2/3")
	}
	words := []string{"", "a", "ab", "abc", "b", "\xff", "a\x00", "abd"}
	for _, a := range words {
		for _, b := range words {
			want := strings.Compare(a, b)
			if got := cmpstring(a, b); got != want {
				t.Errorf("cmpstring(%q, %q) = %d, want %d", a, b, got, want)
			}
			if strequal(a, b) != (a == b) {
				t.Errorf("strequal(%q, %q)", a, b)
			}
		}
	}
	for _, s := range []string{"", "hello", "héllo, 世界", "\xff\xfe", strings.Repeat("x", 100)} {
		b := stringtoslicebyte(s)
		want := []byte(s)
		if string(b) != s || cap(b) != cap(append([]byte(nil), want...)) && len(s) > 32 {
			t.Errorf("stringtoslicebyte(%q) = %q cap %d", s, b, cap(b))
		}
		if slicebytetostring(want) != s {
			t.Errorf("slicebytetostring(%q)", s)
		}
		r := stringtoslicerune(s)
		if fmt.Sprint(r) != fmt.Sprint([]rune(s)) {
			t.Errorf("stringtoslicerune(%q) = %v", s, r)
		}
		if slicerunetostring(r) != string([]rune(s)) {
			t.Errorf("slicerunetostring(%q)", s)
		}
	}
	runes := []rune{0, 'a', 0x7f, 0x80, 0x7ff, 0x800, 0xd7ff, 0xd800, 0xdfff, 0xe000, 0xfffd, 0xffff, 0x10000, 0x10ffff, 0x110000, -1}
	if slicerunetostring(runes) != string(runes) {
		t.Errorf("slicerunetostring invalid runes: %q want %q", slicerunetostring(runes), string(runes))
	}
	for _, v := range []int64{0, 65, 0x10ffff, 0x110000, -1, 1 << 40, 0xd800} {
		if got, want := intstring(v), string(rune(v)); int64(rune(v)) == v && got != want {
			t.Errorf("intstring(%d) = %q want %q", v, got, want)
		}
	}
	if intstring(1<<40) != "\uFFFD" {
		t.Errorf("intstring(1<<40)")
	}
}

func TestDecoderuneMatchesRange(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	inputs := []string{"", "abc", "héllo", "\xff", "\xe2\x82", "\xed\xa0\x80", "\xf4\x90\x80\x80", "\xc0\x80", "€𝄞"}
	for i := 0; i < 2000; i++ {
		b := make([]byte, rng.Intn(12))
		for j := range b {
			b[j] = byte(rng.Intn(256))
			if rng.Intn(3) == 0 {
				b[j] = 0x80 | byte(rng.Intn(64))
			}
		}
		inputs = append(inputs, string(b))
	}
	for _, s := range inputs {
		var want, got []string
		for i, r := range s {
			want = append(want, fmt.Sprint(i, r))
		}
		for i := 0; i < len(s); {
			r, next := decoderune(s, i)
			got = append(got, fmt.Sprint(i, r))
			i = next
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("decoderune(%q): got %v want %v", s, got, want)
		}
		if countrunes(s) != utf8.RuneCountInString(s) {
			t.Fatalf("countrunes(%q)", s)
		}
	}
}

func TestSlicecopy(t *testing.T) {
	a := []byte("abcdef")
	n := slicecopy(unsafe.Pointer(&a[1]), 5, unsafe.Pointer(&a[0]), 5, 1)
	if n != 5 || string(a) != "aabcde" {
		t.Errorf("overlap copy: %d %q", n, a)
	}
	if slicecopy(nil, 0, unsafe.Pointer(&a[0]), 3, 1) != 0 {
		t.Errorf("copy to empty")
	}
}

func TestComplexDiv(t *testing.T) {
	inf := float64frombits(0x7FF0000000000000)
	nan := inf - inf
	vals := []complex128{0, 1, -1, 1i, complex(3, 4), complex(inf, 0), complex(0, inf),
		complex(nan, 0), complex(1e300, 1e300), complex(1e-300, 1e-300), complex(-2, 0.5)}
	for _, n := range vals {
		for _, m := range vals {
			want := n / m
			got := complex128div(n, m)
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("%v / %v = %v, want %v", n, m, got, want)
			}
		}
	}
}

func TestPanicValuePrinting(t *testing.T) {
	type myInt int
	tMyInt := basicType(8, kindInt, false, tflagComparable|tflagRegularMemory|tflagNamed)
	tMyInt.str = "main.myInt"
	tMyStr := &_type{size: 16, ptrdata: 8, kind: kindString, align: 8, tflag: tflagComparable | tflagNamed, str: "main.S"}
	tStruct := &_type{size: 16, kind: kindStruct, align: 8, str: "main.T"}

	box := func(t *_type, p unsafe.Pointer) any {
		var v any
		e := efaceOf(&v)
		e.typ = t
		e.data = p
		return v
	}
	n := 42
	s := "a\nb"
	boom := "boom"
	xy := "x\ny"
	f := 1.5
	f32 := float32(0.1)
	c := complex(1, 2)
	u8 := uint8(7)
	tr := true
	tF32 := basicType(4, kindFloat32, false, tflagComparable|tflagNamed)
	tF32.str = "float32"
	tC128 := basicType(16, kindComplex128, false, tflagComparable|tflagNamed)
	tC128.str = "complex128"
	tBool := basicType(1, kindBool, false, tflagComparable|tflagRegularMemory|tflagNamed)
	tBool.str = "bool"
	cases := []struct {
		v    any
		want string
	}{
		{nil, "nil"},
		{box(tString, unsafe.Pointer(&boom)), "boom"},
		{box(tString, unsafe.Pointer(&xy)), "x\n\ty"},
		{box(tFloat, unsafe.Pointer(&f)), "1.5"},
		{box(tF32, unsafe.Pointer(&f32)), "0.1"},
		{box(tC128, unsafe.Pointer(&c)), "(1+2i)"},
		{box(tByte, unsafe.Pointer(&u8)), "7"},
		{box(tBool, unsafe.Pointer(&tr)), "true"},
		{box(tInt, unsafe.Pointer(&n)), "42"},
		{box(tMyInt, unsafe.Pointer(&n)), "main.myInt(42)"},
		{box(tMyStr, unsafe.Pointer(&s)), "main.S(\"a\n\tb\")"},
	}
	for _, c := range cases {
		takeOutput()
		printpanicval(c.v)
		if got := takeOutput(); got != c.want {
			t.Errorf("printpanicval = %q want %q", got, c.want)
		}
	}
	takeOutput()
	printpanicval(box(tStruct, unsafe.Pointer(uintptr(0x1234))))
	if got := takeOutput(); got != "(main.T) 0x1234" {
		t.Errorf("struct panic value: %q", got)
	}
}

func TestPanicChainPrinting(t *testing.T) {
	err := errorString("integer divide by zero")
	first := "first"
	p1 := &_panic{arg: boxString(&first)}
	p2 := &_panic{arg: any(err), link: p1}
	preprintpanics(p2)
	takeOutput()
	printpanics(p2)
	want := "panic: first\n\tpanic: runtime error: integer divide by zero\n"
	if got := takeOutput(); got != want {
		t.Errorf("got %q want %q", got, want)
	}

	again := "again"
	v := boxString(&again)
	q1 := &_panic{arg: v, recovered: true}
	q2 := &_panic{arg: v, link: q1}
	preprintpanics(q2)
	printpanics(q2)
	want = "panic: again [recovered, repanicked]\n"
	if got := takeOutput(); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestFatalPanicMatchesGo compares the first stderr lines of real Go
// panics with what fatalpanic prints.
func TestFatalPanicMatchesGo(t *testing.T) {
	src := `package main

import "errors"

type E struct{}

func (E) Error() string { return "custom error" }

func main() {
	defer func() {
		r := recover()
		panic(r)
	}()
	var err error = E{}
	_ = errors.New
	panic(err)
}
`
	want, code := runGo(t, src)
	if code != 2 {
		t.Fatalf("exit code %d: %s", code, want)
	}
	// Only the panic lines are compared.
	want = want[:strings.Index(want, "\n\ngoroutine")+1]
	takeOutput()
	p1 := &_panic{recovered: true}
	p2 := &_panic{link: p1}
	// The real chain holds the same interface value twice.
	var ev any = errorString("x")
	p1.arg, p2.arg = ev, ev
	preprintpanics(p2)
	printpanics(p2)
	got := takeOutput()
	want = strings.Replace(want, "custom error", "runtime error: x", 1)
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestIfaceeqDistinctItabs(t *testing.T) {
	it := &interfacetype{typ: _type{kind: kindInterface}, mcount: 1}
	tab1 := &itab{inter: it, typ: tInt}
	tab2 := &itab{inter: it, typ: tInt}
	a, b := 5, 5
	x := iface{tab1, unsafe.Pointer(&a)}
	y := iface{tab2, unsafe.Pointer(&b)}
	if !ifaceeq(x, y) {
		t.Errorf("equal values with distinct itabs compare unequal")
	}
	b = 6
	if ifaceeq(x, y) {
		t.Errorf("different values compare equal")
	}
	if ifaceeq(x, iface{}) || !ifaceeq(iface{}, iface{}) {
		t.Errorf("nil interface comparison")
	}
}

func TestGetitab(t *testing.T) {
	sig := &_type{kind: kindFunc, str: "func() string"}
	other := &_type{kind: kindFunc, str: "func() int"}
	imeths := []imethod{{name: "A", ityp: sig}, {name: "B", ityp: sig}}
	inter := &interfacetype{typ: _type{kind: kindInterface, str: "main.I"}, methods: &imeths[0], mcount: 2}
	code := unsafe.Pointer(new(int))
	mk := func(ms []method) *_type {
		return &_type{kind: kindStruct, str: "main.T", uncommon: &uncommontype{methods: &ms[0], mcount: uint32(len(ms))}}
	}
	// B has a nil ifn (never called through an interface): still matches.
	good := mk([]method{{name: "A", mtyp: sig, ifn: code}, {name: "B", mtyp: sig}, {name: "C", mtyp: sig, ifn: code}})
	m := assertE2I2(inter, good)
	if m == nil || *itabFun(m, 0) != code || *itabFun(m, 1) != nil {
		t.Fatalf("good type: itab %v", m)
	}
	if assertE2I2(inter, good) != m {
		t.Errorf("itab not cached")
	}
	wrongSig := mk([]method{{name: "A", mtyp: sig, ifn: code}, {name: "B", mtyp: other, ifn: code}})
	if assertE2I2(inter, wrongSig) != nil || assertE2I2(inter, wrongSig) != nil {
		t.Errorf("wrong signature accepted")
	}
	missing := mk([]method{{name: "A", mtyp: sig, ifn: code}})
	got := errMsg(catch(func() { assertE2I(inter, missing) }))
	if got != "interface conversion: main.T is not main.I: missing method B" {
		t.Errorf("got %q", got)
	}
	got = errMsg(catch(func() { assertE2I(inter, nil) }))
	if got != "interface conversion: interface is nil, not main.I" {
		t.Errorf("got %q", got)
	}
}

// boxString makes an interface{} holding *p with the marmot-layout string
// descriptor, as compiled code would.
func boxString(p *string) any {
	var v any
	e := efaceOf(&v)
	e.typ, e.data = tString, unsafe.Pointer(p)
	return v
}
