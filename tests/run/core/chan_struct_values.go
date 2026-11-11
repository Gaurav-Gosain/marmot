package main

type Msg struct {
	ID   int
	Text string
	Pad  [8]int
}

func main() {
	c := make(chan Msg, 2)
	c <- Msg{1, "one", [8]int{7: 7}}
	c <- Msg{ID: 2, Text: "two"}
	a := <-c
	b := <-c
	println(a.ID, a.Text, a.Pad[7], b.ID, b.Text)
	e := make(chan struct{})
	go func() { close(e) }()
	<-e
	println("closed")
	ic := make(chan interface{}, 3)
	ic <- 1
	ic <- "s"
	ic <- nil
	println(<-ic == 1, (<-ic).(string), <-ic == nil)
}
