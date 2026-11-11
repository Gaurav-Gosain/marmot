package main

func main() {
	s := []int{1, 2, 3}
	i := 3
	defer func() {
		r := recover()
		println(r.(error).Error())
	}()
	println(s[i])
}
