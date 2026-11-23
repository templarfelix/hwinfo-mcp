//go:build windows

package hwinfo

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// x/sys/windows has CreateFileMapping but no OpenFileMapping. Creating is not
// an option: it would bring an empty mapping into existence when HWiNFO is
// not running, and this package must never write anything.
var procOpenFileMapping = windows.NewLazySystemDLL("kernel32.dll").NewProc("OpenFileMappingW")

// tornReadRetries bounds how often an unlocked copy is repeated when HWiNFO
// rewrites the block while it is being taken.
const tornReadRetries = 3

func openFileMapping(access uint32, name string) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	h, _, callErr := procOpenFileMapping.Call(uintptr(access), 0, uintptr(unsafe.Pointer(p)))
	if h == 0 {
		if errno, ok := callErr.(syscall.Errno); ok && errno != 0 {
			return 0, errno
		}
		return 0, syscall.EINVAL
	}
	return windows.Handle(h), nil
}

// Read returns a private copy of the shared memory block, and whether the
// copy was taken while holding HWiNFO's mutex.
//
// The mapping is opened and mapped read-only and released again before Read
// returns, so nothing is kept open between calls and a HWiNFO restart needs
// no special handling.
func (r Reader) Read() (data []byte, locked bool, err error) {
	r = r.withDefaults()

	h, err := openFileMapping(windows.FILE_MAP_READ, r.Mapping)
	if err != nil {
		switch {
		case errors.Is(err, windows.ERROR_FILE_NOT_FOUND):
			return nil, false, fmt.Errorf("%w (%s)", ErrNotFound, r.Mapping)
		case errors.Is(err, windows.ERROR_ACCESS_DENIED):
			return nil, false, fmt.Errorf("%w (%s)", ErrAccessDenied, r.Mapping)
		}
		return nil, false, fmt.Errorf("hwinfo: open %s: %w", r.Mapping, err)
	}
	defer windows.CloseHandle(h)

	addr, err := windows.MapViewOfFile(h, windows.FILE_MAP_READ, 0, 0, 0)
	if err != nil {
		return nil, false, fmt.Errorf("hwinfo: map %s: %w", r.Mapping, err)
	}
	defer windows.UnmapViewOfFile(addr)

	// A view mapped with length 0 covers the whole section, whose size is not
	// otherwise known. VirtualQuery says how many bytes from addr are
	// actually committed and readable; nothing beyond that is touched.
	var info windows.MemoryBasicInformation
	if err := windows.VirtualQuery(addr, &info, unsafe.Sizeof(info)); err != nil {
		return nil, false, fmt.Errorf("hwinfo: query %s: %w", r.Mapping, err)
	}
	if info.State != windows.MEM_COMMIT || info.RegionSize == 0 {
		return nil, false, fmt.Errorf("%w: the mapped view of %s is not readable", ErrLayout, r.Mapping)
	}
	view := unsafe.Slice((*byte)(pointerAt(addr)), info.RegionSize)

	withMutex(r.Mutex, r.LockWait, func(held bool) {
		locked = held
		data, err = copyBlock(view, held)
	})
	return data, locked, err
}

// pointerAt turns an address handed out by the kernel into a pointer. Going
// through a pointer-to-pointer keeps "go vet" quiet about the conversion; it
// is sound because the address refers to a mapped view outside the Go heap,
// which the garbage collector neither moves nor frees.
func pointerAt(addr uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr))
}

// withMutex calls fn while holding the named mutex, if the mutex can be
// acquired within wait, and tells fn whether it is held. HWiNFO holds this
// mutex while it rewrites the block, so a copy taken under it cannot be torn
// by a concurrent poll.
//
// Not getting the mutex is no error: it may be missing, not openable from
// this integrity level, or held for longer than wait. fn then runs unlocked.
// It is also told the mutex is not held when the mutex was abandoned by its
// previous owner, because the block may then be half-written.
//
// fn runs on a goroutine of its own, because a Windows mutex belongs to the
// thread that acquired it and must be released by that same thread. The
// goroutine stays pinned to its thread from the wait until the release.
func withMutex(name string, wait time.Duration, fn func(held bool)) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread()

		m, owned, clean := acquireMutex(name, wait)
		fn(clean)
		if owned {
			err := windows.ReleaseMutex(m)
			windows.CloseHandle(m)
			if err != nil {
				// Releasing a mutex this thread owns should not fail. If it
				// ever does, the thread still owns it, so the thread stays
				// locked: the Go runtime normally destroys a locked thread
				// when its goroutine ends, and Windows then passes the
				// mutex on as abandoned. Handing the thread back to the
				// pool would keep HWiNFO waiting for it for certain.
				return
			}
		}
		runtime.UnlockOSThread()
	}()
	<-done
}

// acquireMutex opens the named mutex and waits for it. It must be called on
// a goroutine that is locked to its thread.
//
// owned reports whether this thread now holds the mutex and has to release
// it. clean reports whether holding it also guarantees a consistent block:
// that is not so for an abandoned mutex, whose previous owner ended while
// holding it and may have left the block half-written.
func acquireMutex(name string, wait time.Duration) (m windows.Handle, owned, clean bool) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, false, false
	}
	// SYNCHRONIZE is all it takes to wait for a mutex and to release it.
	m, err = windows.OpenMutex(windows.SYNCHRONIZE, false, p)
	if err != nil {
		return 0, false, false
	}
	event, err := windows.WaitForSingleObject(m, waitMillis(wait))
	switch {
	case err == nil && event == windows.WAIT_OBJECT_0:
		return m, true, true
	case err == nil && event == windows.WAIT_ABANDONED:
		return m, true, false
	}
	windows.CloseHandle(m)
	return 0, false, false
}

// waitMillis converts a wait to the milliseconds WaitForSingleObject takes.
// It rounds up, so that a short positive wait does not become none at all,
// and stays clear of 0xFFFFFFFF, which means "wait forever".
func waitMillis(d time.Duration) uint32 {
	const longest = 60 * time.Second
	d = min(max(d, 0), longest)
	return uint32((d + time.Millisecond - 1) / time.Millisecond)
}

// copyBlock copies the used part of the mapped view into Go memory.
//
// Without the mutex HWiNFO may be in the middle of a poll. The copy is then
// compared with the block as it reads right afterwards: HWiNFO rewrites the
// values on every poll, so a block that still reads the same was not being
// written. If it differs, the copy is retried a few times.
//
// The view is memory this process does not control. Should reading it ever
// fault, the fault is reported as an error instead of ending the process.
func copyBlock(view []byte, locked bool) (data []byte, err error) {
	defer debug.SetPanicOnFault(debug.SetPanicOnFault(true))
	defer func() {
		if p := recover(); p != nil {
			fault, ok := p.(interface{ Addr() uintptr })
			if !ok {
				panic(p)
			}
			data, err = nil, fmt.Errorf("hwinfo: fault at %#x while reading shared memory", fault.Addr())
		}
	}()

	for attempt := 0; ; attempt++ {
		n, err := blockSize(view)
		if err != nil {
			return nil, err
		}
		data := bytes.Clone(view[:n])
		if locked || attempt >= tornReadRetries || bytes.Equal(data, view[:n]) {
			return data, nil
		}
	}
}
