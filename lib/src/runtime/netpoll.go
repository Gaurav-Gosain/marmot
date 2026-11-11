// internal/poll hooks on the C core's I/O poller (rt_poll_*). A pollable
// descriptor (pipe, socket, terminal) is non-blocking and registered once;
// internal/poll retries reads and writes until EAGAIN and then waits here,
// which parks only the goroutine. Compiled only by marmot.

package runtime

import "unsafe"

//go:linkname rt_poll_open rt_poll_open
func rt_poll_open(fd int32) unsafe.Pointer

//go:linkname rt_poll_close rt_poll_close
func rt_poll_close(pd unsafe.Pointer)

//go:linkname rt_poll_wait rt_poll_wait
func rt_poll_wait(pd unsafe.Pointer, mode int32, deadline int64) int32

//go:linkname rt_poll_evict rt_poll_evict
func rt_poll_evict(pd unsafe.Pointer)

//go:linkname rt_poll_wake rt_poll_wake
func rt_poll_wake(pd unsafe.Pointer, mode int32)

// Error codes returned to internal/poll (see internal/poll/fd_poll_runtime.go).
const (
	pollNoError        = 0
	pollErrClosing     = 1
	pollErrTimeout     = 2
	pollErrNotPollable = 3
)

// pollDesc is the runtime side of an internal/poll pollDesc. internal/poll
// keeps it as a uintptr, so all live descriptors are also on pollList to
// keep them reachable.
type pollDesc struct {
	link    *pollDesc
	prev    *pollDesc
	pd      unsafe.Pointer // C core handle
	lock    mutex
	closing bool
	rd      int64 // read deadline: monotonic ns, 0 none, -1 expired
	wd      int64 // write deadline
}

var (
	pollListLock mutex
	pollList     *pollDesc
)

//go:linkname poll_runtime_pollServerInit internal/poll.runtime_pollServerInit
func poll_runtime_pollServerInit() {}

//go:linkname poll_runtime_pollOpen internal/poll.runtime_pollOpen
func poll_runtime_pollOpen(fd uintptr) (uintptr, int) {
	h := rt_poll_open(int32(fd))
	if h == nil {
		return 0, _ENOTSUP
	}
	p := (*pollDesc)(mallocConservative(unsafe.Sizeof(pollDesc{})))
	p.pd = h
	lock(&pollListLock)
	p.link = pollList
	if pollList != nil {
		pollList.prev = p
	}
	pollList = p
	unlock(&pollListLock)
	return uintptr(unsafe.Pointer(p)), 0
}

//go:linkname poll_runtime_pollClose internal/poll.runtime_pollClose
func poll_runtime_pollClose(ctx uintptr) {
	p := (*pollDesc)(unsafe.Pointer(ctx))
	rt_poll_close(p.pd)
	lock(&pollListLock)
	if p.prev != nil {
		p.prev.link = p.link
	} else {
		pollList = p.link
	}
	if p.link != nil {
		p.link.prev = p.prev
	}
	p.link = nil
	p.prev = nil
	unlock(&pollListLock)
}

// deadline returns the deadline for mode ('r' or 'w').
func (p *pollDesc) deadline(mode int) int64 {
	if mode == 'r' {
		return p.rd
	}
	return p.wd
}

// check reports closing or an expired deadline for mode.
func (p *pollDesc) check(mode int) int {
	lock(&p.lock)
	closing := p.closing
	dl := p.deadline(mode)
	unlock(&p.lock)
	if closing {
		return pollErrClosing
	}
	if dl < 0 || dl > 0 && dl <= rt_nanotime() {
		return pollErrTimeout
	}
	return pollNoError
}

//go:linkname poll_runtime_pollReset internal/poll.runtime_pollReset
func poll_runtime_pollReset(ctx uintptr, mode int) int {
	return (*pollDesc)(unsafe.Pointer(ctx)).check(mode)
}

//go:linkname poll_runtime_pollWait internal/poll.runtime_pollWait
func poll_runtime_pollWait(ctx uintptr, mode int) int {
	p := (*pollDesc)(unsafe.Pointer(ctx))
	for {
		if errcode := p.check(mode); errcode != pollNoError {
			return errcode
		}
		lock(&p.lock)
		dl := p.deadline(mode)
		unlock(&p.lock)
		switch rt_poll_wait(p.pd, int32(mode), dl) {
		case 0:
			return pollNoError
		case 1:
			// Timed out, unless the deadline moved while we waited.
			if p.check(mode) == pollNoError {
				continue
			}
			return pollErrTimeout
		case 3:
			// Woken by a deadline change: check the new deadline.
			continue
		default:
			return pollErrClosing
		}
	}
}

//go:linkname poll_runtime_pollWaitCanceled internal/poll.runtime_pollWaitCanceled
func poll_runtime_pollWaitCanceled(ctx uintptr, mode int) {}

// poll_runtime_pollSetDeadline sets the read ('r'), write ('w') or both
// ('r'+'w') deadlines. d is relative: 0 means none, negative means already
// expired. A goroutine already waiting is woken to apply the new
// deadline.
//
//go:linkname poll_runtime_pollSetDeadline internal/poll.runtime_pollSetDeadline
func poll_runtime_pollSetDeadline(ctx uintptr, d int64, mode int) {
	p := (*pollDesc)(unsafe.Pointer(ctx))
	if d > 0 {
		d += rt_nanotime()
		if d <= 0 {
			d = 1<<63 - 1 // overflow: effectively no deadline
		}
	} else if d < 0 {
		d = -1
	}
	lock(&p.lock)
	if mode == 'r' || mode == 'r'+'w' {
		p.rd = d
	}
	if mode == 'w' || mode == 'r'+'w' {
		p.wd = d
	}
	unlock(&p.lock)
	if mode == 'r' || mode == 'r'+'w' {
		rt_poll_wake(p.pd, 'r')
	}
	if mode == 'w' || mode == 'r'+'w' {
		rt_poll_wake(p.pd, 'w')
	}
}

//go:linkname poll_runtime_pollUnblock internal/poll.runtime_pollUnblock
func poll_runtime_pollUnblock(ctx uintptr) {
	p := (*pollDesc)(unsafe.Pointer(ctx))
	lock(&p.lock)
	p.closing = true
	unlock(&p.lock)
	rt_poll_evict(p.pd)
}

//go:linkname poll_runtime_isPollServerDescriptor internal/poll.runtime_isPollServerDescriptor
func poll_runtime_isPollServerDescriptor(fd uintptr) bool { return false }
