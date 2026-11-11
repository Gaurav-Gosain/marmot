package main

func main() {
	a := make([]int32, 1000)
	for i := range a {
		a[i] = int32(i*1103515245 + 12345)
	}
	var s32 int32
	var s64 int64
	var u8 uint8
	var x, o uint32 = 0, 0
	var mn, mx int32 = 1 << 30, -1 << 30
	for _, v := range a {
		s32 += v
		s64 += int64(v)
		u8 += uint8(v)
		x ^= uint32(v)
		o |= uint32(v)
		mn = min(mn, v)
		mx = max(mx, v)
	}
	println(s32, s64, u8, x, o, mn, mx)
	var prod uint64 = 1
	for i := 1; i < 40; i++ {
		prod *= uint64(i)
	}
	println(prod)
	cnt := 0
	for _, v := range a {
		if v > 0 && v%3 == 0 {
			cnt++
		}
	}
	println(cnt)
	var h uint32 = 2166136261
	for i := range a {
		h = (h ^ uint32(a[i])) * 16777619
	}
	println(h)
}
