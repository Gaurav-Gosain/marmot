// Maps: an open addressing hash table in the swiss table style.
//
// The table is an array of groups. A group holds 8 control bytes (one
// uint64) followed by 8 slots; a slot holds a key and then an element. A
// control byte is ctrlEmpty, ctrlDeleted, or for a full slot the low 7 bits
// (h2) of the key's hash. The upper hash bits (h1) select the first group;
// probing visits groups in triangular order, which covers every group of a
// power of two table. Control bytes are matched 8 at a time with plain
// 64 bit arithmetic.
//
// The load factor is at most 7/8. When an insert finds no room the table is
// rebuilt into a new array (twice the size, or the same size when deleted
// slots dominate). Entries never move inside an array, so an iterator that
// holds an older array stays valid: for each entry of its array it looks
// the key up in the current table to see whether the entry still exists and
// to get its current element.
//
// Maps with 4 or 8 byte plain memory keys and string keys use fast paths
// for hashing and key comparison. Pure file.

package runtime

import "unsafe"

const (
	groupSlots  = 8
	ctrlEmpty   = 0x80
	ctrlDeleted = 0xFE

	lsbs = 0x0101010101010101
	msbs = 0x8080808080808080

	// Key kinds for the fast paths.
	keyGeneric = 0
	keyMem32   = 1
	keyMem64   = 2
	keyString  = 3

	// maxZero is the size of the shared zero value returned by mapaccess1
	// for missing keys.
	maxZero = 1024
)

var zeroVal [maxZero]byte

// hmap is the map header. A map value is a *hmap.
type hmap struct {
	count      int // number of live entries; must be first (maplen)
	seed       uintptr
	groups     unsafe.Pointer // ngroups groups
	mask       uintptr        // ngroups - 1
	growthLeft int            // empty slots that can still be filled
	tombstones int
	clearSeq   uint32
	keyKind    uint8
	noscan     bool
	key        *_type
	elem       *_type
	elemOff    uintptr // offset of the element in a slot
	slotSize   uintptr
	groupSize  uintptr
}

// hiter is a map iteration. The compiler allocates it (zeroed) and reads
// key and elem after each mapiterinit/mapiternext call; key is nil when the
// iteration is over.
type hiter struct {
	key  unsafe.Pointer
	elem unsafe.Pointer
	t    *maptype
	m    *hmap

	groups   unsafe.Pointer // array being iterated
	mask     uintptr
	start    uintptr // random first slot index
	i        uintptr // slots visited
	clearSeq uint32
}

func alignUp(n, a uintptr) uintptr {
	return (n + a - 1) &^ (a - 1)
}

// makemap implements make(map[k]v, hint).
func makemap(t *maptype, hint int) *hmap {
	if hint < 0 {
		hint = 0
	}
	m := (*hmap)(mallocConservative(unsafe.Sizeof(hmap{})))
	m.seed = uintptr(cheaprand64())
	m.key = t.key
	m.elem = t.elem
	k := t.key
	e := t.elem
	ealign := uintptr(e.align)
	if ealign == 0 {
		ealign = 1
	}
	kalign := uintptr(k.align)
	if kalign == 0 {
		kalign = 1
	}
	m.elemOff = alignUp(k.size, ealign)
	m.slotSize = alignUp(m.elemOff+e.size, max(kalign, ealign))
	m.groupSize = 8 + groupSlots*m.slotSize
	m.noscan = k.ptrdata == 0 && e.ptrdata == 0
	switch {
	case k.kind == kindString:
		m.keyKind = keyString
	case k.tflag&tflagRegularMemory != 0 && k.size == 4:
		m.keyKind = keyMem32
	case k.tflag&tflagRegularMemory != 0 && k.size == 8:
		m.keyKind = keyMem64
	}
	if hint > 0 {
		m.resize(groupsForCount(uintptr(hint)))
	}
	return m
}

// makemap64 is makemap with an int64 hint.
func makemap64(t *maptype, hint int64) *hmap {
	if int64(int(hint)) != hint {
		hint = 0
	}
	return makemap(t, int(hint))
}

// makemap_small implements make(map[k]v) and map literals with few
// entries.
func makemap_small(t *maptype) *hmap {
	return makemap(t, 0)
}

// groupsForCount returns the number of groups needed to hold n entries
// under the load factor.
func groupsForCount(n uintptr) uintptr {
	slots := (n*8 + 6) / 7
	g := uintptr(1)
	for g*groupSlots < slots {
		g <<= 1
	}
	return g
}

func (m *hmap) group(g uintptr) unsafe.Pointer {
	return unsafe.Add(m.groups, g*m.groupSize)
}

func ctrlOf(grp unsafe.Pointer) *uint64 { return (*uint64)(grp) }

func (m *hmap) slotKey(grp unsafe.Pointer, s uintptr) unsafe.Pointer {
	return unsafe.Add(grp, 8+s*m.slotSize)
}

func (m *hmap) slotElem(grp unsafe.Pointer, s uintptr) unsafe.Pointer {
	return unsafe.Add(grp, 8+s*m.slotSize+m.elemOff)
}

func setCtrl(grp unsafe.Pointer, s uintptr, b uint8) {
	*(*uint8)(unsafe.Add(grp, s)) = b
}

// Control byte matching, 8 slots at a time. The result has bit 8*i+7 set
// for each matching slot i.

func matchH2(ctrl uint64, h2 uintptr) uint64 {
	v := ctrl ^ (lsbs * uint64(h2))
	return ((v - lsbs) &^ v) & msbs
}

func matchEmpty(ctrl uint64) uint64 {
	return (ctrl &^ (ctrl << 6)) & msbs
}

func matchEmptyOrDeleted(ctrl uint64) uint64 {
	return ctrl & msbs
}

func matchFull(ctrl uint64) uint64 {
	return ^ctrl & msbs
}

// firstSlot returns the index of the lowest set match bit.
func firstSlot(b uint64) uintptr {
	return uintptr(trailingZeros64(b) >> 3)
}

// fmix64 is the murmur3 finalizer: a fast, well mixing hash for integer
// keys.
func fmix64(x uint64) uintptr {
	x ^= x >> 33
	x *= 0xff51afd7ed558ccd
	x ^= x >> 33
	x *= 0xc4ceb9fe1a85ec53
	x ^= x >> 33
	return uintptr(x)
}

// hash computes the hash of the key at k.
func (m *hmap) hash(k unsafe.Pointer) uintptr {
	switch m.keyKind {
	case keyMem32:
		return fmix64(uint64(*(*uint32)(k)) ^ uint64(m.seed))
	case keyMem64:
		return fmix64(*(*uint64)(k) ^ uint64(m.seed))
	case keyString:
		return strhash(k, m.seed)
	}
	return typehash(m.key, k, m.seed)
}

// keyEqual reports whether the stored key a equals the key b.
func (m *hmap) keyEqual(a, b unsafe.Pointer) bool {
	switch m.keyKind {
	case keyMem32:
		return *(*uint32)(a) == *(*uint32)(b)
	case keyMem64:
		return *(*uint64)(a) == *(*uint64)(b)
	case keyString:
		return *(*string)(a) == *(*string)(b)
	}
	return typeequal(m.key, a, b)
}

// find returns the slot of key k, or nil.
func (m *hmap) find(k unsafe.Pointer, hash uintptr) (grp unsafe.Pointer, slot uintptr) {
	if m.groups == nil {
		return nil, 0
	}
	h2 := hash & 0x7f
	g := (hash >> 7) & m.mask
	for i := uintptr(1); ; i++ {
		grp = m.group(g)
		ctrl := *ctrlOf(grp)
		for b := matchH2(ctrl, h2); b != 0; b &= b - 1 {
			s := firstSlot(b)
			if m.keyEqual(m.slotKey(grp, s), k) {
				return grp, s
			}
		}
		if matchEmpty(ctrl) != 0 {
			return nil, 0
		}
		g = (g + i) & m.mask
	}
}

// find64 is find for 8 byte plain memory keys.
func (m *hmap) find64(key uint64) unsafe.Pointer {
	hash := fmix64(key ^ uint64(m.seed))
	h2 := hash & 0x7f
	g := (hash >> 7) & m.mask
	for i := uintptr(1); ; i++ {
		grp := unsafe.Add(m.groups, g*m.groupSize)
		ctrl := *(*uint64)(grp)
		for b := matchH2(ctrl, h2); b != 0; b &= b - 1 {
			slot := unsafe.Add(grp, 8+firstSlot(b)*m.slotSize)
			if *(*uint64)(slot) == key {
				return unsafe.Add(slot, m.elemOff)
			}
		}
		if matchEmpty(ctrl) != 0 {
			return nil
		}
		g = (g + i) & m.mask
	}
}

// find32 is find for 4 byte plain memory keys.
func (m *hmap) find32(key uint32) unsafe.Pointer {
	hash := fmix64(uint64(key) ^ uint64(m.seed))
	h2 := hash & 0x7f
	g := (hash >> 7) & m.mask
	for i := uintptr(1); ; i++ {
		grp := unsafe.Add(m.groups, g*m.groupSize)
		ctrl := *(*uint64)(grp)
		for b := matchH2(ctrl, h2); b != 0; b &= b - 1 {
			slot := unsafe.Add(grp, 8+firstSlot(b)*m.slotSize)
			if *(*uint32)(slot) == key {
				return unsafe.Add(slot, m.elemOff)
			}
		}
		if matchEmpty(ctrl) != 0 {
			return nil
		}
		g = (g + i) & m.mask
	}
}

// findstr is find for string keys.
func (m *hmap) findstr(key string) unsafe.Pointer {
	hash := memhash(unsafe.Pointer(unsafe.StringData(key)), m.seed, uintptr(len(key)))
	h2 := hash & 0x7f
	g := (hash >> 7) & m.mask
	for i := uintptr(1); ; i++ {
		grp := unsafe.Add(m.groups, g*m.groupSize)
		ctrl := *(*uint64)(grp)
		for b := matchH2(ctrl, h2); b != 0; b &= b - 1 {
			slot := unsafe.Add(grp, 8+firstSlot(b)*m.slotSize)
			if k := *(*string)(slot); len(k) == len(key) &&
				(unsafe.StringData(k) == unsafe.StringData(key) || k == key) {
				return unsafe.Add(slot, m.elemOff)
			}
		}
		if matchEmpty(ctrl) != 0 {
			return nil
		}
		g = (g + i) & m.mask
	}
}

// lookup returns a pointer to the element for key k, or nil.
func (m *hmap) lookup(k unsafe.Pointer) unsafe.Pointer {
	if m.count == 0 {
		// Hash anyway so that unhashable interface keys panic, as in
		// the gc runtime.
		if m.keyKind == keyGeneric {
			m.hash(k)
		}
		return nil
	}
	switch m.keyKind {
	case keyMem64:
		return m.find64(*(*uint64)(k))
	case keyMem32:
		return m.find32(*(*uint32)(k))
	case keyString:
		return m.findstr(*(*string)(k))
	}
	hash := m.hash(k)
	grp, s := m.find(k, hash)
	if grp == nil {
		return nil
	}
	return m.slotElem(grp, s)
}

// zeroFor returns a pointer to a zero value of type t.
func zeroFor(t *_type) unsafe.Pointer {
	if t.size <= maxZero {
		return unsafe.Pointer(&zeroVal[0])
	}
	return mallocgc(t.size, t)
}

// mapaccess1 implements v := m[k]. It never returns nil: a missing key
// yields a pointer to a zero value that must not be written.
func mapaccess1(t *maptype, m *hmap, key unsafe.Pointer) unsafe.Pointer {
	if m == nil {
		mapNilHashCheck(t, key)
		return zeroFor(t.elem)
	}
	if e := m.lookup(key); e != nil {
		return e
	}
	return zeroFor(t.elem)
}

// mapaccess2 implements v, ok := m[k].
func mapaccess2(t *maptype, m *hmap, key unsafe.Pointer) (unsafe.Pointer, bool) {
	if m == nil {
		mapNilHashCheck(t, key)
		return zeroFor(t.elem), false
	}
	if e := m.lookup(key); e != nil {
		return e, true
	}
	return zeroFor(t.elem), false
}

// mapNilHashCheck panics for unhashable interface keys used on a nil map,
// as the gc runtime does.
func mapNilHashCheck(t *maptype, key unsafe.Pointer) {
	if t.key.kind == kindInterface {
		typehash(t.key, key, 0)
	}
}

// mapassign implements m[k] = v: it returns the slot where the caller
// stores the element. The key is stored by mapassign.
func mapassign(t *maptype, m *hmap, key unsafe.Pointer) unsafe.Pointer {
	if m == nil {
		panicnilmap()
	}
	hash := m.hash(key)
	for {
		if m.groups == nil {
			m.resize(1)
		}
		h2 := hash & 0x7f
		g := (hash >> 7) & m.mask
		var insGrp unsafe.Pointer
		var insSlot uintptr
		for i := uintptr(1); ; i++ {
			grp := m.group(g)
			ctrl := *ctrlOf(grp)
			for b := matchH2(ctrl, h2); b != 0; b &= b - 1 {
				s := firstSlot(b)
				k := m.slotKey(grp, s)
				if m.keyEqual(k, key) {
					if m.keyKind == keyGeneric && m.key.tflag&tflagRegularMemory == 0 {
						// Update the key, as gc does for keys whose
						// equal values can differ (+0 and -0, strings,
						// interfaces).
						typedmemmove(m.key, k, key)
					}
					return m.slotElem(grp, s)
				}
			}
			if insGrp == nil {
				if b := matchEmptyOrDeleted(ctrl); b != 0 {
					insGrp = grp
					insSlot = firstSlot(b)
				}
			}
			if matchEmpty(ctrl) != 0 {
				break
			}
			g = (g + i) & m.mask
		}
		// Not present. insGrp is set: the probe ended at a group with an
		// empty slot.
		wasEmpty := *(*uint8)(unsafe.Add(insGrp, insSlot)) == ctrlEmpty
		if wasEmpty && m.growthLeft == 0 {
			m.grow()
			continue
		}
		if wasEmpty {
			m.growthLeft--
		} else {
			m.tombstones--
		}
		setCtrl(insGrp, insSlot, uint8(h2))
		typedmemmove(m.key, m.slotKey(insGrp, insSlot), key)
		m.count++
		return m.slotElem(insGrp, insSlot)
	}
}

// grow rebuilds the table when it is full: twice as large, or the same
// size if at least half of the used slots are tombstones.
func (m *hmap) grow() {
	n := m.mask + 1
	if m.tombstones < m.count {
		n *= 2
	}
	m.resize(n)
}

// resize moves all entries into a new array of n groups.
func (m *hmap) resize(n uintptr) {
	oldGroups := m.groups
	oldN := uintptr(0)
	if oldGroups != nil {
		oldN = m.mask + 1
	}
	size := n * m.groupSize
	if m.noscan {
		m.groups = mallocgc(size, nil)
	} else {
		m.groups = mallocConservative(size)
	}
	for g := uintptr(0); g < n; g++ {
		*ctrlOf(m.group(g)) = lsbs * ctrlEmpty
	}
	m.mask = n - 1
	m.growthLeft = int(n * groupSlots * 7 / 8)
	m.tombstones = 0
	for g := uintptr(0); g < oldN; g++ {
		grp := unsafe.Add(oldGroups, g*m.groupSize)
		for b := matchFull(*ctrlOf(grp)); b != 0; b &= b - 1 {
			s := firstSlot(b)
			k := m.slotKey(grp, s)
			m.insertNew(k, m.slotElem(grp, s), m.hash(k))
		}
	}
}

// insertNew inserts an entry known to be absent into a table with room.
// It does not change count.
func (m *hmap) insertNew(k, e unsafe.Pointer, hash uintptr) {
	g := (hash >> 7) & m.mask
	for i := uintptr(1); ; i++ {
		grp := m.group(g)
		if b := matchEmptyOrDeleted(*ctrlOf(grp)); b != 0 {
			s := firstSlot(b)
			setCtrl(grp, s, uint8(hash&0x7f))
			typedmemmove(m.key, m.slotKey(grp, s), k)
			typedmemmove(m.elem, m.slotElem(grp, s), e)
			m.growthLeft--
			return
		}
		g = (g + i) & m.mask
	}
}

// mapdelete implements delete(m, k).
func mapdelete(t *maptype, m *hmap, key unsafe.Pointer) {
	if m == nil || m.count == 0 {
		if m == nil {
			mapNilHashCheck(t, key)
		} else if m.keyKind == keyGeneric {
			m.hash(key)
		}
		return
	}
	hash := m.hash(key)
	grp, s := m.find(key, hash)
	if grp == nil {
		return
	}
	// Clear the slot so the collector does not keep its referents.
	memclrNoHeapPointers(m.slotKey(grp, s), m.slotSize)
	if matchEmpty(*ctrlOf(grp)) != 0 {
		// Probes never continue past a group with an empty slot, so
		// this slot can become empty again.
		setCtrl(grp, s, ctrlEmpty)
		m.growthLeft++
	} else {
		setCtrl(grp, s, ctrlDeleted)
		m.tombstones++
	}
	m.count--
}

// maplen implements len(m).
func maplen(m *hmap) int {
	if m == nil {
		return 0
	}
	return m.count
}

// mapclear implements clear(m).
func mapclear(t *maptype, m *hmap) {
	if m == nil || m.groups == nil {
		return
	}
	n := m.mask + 1
	memclrNoHeapPointers(m.groups, n*m.groupSize)
	for g := uintptr(0); g < n; g++ {
		*ctrlOf(m.group(g)) = lsbs * ctrlEmpty
	}
	m.count = 0
	m.tombstones = 0
	m.growthLeft = int(n * groupSlots * 7 / 8)
	m.clearSeq++
	// A new seed makes NaN keys and collisions independent of the past.
	m.seed = uintptr(cheaprand64())
}

// mapiterinit starts an iteration over m. it must be zeroed.
func mapiterinit(t *maptype, m *hmap, it *hiter) {
	it.t = t
	it.m = m
	it.key = nil
	it.elem = nil
	if m == nil || m.count == 0 || m.groups == nil {
		return
	}
	it.groups = m.groups
	it.mask = m.mask
	it.start = uintptr(cheaprand64())
	it.i = 0
	it.clearSeq = m.clearSeq
	mapiternext(it)
}

// mapiternext advances it to the next entry; it.key is nil at the end.
func mapiternext(it *hiter) {
	m := it.m
	if m == nil || it.groups == nil {
		it.key = nil
		it.elem = nil
		return
	}
	total := (it.mask + 1) * groupSlots
	for it.i < total {
		j := (it.start + it.i) & (total - 1)
		it.i++
		grp := unsafe.Add(it.groups, (j>>3)*m.groupSize)
		s := j & 7
		c := *(*uint8)(unsafe.Add(grp, s))
		if c&ctrlEmpty != 0 {
			continue // empty or deleted
		}
		k := m.slotKey(grp, s)
		e := m.slotElem(grp, s)
		if it.groups != m.groups {
			// The table was rebuilt since the iteration started. Report
			// the entry only if it is still in the map, with its current
			// element.
			if !m.keyEqual(k, k) {
				// NaN keys cannot be looked up; they stay unless the
				// map was cleared.
				if it.clearSeq != m.clearSeq {
					continue
				}
			} else {
				cur := m.lookup(k)
				if cur == nil {
					continue
				}
				e = cur
			}
		}
		it.key = k
		it.elem = e
		return
	}
	it.key = nil
	it.elem = nil
}

// Fast entry points for maps with 4 byte, 8 byte and string keys, with the
// key passed by value. The compiler may use them when the key type is a
// 4 or 8 byte plain memory type (integers, pointers, chans; not floats) or
// a string, and the element is at most 1024 bytes. They are equivalent to
// the generic entry points.

func mapaccess1_fast32(t *maptype, m *hmap, key uint32) unsafe.Pointer {
	if m != nil && m.count != 0 {
		if e := m.find32(key); e != nil {
			return e
		}
	}
	return unsafe.Pointer(&zeroVal[0])
}

func mapaccess2_fast32(t *maptype, m *hmap, key uint32) (unsafe.Pointer, bool) {
	if m != nil && m.count != 0 {
		if e := m.find32(key); e != nil {
			return e, true
		}
	}
	return unsafe.Pointer(&zeroVal[0]), false
}

func mapassign_fast32(t *maptype, m *hmap, key uint32) unsafe.Pointer {
	return mapassign(t, m, unsafe.Pointer(&key))
}

func mapdelete_fast32(t *maptype, m *hmap, key uint32) {
	mapdelete(t, m, unsafe.Pointer(&key))
}

func mapaccess1_fast64(t *maptype, m *hmap, key uint64) unsafe.Pointer {
	if m != nil && m.count != 0 {
		if e := m.find64(key); e != nil {
			return e
		}
	}
	return unsafe.Pointer(&zeroVal[0])
}

func mapaccess2_fast64(t *maptype, m *hmap, key uint64) (unsafe.Pointer, bool) {
	if m != nil && m.count != 0 {
		if e := m.find64(key); e != nil {
			return e, true
		}
	}
	return unsafe.Pointer(&zeroVal[0]), false
}

func mapassign_fast64(t *maptype, m *hmap, key uint64) unsafe.Pointer {
	return mapassign(t, m, unsafe.Pointer(&key))
}

func mapdelete_fast64(t *maptype, m *hmap, key uint64) {
	mapdelete(t, m, unsafe.Pointer(&key))
}

func mapaccess1_faststr(t *maptype, m *hmap, key string) unsafe.Pointer {
	if m != nil && m.count != 0 {
		if e := m.findstr(key); e != nil {
			return e
		}
	}
	return zeroFor(t.elem)
}

func mapaccess2_faststr(t *maptype, m *hmap, key string) (unsafe.Pointer, bool) {
	if m != nil && m.count != 0 {
		if e := m.findstr(key); e != nil {
			return e, true
		}
	}
	return zeroFor(t.elem), false
}

func mapassign_faststr(t *maptype, m *hmap, key string) unsafe.Pointer {
	return mapassign(t, m, unsafe.Pointer(&key))
}

func mapdelete_faststr(t *maptype, m *hmap, key string) {
	mapdelete(t, m, unsafe.Pointer(&key))
}
