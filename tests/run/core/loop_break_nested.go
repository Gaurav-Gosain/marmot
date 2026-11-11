package main

// An inner break or continue must only affect the inner loop, for every combination of loop kinds.
func main() {
	envs := []string{"A=1", "B=22", "C=333", "D"}
	n := 0
	for _, s := range envs {
		for j := 0; j < len(s); j++ {
			if s[j] == '=' {
				n++
				break
			}
		}
	}
	println("slice/for", n)
	n = 0
	for i := range envs {
		for _, c := range envs[i] {
			if c == '=' {
				n += i
				break
			}
		}
	}
	println("slice/range-string", n)
	n = 0
	for _, s := range "ab,cd,ef" {
		for k := range 5 {
			if k == 2 {
				break
			}
			n += int(s)
		}
	}
	println("string/range-int", n)
	n = 0
	for i := range 4 {
		for _, v := range []int{1, 2, 3, 4} {
			if v > i {
				break
			}
			n += v
		}
		n += 100
	}
	println("int/range-slice", n)
	m := map[int]int{1: 1, 2: 2, 3: 3}
	n = 0
	for range m {
		for k := 0; ; k++ {
			if k == 3 {
				break
			}
			n++
		}
	}
	println("map/for", n)
	n = 0
	for i := 0; i < 5; i++ {
		for j := 0; j < 5; j++ {
			if j == 2 {
				continue
			}
			if j == 4 {
				break
			}
			n++
		}
		n += 10
	}
	println("for/for", n)
	n = 0
	for _, s := range envs {
		switch {
		case len(s) > 2:
			for _, c := range s {
				if c == '=' {
					break
				}
				n++
			}
		default:
			n += 100
		}
	}
	println("range/switch/range", n)
	n = 0
	for _, a := range []int{1, 2, 3} {
		for _, b := range []int{1, 2, 3} {
			for _, c := range []int{1, 2, 3} {
				if c == 2 {
					break
				}
				n += a*100 + b*10 + c
			}
			if b == 2 {
				break
			}
		}
	}
	println("triple", n)
	ch := make(chan int, 3)
	ch <- 1
	ch <- 2
	ch <- 3
	close(ch)
	n = 0
	for v := range ch {
		for k := 0; k < 10; k++ {
			if k > v {
				break
			}
			n++
		}
	}
	println("chan/for", n)
	n = 0
	seq := func(yield func(int) bool) {
		for i := 0; i < 3; i++ {
			if !yield(i) {
				return
			}
		}
	}
	for i := range seq {
		for j := range seq {
			if j == 1 {
				break
			}
			n += i + j + 1
		}
	}
	println("func/func", n)
	cnt := 0
outer:
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			for k := 0; k < 3; k++ {
				if k == 1 {
					continue outer
				}
				cnt++
			}
		}
	}
	println("labeled", cnt)
}
