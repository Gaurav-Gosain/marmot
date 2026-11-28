package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeProc writes a /proc with pids, each "comm ticks cgroup cmdline", and a vmstat.
func fakeProc(t *testing.T, dir string, pids map[int][4]string, swpin, swpout int) {
	t.Helper()
	for pid, p := range pids {
		d := filepath.Join(dir, strconv.Itoa(pid))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		// utime is field 14, stime 15: the ticks go to utime, stime is 0.
		stat := strconv.Itoa(pid) + " (" + p[0] + ") S 1 1 1 0 -1 0 0 0 0 0 " + p[1] + " 0 0 0 20 0 1 0 100 0 0\n"
		for f, s := range map[string]string{"stat": stat, "cgroup": "0::" + p[2] + "\n", "cmdline": p[3]} {
			if err := os.WriteFile(filepath.Join(d, f), []byte(s), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	vm := "nr_free_pages 1\npswpin " + strconv.Itoa(swpin) + "\npswpout " + strconv.Itoa(swpout) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "vmstat"), []byte(vm), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOutsideLines(t *testing.T) {
	dir := t.TempDir()
	defer func(r string) { procRoot = r }(procRoot)
	procRoot = dir
	own := "/user.slice/marmot-dx-job.service"
	long := "/usr/bin/clang\x00-O2\x00-c\x00" + strings.Repeat("x", 80)
	before := map[int][4]string{
		10: {"perfcmp", "100", own, "perfcmp\x00-modes\x00dev"},
		11: {"matmul", "50", own + "/run", "./matmul"},
		20: {"clang", "1000", "/user.slice/marmot-frontend-b4e.service", long},
		21: {"go (build)", "500", "/user.slice/app.slice/gopls.service", "gopls\x00serve"},
		22: {"idle", "7", "/user.slice/idle.service", "idle"},
		30: {"kswapd0", "40", "/", ""},
	}
	fakeProc(t, dir, before, 100, 200)
	prev := takeSnap()
	after := map[int][4]string{
		10: {"perfcmp", "900", own, "perfcmp\x00-modes\x00dev"}, // own job: left out
		11: {"matmul", "2050", own + "/run", "./matmul"},        // below own: left out
		20: {"clang", "1800", "/user.slice/marmot-frontend-b4e.service", long},
		21: {"go (build)", "700", "/user.slice/app.slice/gopls.service", "gopls\x00serve"},
		22: {"idle", "7", "/user.slice/idle.service", "idle"}, // no CPU: left out
		30: {"kswapd0", "140", "/", ""},
		40: {"new", "30", "/system.slice/cron.service", "backup.sh"}, // new pid: all its ticks
	}
	fakeProc(t, dir, after, 150, 4200)
	cur := takeSnap()
	cur.t = prev.t.Add(10 * time.Second)
	got := outsideLines(prev, cur, "load 9.1, above 8", own)
	want := []string{
		"outside load (load 9.1, above 8; the last 10 s): kswapd 10.0% of one CPU, swap in 50 pages, swap out 4000 pages; the top CPU users outside this job:",
		"  pid 20        80.0%  marmot-frontend-b4e.service              /usr/bin/clang -O2 -c " + strings.Repeat("x", 38),
		"  pid 21        20.0%  gopls.service                            gopls serve",
		"  pid 30        10.0%  -                                        [kswapd0]",
		"  pid 40         3.0%  cron.service                             backup.sh",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if l := outsideLines(prev, prev, "x", own); l != nil {
		t.Errorf("an interval of 0 s gave %q, want nothing", l)
	}
}

func TestOutsideTop5(t *testing.T) {
	dir := t.TempDir()
	defer func(r string) { procRoot = r }(procRoot)
	procRoot = dir
	pids := map[int][4]string{}
	for i := range 8 {
		pids[100+i] = [4]string{"p", "0", "/u/a.service", "p"}
	}
	fakeProc(t, dir, pids, 0, 0)
	prev := takeSnap()
	for i := range 8 {
		pids[100+i] = [4]string{"p", strconv.Itoa(10 * (i + 1)), "/u/a.service", "p"}
	}
	fakeProc(t, dir, pids, 0, 0)
	cur := takeSnap()
	cur.t = prev.t.Add(time.Second)
	got := outsideLines(prev, cur, "x", "/u/own.service")
	if len(got) != 6 || !strings.HasPrefix(got[1], "  pid 107 ") || !strings.HasPrefix(got[5], "  pid 103 ") {
		t.Errorf("want the header and pids 107 down to 103, got %q", got)
	}
}

// TestOutsideLive reads the real /proc: this test process is inside its own cgroup, so it
// never names itself.
func TestOutsideLive(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/proc is Linux only")
	}
	prev := takeSnap()
	if prev == nil || len(prev.ticks) == 0 {
		t.Fatal("no processes in /proc")
	}
	for end := time.Now().Add(700 * time.Millisecond); time.Now().Before(end); {
	} // burn CPU in this process
	cur := takeSnap()
	own := ownCgroup()
	if own == "" {
		t.Skip("no cgroup v2 path for this process")
	}
	me := "pid " + strconv.Itoa(os.Getpid()) + " "
	for _, l := range outsideLines(prev, cur, "live", own) {
		if strings.Contains(l, me) {
			t.Errorf("the report names this process, which is inside its own cgroup: %s", l)
		}
	}
}
