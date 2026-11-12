// golang.org/x/sys/unix: libc calls through trampoline address variables
// on darwin, raw syscalls on linux.
package main

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func main() {
	fmt.Println("pid matches:", unix.Getpid() == os.Getpid())
	var u unix.Utsname
	if err := unix.Uname(&u); err != nil {
		fmt.Println("uname:", err)
	}
	fmt.Println("sysname set:", u.Sysname[0] != 0)
	var rl unix.Rlimit
	err := unix.Getrlimit(unix.RLIMIT_NOFILE, &rl)
	fmt.Println("getrlimit:", err, rl.Cur > 0, rl.Cur <= rl.Max)

	p := make([]int, 2)
	if err := unix.Pipe(p); err != nil {
		fmt.Println("pipe:", err)
		return
	}
	n, err := unix.Write(p[1], []byte("ping"))
	fmt.Println("write:", n, err)
	buf := make([]byte, 8)
	n, err = unix.Read(p[0], buf)
	fmt.Println("read:", string(buf[:n]), err)
	_, err = unix.IoctlGetWinsize(p[0], unix.TIOCGWINSZ)
	fmt.Println("winsize on a pipe is ENOTTY:", errors.Is(err, unix.ENOTTY))
	unix.Close(p[0])
	unix.Close(p[1])
	_, err = unix.Read(p[0], buf)
	fmt.Println("read after close is EBADF:", errors.Is(err, unix.EBADF))
}
