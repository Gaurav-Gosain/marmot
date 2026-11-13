package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/Gaurav-Gosain/marmot/internal/driver"
	"github.com/Gaurav-Gosain/marmot/internal/play"
)

type playFlags struct {
	addr    *string
	open    *bool
	service *bool
	origin  *string
}

func newPlayFlags() (*flag.FlagSet, *playFlags) {
	fs := flag.NewFlagSet("play", flag.ContinueOnError)
	f := &playFlags{}
	f.addr = fs.String("addr", "127.0.0.1:7375", "address to serve on (keep it on the loopback: programs run unconfined)")
	f.open = fs.Bool("open", false, "open the page in the browser")
	f.service = fs.Bool("service", false, "serve only the compile API (POST /compile, GET /healthz): no page, programs are never run")
	f.origin = fs.String("allow-origin", "", "with -service: the Access-Control-Allow-Origin of /compile")
	return fs, f
}

const playSample = `package main

import "fmt"

// Sum adds the numbers. Hover a line to see what it becomes at each stage.
func Sum(xs []int32) int32 {
	var t int32
	for _, x := range xs {
		t += x
	}
	return t
}

func main() {
	xs := make([]int32, 1000)
	for i := range xs {
		xs[i] = int32(i)
	}
	fmt.Println("sum:", Sum(xs))
}
`

// cmdPlay implements marmot play [dir]: the playground on this machine.
func cmdPlay(args []string) int {
	fs, pf := newPlayFlags()
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return usageError("play", err)
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "usage: marmot play [-addr HOST:PORT] [-open] [dir or file.go]")
		return 2
	}
	initial := playSample
	if fs.NArg() == 1 {
		p := fs.Arg(0)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			p = filepath.Join(p, "main.go")
		}
		data, err := os.ReadFile(p)
		if err != nil {
			fmt.Fprintln(os.Stderr, "marmot play:", err)
			return 1
		}
		initial = string(data)
	}
	root, err := driver.FindRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "marmot play:", err)
		return 2
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "marmot play:", err)
		return 1
	}
	srv := play.New(play.Options{Marmot: exe, Root: root, Initial: initial, Local: !*pf.service, AllowOrigin: *pf.origin})
	ln, err := net.Listen("tcp", *pf.addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "marmot play:", err)
		return 1
	}
	url := "http://" + ln.Addr().String() + "/"
	if *pf.service {
		fmt.Printf("marmot play: compile service on %scompile\n", url)
	} else {
		fmt.Printf("marmot play on %s (Ctrl-C stops it)\n", url)
	}
	if *pf.open && !*pf.service {
		opener := "xdg-open"
		if runtime.GOOS == "darwin" {
			opener = "open"
		}
		_ = exec.Command(opener, url).Start() // best effort: the URL is printed
	}
	if err := http.Serve(ln, srv.Handler()); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, "marmot play:", err)
		return 1
	}
	return 0
}
