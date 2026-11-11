package runtime

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"testing"
	"unsafe"
)

func mapType(k, e *_type) *maptype {
	return &maptype{typ: _type{size: 8, kind: kindMap, align: 8}, key: k, elem: e}
}

var (
	tStruct16 = &_type{size: 16, kind: kindArray, align: 8, tflag: tflagComparable | tflagRegularMemory}
	tEface    = &(&interfacetype{typ: _type{size: 16, ptrdata: 16, kind: kindInterface, align: 8, tflag: tflagComparable, str: "interface {}"}}).typ
	tIntSlice = &_type{size: 24, ptrdata: 8, kind: kindSlice, align: 8, str: "[]int"}
)

func mget(t *maptype, m *hmap, k int) (int, bool) {
	p, ok := mapaccess2(t, m, unsafe.Pointer(&k))
	return *(*int)(p), ok
}

func mset(t *maptype, m *hmap, k, v int) {
	*(*int)(mapassign(t, m, unsafe.Pointer(&k))) = v
}

func mdel(t *maptype, m *hmap, k int) {
	mapdelete(t, m, unsafe.Pointer(&k))
}

func iterKeys(t *maptype, m *hmap) []int {
	var it hiter
	var keys []int
	for mapiterinit(t, m, &it); it.key != nil; mapiternext(&it) {
		keys = append(keys, *(*int)(it.key))
	}
	return keys
}

func TestMapRandomOps(t *testing.T) {
	for _, kt := range []*_type{tInt, tStruct16} {
		mt := mapType(kt, tInt)
		rng := rand.New(rand.NewSource(2))
		for _, hint := range []int{0, 1, 9, 1000} {
			m := makemap(mt, hint)
			ref := map[int]int{}
			for i := 0; i < 20000; i++ {
				k := rng.Intn(3000)
				// Make the key bytes of tStruct16 keys both words.
				switch rng.Intn(4) {
				case 0, 1:
					v := rng.Int()
					mset16(mt, m, k, v)
					ref[k] = v
				case 2:
					mdel16(mt, m, k)
					delete(ref, k)
				case 3:
					v, ok := mget16(mt, m, k)
					rv, rok := ref[k]
					if v != rv || ok != rok {
						t.Fatalf("get %d: got %d %v want %d %v", k, v, ok, rv, rok)
					}
				}
				if maplen(m) != len(ref) {
					t.Fatalf("len %d want %d", maplen(m), len(ref))
				}
			}
			keys := iterKeys16(mt, m)
			if len(keys) != len(ref) {
				t.Fatalf("iteration found %d keys want %d", len(keys), len(ref))
			}
			seen := map[int]bool{}
			for _, k := range keys {
				if seen[k] {
					t.Fatalf("key %d twice", k)
				}
				seen[k] = true
				if _, ok := ref[k]; !ok {
					t.Fatalf("iteration produced deleted key %d", k)
				}
			}
		}
	}
}

// For 16 byte keys the int is stored in both words.
func key16(mt *maptype, k int) unsafe.Pointer {
	if mt.key.size == 16 {
		v := [2]int{k, ^k}
		return unsafe.Pointer(&v)
	}
	return unsafe.Pointer(&k)
}

func mset16(mt *maptype, m *hmap, k, v int) { *(*int)(mapassign(mt, m, key16(mt, k))) = v }
func mdel16(mt *maptype, m *hmap, k int)    { mapdelete(mt, m, key16(mt, k)) }
func mget16(mt *maptype, m *hmap, k int) (int, bool) {
	p, ok := mapaccess2(mt, m, key16(mt, k))
	return *(*int)(p), ok
}
func iterKeys16(mt *maptype, m *hmap) []int { return iterKeys(mt, m) }

func TestMapNil(t *testing.T) {
	mt := mapType(tInt, tInt)
	if v, ok := mget(mt, nil, 1); v != 0 || ok {
		t.Errorf("nil map get")
	}
	mdel(mt, nil, 1)
	if maplen(nil) != 0 || len(iterKeys(mt, nil)) != 0 {
		t.Errorf("nil map len/iter")
	}
	mapclear(mt, nil)
	if got := errMsg(catch(func() { mset(mt, nil, 1, 2) })); got != "assignment to entry in nil map" {
		t.Errorf("nil map assign: %q", got)
	}
}

func TestMapStringKeys(t *testing.T) {
	mt := mapType(tString, tInt)
	m := makemap(mt, 0)
	ref := map[string]int{}
	for i := 0; i < 5000; i++ {
		k := fmt.Sprint("key", i%1700, "-", i%7)
		*(*int)(mapassign_faststr(mt, m, k)) = i
		ref[k] = i
	}
	for k, v := range ref {
		p, ok := mapaccess2_faststr(mt, m, k)
		if !ok || *(*int)(p) != v {
			t.Fatalf("key %q", k)
		}
	}
	if maplen(m) != len(ref) {
		t.Fatalf("len")
	}
	for k := range ref {
		mapdelete_faststr(mt, m, k)
	}
	if maplen(m) != 0 {
		t.Fatalf("len after delete")
	}
}

func TestMapNaN(t *testing.T) {
	mt := mapType(tFloat, tInt)
	m := makemap(mt, 0)
	nan := math.NaN()
	for i := 0; i < 3; i++ {
		*(*int)(mapassign(mt, m, unsafe.Pointer(&nan))) = i
	}
	if maplen(m) != 3 {
		t.Fatalf("NaN keys: len %d", maplen(m))
	}
	if _, ok := mapaccess2(mt, m, unsafe.Pointer(&nan)); ok {
		t.Fatalf("NaN lookup succeeded")
	}
	mapdelete(mt, m, unsafe.Pointer(&nan))
	if maplen(m) != 3 {
		t.Fatalf("NaN delete changed len")
	}
	n := 0
	var it hiter
	for mapiterinit(mt, m, &it); it.key != nil; mapiternext(&it) {
		if f := *(*float64)(it.key); f == f {
			t.Fatalf("non-NaN key")
		}
		n++
	}
	if n != 3 {
		t.Fatalf("iterated %d NaN keys", n)
	}
	// +0 and -0 are the same key; the stored key is updated.
	z := 0.0
	nz := math.Copysign(0, -1)
	*(*int)(mapassign(mt, m, unsafe.Pointer(&z))) = 1
	*(*int)(mapassign(mt, m, unsafe.Pointer(&nz))) = 2
	if maplen(m) != 4 {
		t.Fatalf("zero keys: len %d", maplen(m))
	}
	mapclear(mt, m)
	if maplen(m) != 0 || len(iterKeys(mt, m)) != 0 {
		t.Fatalf("clear")
	}
}

func TestMapInterfaceKeys(t *testing.T) {
	mt := mapType(tEface, tInt)
	m := makemap(mt, 0)
	box := func(typ *_type, p unsafe.Pointer) any {
		var v any
		e := efaceOf(&v)
		e.typ, e.data = typ, p
		return v
	}
	one, two := 1, 2
	s := "x"
	keys := []any{box(tInt, unsafe.Pointer(&one)), box(tInt, unsafe.Pointer(&two)), box(tString, unsafe.Pointer(&s))}
	for i := range keys {
		*(*int)(mapassign(mt, m, unsafe.Pointer(&keys[i]))) = i
	}
	other := 1
	k := box(tInt, unsafe.Pointer(&other))
	if p, ok := mapaccess2(mt, m, unsafe.Pointer(&k)); !ok || *(*int)(p) != 0 {
		t.Fatalf("interface key lookup by value")
	}
	var sl []int
	bad := box(tIntSlice, unsafe.Pointer(&sl))
	got := errMsg(catch(func() { mapassign(mt, m, unsafe.Pointer(&bad)) }))
	want := errMsg(catch(func() {
		mm := map[any]int{}
		mm[any([]int(nil))] = 1
	}))
	if got != want {
		t.Errorf("unhashable key: got %q want %q", got, want)
	}
	got = errMsg(catch(func() { mapaccess1(mt, nil, unsafe.Pointer(&bad)) }))
	if got != want {
		t.Errorf("unhashable key on nil map: got %q want %q", got, want)
	}
}

// TestMapIterateWhileMutating checks the Go spec rules: every entry that
// exists for the whole iteration is produced exactly once, deleted entries
// not yet reached are not produced, and no entry is produced twice, even
// when the table grows or is cleared during the iteration.
func TestMapIterateWhileMutating(t *testing.T) {
	mt := mapType(tInt, tInt)
	rng := rand.New(rand.NewSource(3))
	for trial := 0; trial < 300; trial++ {
		m := makemap(mt, 0)
		n := 1 + rng.Intn(200)
		for i := 0; i < n; i++ {
			mset(mt, m, i, i)
		}
		stable := map[int]bool{}
		for i := 0; i < n; i++ {
			stable[i] = true
		}
		deleted := map[int]bool{}
		produced := map[int]bool{}
		var it hiter
		next := n
		cleared := false
		for mapiterinit(mt, m, &it); it.key != nil; mapiternext(&it) {
			k := *(*int)(it.key)
			v := *(*int)(it.elem)
			if produced[k] {
				t.Fatalf("trial %d: key %d produced twice", trial, k)
			}
			produced[k] = true
			if deleted[k] || cleared && k < n {
				t.Fatalf("trial %d: deleted key %d produced", trial, k)
			}
			if k < n && v != k && v != -k {
				t.Fatalf("trial %d: key %d has value %d", trial, k, v)
			}
			switch rng.Intn(10) {
			case 0, 1, 2:
				// Insert new keys, which may grow the table.
				for j := 0; j < rng.Intn(50); j++ {
					mset(mt, m, next, next)
					next++
				}
			case 3, 4:
				d := rng.Intn(n)
				if !produced[d] {
					deleted[d] = true
				}
				delete(stable, d)
				mdel(mt, m, d)
			case 5:
				u := rng.Intn(n)
				if _, ok := mget(mt, m, u); ok {
					mset(mt, m, u, -u)
				}
			case 6:
				if rng.Intn(20) == 0 {
					mapclear(mt, m)
					cleared = true
					for k := range stable {
						delete(stable, k)
					}
				}
			}
		}
		for k := range stable {
			if !produced[k] {
				t.Fatalf("trial %d: stable key %d not produced", trial, k)
			}
		}
	}
}

func TestMapIterationOrderRandom(t *testing.T) {
	mt := mapType(tInt, tInt)
	m := makemap(mt, 0)
	for i := 0; i < 20; i++ {
		mset(mt, m, i, i)
	}
	firsts := map[int]bool{}
	for i := 0; i < 100; i++ {
		firsts[iterKeys(mt, m)[0]] = true
	}
	if len(firsts) < 5 {
		t.Errorf("iteration start not random: %v", firsts)
	}
	keys := iterKeys(mt, m)
	sort.Ints(keys)
	for i, k := range keys {
		if k != i {
			t.Fatalf("keys %v", keys)
		}
	}
}

func TestMapLargeElem(t *testing.T) {
	big := &_type{size: 2048, kind: kindArray, align: 8}
	mt := mapType(tInt, big)
	k := 5
	p := mapaccess1(mt, nil, unsafe.Pointer(&k))
	for _, b := range unsafe.Slice((*byte)(p), 2048) {
		if b != 0 {
			t.Fatal("large zero value not zero")
		}
	}
}

func BenchmarkMapAssignInt(b *testing.B) {
	mt := mapType(tInt, tInt)
	m := makemap(mt, 0)
	for i := 0; i < b.N; i++ {
		k := i & 0xffff
		*(*int)(mapassign_fast64(mt, m, uint64(k))) = i
	}
}

func BenchmarkMapAccessInt(b *testing.B) {
	mt := mapType(tInt, tInt)
	m := makemap(mt, 0)
	for i := 0; i < 1<<16; i++ {
		*(*int)(mapassign_fast64(mt, m, uint64(i))) = i
	}
	b.ResetTimer()
	s := 0
	for i := 0; i < b.N; i++ {
		s += *(*int)(mapaccess1_fast64(mt, m, uint64(i&0xffff)))
	}
	_ = s
}

func BenchmarkHostMapAccessInt(b *testing.B) {
	m := map[int]int{}
	for i := 0; i < 1<<16; i++ {
		m[i] = i
	}
	b.ResetTimer()
	s := 0
	for i := 0; i < b.N; i++ {
		s += m[i&0xffff]
	}
	_ = s
}
