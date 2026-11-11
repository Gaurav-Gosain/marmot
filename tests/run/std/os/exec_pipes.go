package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

func main() {
	cmd := exec.Command("sh", "-c", "read a; read b; echo \"$a+$b\"; echo err >&2; exit 4")
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	var se bytes.Buffer
	cmd.Stderr = &se
	fmt.Println(cmd.Start())
	io.WriteString(in, "x\ny\n")
	in.Close()
	b, _ := io.ReadAll(out)
	err := cmd.Wait()
	fmt.Printf("%q %q %v %d\n", b, se.String(), err, cmd.ProcessState.ExitCode())
	c2 := exec.Command("sh", "-c", "echo $MARMOT_X:$PWD")
	c2.Env = []string{"MARMOT_X=custom"}
	c2.Dir = os.TempDir()
	o2, _ := c2.Output()
	fmt.Println(strings.HasPrefix(string(o2), "custom:/"))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = exec.CommandContext(ctx, "sleep", "5").Run()
	fmt.Println(err != nil, time.Since(start) < 3*time.Second)
	c3 := exec.Command("sh", "-c", "for i in 1 2 3; do echo line$i; done")
	o3, _ := c3.StdoutPipe()
	c3.Start()
	sc := bufio.NewScanner(o3)
	n := 0
	for sc.Scan() {
		n++
	}
	c3.Wait()
	fmt.Println(n)
	c4 := exec.Command("sh", "-c", "kill -9 $$")
	err = c4.Run()
	ee := err.(*exec.ExitError)
	fmt.Println(ee.ExitCode(), ee.Exited(), ee.String())
	_, err = exec.Command("sh", "-c", "exit 0").CombinedOutput()
	fmt.Println(err)
	fmt.Println(exec.Command("echo", "a", "b").String())
}
