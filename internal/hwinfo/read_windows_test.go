//go:build windows

package hwinfo

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// These tests exercise the Windows reader against kernel objects of their
// own, under session-local names that are unique to the test process. They
// never touch Global\HWiNFO_*: HWiNFO is running on some machines that run
// the tests and absent on others, and neither may change the outcome.

var objectSeq atomic.Uint32

func objectName(kind string) string {
	return fmt.Sprintf(`Local\hwinfo-mcp-test-%s-%d-%d`, kind, os.Getpid(), objectSeq.Add(1))
}

// publish creates a named, pagefile-backed mapping of size bytes that starts
// with block, the way HWiNFO publishes its sensors, and returns its name.
func publish(t *testing.T, block []byte, size int) string {
	t.Helper()
	name := objectName("mapping")
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFileMapping(windows.InvalidHandle, nil, windows.PAGE_READWRITE, 0, uint32(size), p)
	if err != nil {
		t.Fatalf("CreateFileMapping: %v", err)
	}
	t.Cleanup(func() { windows.CloseHandle(h) })

	addr, err := windows.MapViewOfFile(h, windows.FILE_MAP_WRITE, 0, 0, uintptr(size))
	if err != nil {
		t.Fatalf("MapViewOfFile: %v", err)
	}
	copy(unsafe.Slice((*byte)(pointerAt(addr)), size), block)
	if err := windows.UnmapViewOfFile(addr); err != nil {
		t.Fatalf("UnmapViewOfFile: %v", err)
	}
	return name
}

// newMutex creates a named mutex nobody owns and returns its name and handle.
func newMutex(t *testing.T) (string, windows.Handle) {
	t.Helper()
	name := objectName("mutex")
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		t.Fatal(err)
	}
	m, err := windows.CreateMutex(nil, false, p)
	if err != nil {
		t.Fatalf("CreateMutex: %v", err)
	}
	t.Cleanup(func() { windows.CloseHandle(m) })
	return name, m
}

var procNtQueryMutant = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtQueryMutant")

// mutexOwned reports whether any thread currently owns the mutex. It asks the
// kernel rather than trying to acquire the mutex: an acquisition attempt
// succeeds for the owning thread itself, and goroutines share threads.
func mutexOwned(t *testing.T, m windows.Handle) bool {
	t.Helper()
	var info struct {
		CurrentCount  int32 // 1 when free, 0 or less when owned
		OwnedByCaller uint8
		Abandoned     uint8
	}
	status, _, _ := procNtQueryMutant.Call(uintptr(m), 0, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info), 0)
	if status != 0 {
		t.Fatalf("NtQueryMutant: NTSTATUS %#x", status)
	}
	return info.CurrentCount <= 0
}

// abandon acquires the mutex on a thread that then ends without releasing it,
// which is what a HWiNFO that dies in the middle of a poll leaves behind.
func abandon(t *testing.T, m windows.Handle) {
	t.Helper()
	// The thread is created through Windows directly, not by the Go runtime,
	// so that it is certain to end when its function returns.
	body := windows.NewCallback(func(uintptr) uintptr {
		windows.WaitForSingleObject(m, windows.INFINITE)
		return 0
	})
	thread, _, err := procCreateThread.Call(0, 0, body, 0, 0, 0)
	if thread == 0 {
		t.Fatalf("CreateThread: %v", err)
	}
	defer windows.CloseHandle(windows.Handle(thread))
	if event, err := windows.WaitForSingleObject(windows.Handle(thread), 10_000); err != nil || event != windows.WAIT_OBJECT_0 {
		t.Fatalf("waiting for the thread to end: event %#x, %v", event, err)
	}
	if mutexOwned(t, m) {
		t.Fatal("the mutex is still owned after its owner's thread ended")
	}
}

var procCreateThread = windows.NewLazySystemDLL("kernel32.dll").NewProc("CreateThread")

// hold acquires the mutex on a thread of its own, as HWiNFO does during a
// poll, and keeps it until release is called.
func hold(t *testing.T, m windows.Handle) (release func()) {
	t.Helper()
	acquired := make(chan error, 1)
	letGo := make(chan struct{})
	released := make(chan struct{})
	go func() {
		defer close(released)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if _, err := windows.WaitForSingleObject(m, windows.INFINITE); err != nil {
			acquired <- err
			return
		}
		acquired <- nil
		<-letGo
		windows.ReleaseMutex(m)
	}()
	if err := <-acquired; err != nil {
		t.Fatalf("holding the mutex: %v", err)
	}
	return func() {
		close(letGo)
		<-released
	}
}

func TestReaderCopiesTheBlockUnderTheMutex(t *testing.T) {
	block := sample(2).Bytes()
	mutexName, mutex := newMutex(t)
	r := Reader{Mapping: publish(t, block, 64<<10), Mutex: mutexName}

	data, locked, err := r.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !locked {
		t.Error("locked = false although the mutex was free")
	}
	// The mapping is 64 KiB; only the part the header describes is copied.
	if !bytes.Equal(data, block) {
		t.Errorf("copied %d bytes that differ from the %d published", len(data), len(block))
	}
	if mutexOwned(t, mutex) {
		t.Fatal("Read left the mutex owned; HWiNFO would stop updating")
	}

	// Reading again proves the release worked for real: an owned mutex
	// could only be taken again by the very thread that holds it.
	for i := range 20 {
		if _, locked, err := r.Read(); err != nil || !locked {
			t.Fatalf("read %d: locked = %v, err = %v", i, locked, err)
		}
	}
	if mutexOwned(t, mutex) {
		t.Fatal("repeated reads left the mutex owned")
	}
}

func TestReaderFallsBackWhenTheMutexIsBusy(t *testing.T) {
	block := sample(2).Bytes()
	mutexName, mutex := newMutex(t)
	r := Reader{Mapping: publish(t, block, 64<<10), Mutex: mutexName, LockWait: 30 * time.Millisecond}

	release := hold(t, mutex)
	start := time.Now()
	data, locked, err := r.Read()
	waited := time.Since(start)
	stillHeld := mutexOwned(t, mutex)
	release()

	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if locked {
		t.Error("locked = true although another thread held the mutex")
	}
	if !bytes.Equal(data, block) {
		t.Error("the unlocked copy differs from the published block")
	}
	// No lower bound: Windows waits are only as precise as the timer tick.
	if waited > 5*time.Second {
		t.Errorf("Read took %v; it should give up on the mutex after LockWait (30ms)", waited)
	}
	if !stillHeld {
		t.Error("the holder lost the mutex during Read")
	}
	if mutexOwned(t, mutex) {
		t.Error("the mutex is still owned after its holder released it")
	}
}

// An abandoned mutex can be acquired, but its last owner may have died while
// rewriting the block. The read must succeed, must not claim the guarantee a
// clean lock gives, and must leave the mutex free again.
func TestReaderDoesNotTrustAnAbandonedMutex(t *testing.T) {
	block := sample(2).Bytes()
	mutexName, mutex := newMutex(t)
	r := Reader{Mapping: publish(t, block, 64<<10), Mutex: mutexName}
	abandon(t, mutex)

	data, locked, err := r.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if locked {
		t.Error("locked = true for a mutex whose owner died holding it")
	}
	if !bytes.Equal(data, block) {
		t.Error("the copy differs from the published block")
	}
	if mutexOwned(t, mutex) {
		t.Fatal("Read left the abandoned mutex owned")
	}
	// The abandonment is consumed by the first acquisition; from then on
	// the mutex is an ordinary one again.
	if _, locked, err := r.Read(); err != nil || !locked {
		t.Errorf("second read: locked = %v, err = %v", locked, err)
	}
}

func TestWaitMillis(t *testing.T) {
	for d, want := range map[time.Duration]uint32{
		-time.Second:            0,
		0:                       0,
		time.Nanosecond:         1, // a short wait is still a wait
		500 * time.Microsecond:  1,
		30 * time.Millisecond:   30,
		200 * time.Millisecond:  200,
		1500 * time.Microsecond: 2,
		time.Hour:               60_000, // capped, and never 0xFFFFFFFF ("forever")
		1<<63 - 1:               60_000,
	} {
		if got := waitMillis(d); got != want {
			t.Errorf("waitMillis(%v) = %d, want %d", d, got, want)
		}
	}
}

func TestReaderWithoutAMutex(t *testing.T) {
	block := sample(1).Bytes()
	r := Reader{Mapping: publish(t, block, 8<<10), Mutex: objectName("no-such-mutex")}

	data, locked, err := r.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if locked {
		t.Error("locked = true without a mutex")
	}
	if !bytes.Equal(data, block) {
		t.Error("the copy differs from the published block")
	}
}

func TestReaderMappingNotFound(t *testing.T) {
	r := Reader{Mapping: objectName("no-such-mapping"), Mutex: objectName("no-such-mutex")}
	data, locked, err := r.Read()
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Read: err = %v, want ErrNotFound", err)
	}
	if data != nil || locked {
		t.Errorf("Read returned data (%d bytes) or locked = %v on failure", len(data), locked)
	}
	if snap, err := r.Snapshot(); snap != nil || !errors.Is(err, ErrNotFound) {
		t.Errorf("Snapshot = %v, %v; want nil, ErrNotFound", snap, err)
	}
}

func TestReaderRejectsForeignMappings(t *testing.T) {
	const size = 8 << 10
	oversized := sample(2).Bytes()
	binary.LittleEndian.PutUint32(oversized[40:], 1<<20) // far more readings than the mapping can hold

	tests := []struct {
		name  string
		block []byte
		want  error
	}{
		{"all zero", nil, ErrSignature},
		{"some other program's data", []byte("not a sensor block, just text that happens to be mapped"), ErrSignature},
		{"header that points past the mapping", oversized, ErrLayout},
	}
	for _, tt := range tests {
		r := Reader{Mapping: publish(t, tt.block, size), Mutex: objectName("no-such-mutex")}
		if data, _, err := r.Read(); !errors.Is(err, tt.want) || data != nil {
			t.Errorf("%s: Read = %d bytes, %v; want no data and %v", tt.name, len(data), err, tt.want)
		}
	}
}

// A block that fills its mapping to the last byte must be copied whole, and
// nothing beyond the mapping may be read.
func TestReaderBlockThatFillsTheMapping(t *testing.T) {
	const size = 4 << 10 // one page
	im := sample(2)
	im.Tail = size - len(im.Bytes())
	block := im.Bytes()
	// Stretch the last reading element over the tail so that the block
	// ends exactly where the mapping does.
	_, _, readingSize := im.Sizes()
	binary.LittleEndian.PutUint32(block[36:], uint32(readingSize+im.Tail/len(im.Readings)))
	if im.Tail%len(im.Readings) != 0 {
		t.Fatalf("fixture: tail of %d bytes does not divide over %d readings", im.Tail, len(im.Readings))
	}

	r := Reader{Mapping: publish(t, block, size), Mutex: objectName("no-such-mutex")}
	data, _, err := r.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(data) != size {
		t.Errorf("copied %d bytes, want the whole %d-byte mapping", len(data), size)
	}
}

func TestReaderSnapshot(t *testing.T) {
	mutexName, _ := newMutex(t)

	im := sample(2)
	r := Reader{Mapping: publish(t, im.Bytes(), 64<<10), Mutex: mutexName}
	snap, err := r.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if !snap.Active || !snap.Locked || len(snap.Sensors) != 2 || len(snap.Readings) != 4 {
		t.Errorf("snapshot = active %v, locked %v, %d sensors, %d readings", snap.Active, snap.Locked, len(snap.Sensors), len(snap.Readings))
	}
	if got := snap.Readings[0].Unit; got != degC {
		t.Errorf("unit = %q, want %q", got, degC)
	}

	// Once HWiNFO stops publishing, the mapping stays behind with the dead
	// signature. Its last values are still returned, flagged as inactive.
	im.Dead = true
	r.Mapping = publish(t, im.Bytes(), 64<<10)
	snap, err = r.Snapshot()
	if !errors.Is(err, ErrInactive) {
		t.Fatalf("Snapshot of a dead block: err = %v, want ErrInactive", err)
	}
	if snap == nil || snap.Active || !snap.Locked || len(snap.Readings) != 4 {
		t.Errorf("snapshot of a dead block = %+v", snap)
	}
}

// The zero Reader must aim at HWiNFO's real objects.
func TestReaderDefaults(t *testing.T) {
	r := Reader{}.withDefaults()
	if r.Mapping != `Global\HWiNFO_SENS_SM2` || r.Mutex != `Global\HWiNFO_SM2_MUTEX` || r.LockWait != 200*time.Millisecond {
		t.Errorf("defaults = %+v", r)
	}
}
