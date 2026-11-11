package main

func main() {
	m := map[int]string{}
	for i := 0; i < 5000; i++ {
		m[i] = "v"
	}
	for i := 0; i < 5000; i += 3 {
		delete(m, i)
	}
	for i := 5000; i < 7000; i++ {
		m[i] = "w"
	}
	println(len(m))
	ok := true
	for i := 0; i < 7000; i++ {
		_, has := m[i]
		want := i >= 5000 || i%3 != 0
		if has != want {
			ok = false
		}
	}
	println(ok)
	sm := map[string]int{}
	for i := 0; i < 3000; i++ {
		sm[string(rune('a'+i%26))+string(rune('A'+i/26%26))+string(rune('0'+i/676))] = i
	}
	println(len(sm))
}
