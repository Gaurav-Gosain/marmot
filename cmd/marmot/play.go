package main

import (
	"context"
	"encoding/json"
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
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/driver"
	"github.com/Gaurav-Gosain/marmot/internal/play"
)

type playFlags struct {
	addr    *string
	open    *bool
	service *bool
	origin  *string
	json    *bool
	mode    *string
	stages  *string
}

func newPlayFlags() (*flag.FlagSet, *playFlags) {
	fs := flag.NewFlagSet("play", flag.ContinueOnError)
	f := &playFlags{}
	f.addr = fs.String("addr", "127.0.0.1:7375", "address to serve on (keep it on the loopback: programs run unconfined)")
	f.open = fs.Bool("open", false, "open the page in the browser")
	f.service = fs.Bool("service", false, "serve only the compile API (POST /compile, GET /healthz): no page, programs are never run")
	f.origin = fs.String("allow-origin", "", "with -service: the Access-Control-Allow-Origin of /compile")
	f.json = fs.Bool("json", false, "do not serve: compile the file (or the .go files of the directory) once and print the compile service's JSON reply, stages and line maps included")
	f.mode = fs.String("mode", "O2", "with -json: dev, O2 or release")
	f.stages = fs.String("stages", strings.Join(play.StageNames, ","), "with -json: the stages, comma separated")
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
		return argError((outputFlags{}).reporter("play"), "play", "marmot play [-addr HOST:PORT] [-open] [dir or file.go]", fs.Args(), "dir")
	}
	if *pf.json {
		return playJSON(fs.Args(), *pf.mode, *pf.stages)
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

// playJSON compiles one program the way the compile service does and
// prints its reply: what the docs capture for the playground's examples.
func playJSON(args []string, mode, stages string) int {
	rep := (outputFlags{}).reporter("play")
	if len(args) != 1 {
		return argError(rep, "play", "marmot play -json [-mode M] [-stages LIST] FILE.go|DIR", args, "FILE.go")
	}
	q := &play.Request{Mode: mode}
	if stages != "" {
		q.Stages = strings.Split(stages, ",")
	}
	p := args[0]
	if st, err := os.Stat(p); err == nil && st.IsDir() {
		names, _ := filepath.Glob(filepath.Join(p, "*.go"))
		var b strings.Builder
		for _, n := range names {
			if strings.HasSuffix(n, "_test.go") {
				continue
			}
			data, err := os.ReadFile(n)
			if err != nil {
				fmt.Fprintln(os.Stderr, "marmot play:", err)
				return 1
			}
			fmt.Fprintf(&b, "-- %s --\n%s", filepath.Base(n), data)
		}
		q.Txtar = b.String()
	} else {
		data, err := os.ReadFile(p)
		if err != nil {
			fmt.Fprintln(os.Stderr, "marmot play:", err)
			return 1
		}
		q.Source = string(data)
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
	srv := play.New(play.Options{Marmot: exe, Root: root})
	res, err := srv.Compile(context.Background(), q)
	if err != nil {
		return rep.Usage("marmot play -json: " + err.Error())
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		fmt.Fprintln(os.Stderr, "marmot play:", err)
		return 1
	}
	if !res.OK {
		return 1
	}
	return 0
}
