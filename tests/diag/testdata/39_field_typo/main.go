package main

type Config struct {
	Port int
	Host string
}

func main() {
	c := Config{}
	c.Prot = 80
	println(c.Host)
}
