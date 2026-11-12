package main

type User struct {
	Name string
	Age  int
}

func main() {
	u := User{Name: "a", Age: "ten"}
	println(u.Name)
}
