package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

func main() {
	out, err := exec.Command("echo", "hello", "world").Output()
	fmt.Printf("%q %v\n", out, err)
	cmd := exec.Command("sh", "-c", "echo out; echo err >&2; exit 3")
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err = cmd.Run()
	ee, ok := err.(*exec.ExitError)
	fmt.Println(so.String(), se.String(), ok, ee.ExitCode(), err)
	c2 := exec.Command("cat")
	c2.Stdin = strings.NewReader("piped input\n")
	o2, err := c2.CombinedOutput()
	fmt.Printf("%q %v\n", o2, err)
	_, err = exec.Command("/nonexistent/prog").Output()
	fmt.Println(err != nil)
	_, err = exec.LookPath("definitely-not-a-program-zz")
	fmt.Println(err != nil)
	p, err := exec.LookPath("sh")
	fmt.Println(p != "", err)
}
