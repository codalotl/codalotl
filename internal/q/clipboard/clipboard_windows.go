//go:build windows

package clipboard

import (
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

var (
	user32                     = syscall.MustLoadDLL("user32")
	isClipboardFormatAvailable = user32.MustFindProc("IsClipboardFormatAvailable")
	openClipboard              = user32.MustFindProc("OpenClipboard")
	closeClipboard             = user32.MustFindProc("CloseClipboard")
	emptyClipboard             = user32.MustFindProc("EmptyClipboard")
	getClipboardData           = user32.MustFindProc("GetClipboardData")
	setClipboardData           = user32.MustFindProc("SetClipboardData")

	kernel32     = syscall.NewLazyDLL("kernel32")
	globalAlloc  = kernel32.NewProc("GlobalAlloc")
	globalFree   = kernel32.NewProc("GlobalFree")
	globalLock   = kernel32.NewProc("GlobalLock")
	globalUnlock = kernel32.NewProc("GlobalUnlock")
	lstrcpy      = kernel32.NewProc("lstrcpyW")
)

type winBackend struct{}

func selectBackend() (backend, error) {
	return winBackend{}, nil
}

// waitOpenClipboard opens the clipboard, waiting for up to a second to do so.
func waitOpenClipboard() error {
	start := time.Now()
	deadline := start.Add(time.Second)
	var r uintptr
	var err error
	for time.Now().Before(deadline) {
		r, _, err = openClipboard.Call(0)
		if r != 0 {
			return nil
		}
		time.Sleep(time.Millisecond)
	}
	return err
}

func (winBackend) read() (string, error) {
	// The Windows clipboard APIs require OpenClipboard and CloseClipboard to be
	// called from the same OS thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	formatAvailable, _, err := isClipboardFormatAvailable.Call(cfUnicodeText)
	if formatAvailable == 0 {
		return "", err
	}

	if err := waitOpenClipboard(); err != nil {
		return "", err
	}

	h, _, err := getClipboardData.Call(cfUnicodeText)
	if h == 0 {
		_, _, _ = closeClipboard.Call()
		return "", err
	}

	l, _, err := globalLock.Call(h)
	if l == 0 {
		_, _, _ = closeClipboard.Call()
		return "", err
	}

	text := syscall.UTF16ToString((*[1 << 20]uint16)(unsafe.Pointer(l))[:])

	r, _, err := globalUnlock.Call(h)
	if r == 0 {
		_, _, _ = closeClipboard.Call()
		return "", err
	}

	closed, _, err := closeClipboard.Call()
	if closed == 0 {
		return "", err
	}
	return text, nil
}

func (winBackend) write(text string) error {
	// The Windows clipboard APIs require OpenClipboard and CloseClipboard to be
	// called from the same OS thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := waitOpenClipboard(); err != nil {
		return err
	}

	r, _, err := emptyClipboard.Call(0)
	if r == 0 {
		_, _, _ = closeClipboard.Call()
		return err
	}

	data := syscall.StringToUTF16(text)

	// Per docs, this must be GMEM_MOVEABLE.
	h, _, err := globalAlloc.Call(gmemMoveable, uintptr(len(data)*int(unsafe.Sizeof(data[0]))))
	if h == 0 {
		_, _, _ = closeClipboard.Call()
		return err
	}
	defer func() {
		if h != 0 {
			globalFree.Call(h)
		}
	}()

	l, _, err := globalLock.Call(h)
	if l == 0 {
		_, _, _ = closeClipboard.Call()
		return err
	}

	r, _, err = lstrcpy.Call(l, uintptr(unsafe.Pointer(&data[0])))
	if r == 0 {
		_, _, _ = closeClipboard.Call()
		return err
	}

	r, _, err = globalUnlock.Call(h)
	if r == 0 {
		if errno, ok := err.(syscall.Errno); ok && errno != 0 {
			_, _, _ = closeClipboard.Call()
			return err
		}
	}

	r, _, err = setClipboardData.Call(cfUnicodeText, h)
	if r == 0 {
		_, _, _ = closeClipboard.Call()
		return err
	}
	h = 0 // suppress deferred cleanup; ownership transferred to the system

	closed, _, err := closeClipboard.Call()
	if closed == 0 {
		return err
	}
	return nil
}
