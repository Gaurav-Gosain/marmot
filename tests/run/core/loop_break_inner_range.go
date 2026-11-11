package main

func main() {
	envs := []string{"A=1", "B=2", "C=3"}
	n := 0
	for _, s := range envs {
		for j := 0; j < len(s); j++ {
			if s[j] == '=' {
				n++
				break
			}
		}
	}
	println(n)
}
