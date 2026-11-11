package main

func main() {
	s := make([]int, 12)
	for i := range s {
		s[i] = i
	}
	copy(s[2:], s)
	println(s[0], s[1], s[2], s[3], s[11])
	for i := range s {
		s[i] = i
	}
	copy(s, s[3:])
	println(s[0], s[8], s[9], s[11])
	b := []byte("0123456789abcdef")
	copy(b[1:], b)
	println(string(b))
	b = []byte("0123456789abcdef")
	copy(b, b[5:])
	println(string(b))
	for _, n := range []int{0, 1, 7, 8, 9, 15, 16, 17, 31, 33, 64, 65, 255} {
		x := make([]byte, 300)
		for i := range x {
			x[i] = byte(i)
		}
		copy(x[10:], x[:n])
		y := make([]byte, 300)
		for i := range y {
			y[i] = byte(i)
		}
		tmp := append([]byte(nil), y[:n]...)
		copy(y[10:], tmp)
		same := true
		for i := range x {
			if x[i] != y[i] {
				same = false
			}
		}
		println(n, same)
	}
	arr := [8]int{1, 2, 3, 4, 5, 6, 7, 8}
	copy(arr[3:], arr[:])
	println(arr[0], arr[3], arr[4], arr[7])
	st := []string{"a", "b", "c", "d", "e"}
	copy(st[1:], st)
	println(st[0], st[1], st[2], st[4])
}
