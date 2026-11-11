package main

func main() {
	i := 0
top:
	if i < 3 {
		println("i", i)
		i++
		goto top
	}
	n := 0
	goto skip
skip:
	n++
	if n < 2 {
		goto skip
	}
	println(n)
	for j := 0; j < 10; j++ {
		if j == 3 {
			goto out
		}
	}
	println("not here")
out:
	println("out")
	k := 0
retry:
	k++
	if k < 100 {
		goto retry
	}
	println(k)
}
