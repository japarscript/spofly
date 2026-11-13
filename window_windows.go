//go:build windows

package main

import (
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	procFindWindowW      = user32.NewProc("FindWindowW")
	procGetWindowLongPtr = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtr = user32.NewProc("SetWindowLongPtrW")
	procRegisterHotKey   = user32.NewProc("RegisterHotKey")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procGetAsyncKeyState = user32.NewProc("GetAsyncKeyState")
)

const (
	gwlExStyle       = ^uintptr(19) // -20 (GWL_EXSTYLE)
	wsExLayered      = 0x00080000
	wsExTransparent  = 0x00000020
	wsExToolWindow   = 0x00000080
	modAlt           = 0x0001
	modControl       = 0x0002
	wmHotKey         = 0x0312
	hotkeyToggleLock = 1
	vkMenu           = 0x12 // Alt key
)

type winMSG struct {
	HWND    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

var (
	winLockMu   sync.Mutex
	winLocked   bool
	winPeekStop chan struct{}
	winPeekCB   func(bool)
)

func findWindowHandle() uintptr {
	title, _ := syscall.UTF16PtrFromString("Spofly")
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(title)))
	return hwnd
}

// ConfigureFloatingWindow configures layered transparency on Windows.
func ConfigureFloatingWindow() {
	hwnd := findWindowHandle()
	if hwnd == 0 {
		return
	}
	exStyle, _, _ := procGetWindowLongPtr.Call(hwnd, gwlExStyle)
	exStyle |= wsExLayered | wsExToolWindow
	_, _, _ = procSetWindowLongPtr.Call(hwnd, gwlExStyle, exStyle)
}

// SetWindowAppearance is a no-op on Windows where WebView2 handles transparent corners directly.
func SetWindowAppearance(_ bool, _ float64) {}

func applyWinTransparent(transparent bool) {
	hwnd := findWindowHandle()
	if hwnd == 0 {
		return
	}
	exStyle, _, _ := procGetWindowLongPtr.Call(hwnd, gwlExStyle)
	exStyle |= wsExLayered
	if transparent {
		exStyle |= wsExTransparent
	} else {
		exStyle &^= wsExTransparent
	}
	_, _, _ = procSetWindowLongPtr.Call(hwnd, gwlExStyle, exStyle)
}

// SetClickThrough toggles WS_EX_TRANSPARENT and starts an Alt-key peek watcher while locked.
func SetClickThrough(enabled bool) {
	winLockMu.Lock()
	winLocked = enabled
	if winPeekStop != nil {
		close(winPeekStop)
		winPeekStop = nil
	}
	var stopCh chan struct{}
	if enabled {
		stopCh = make(chan struct{})
		winPeekStop = stopCh
	}
	cb := winPeekCB
	winLockMu.Unlock()

	applyWinTransparent(enabled)
	if cb != nil && !enabled {
		cb(false)
	}

	if enabled && stopCh != nil {
		go func(ch chan struct{}) {
			ticker := time.NewTicker(120 * time.Millisecond)
			defer ticker.Stop()
			lastPeek := false
			for {
				select {
				case <-ch:
					return
				case <-ticker.C:
					state, _, _ := procGetAsyncKeyState.Call(uintptr(vkMenu))
					altHeld := (state & 0x8000) != 0
					if altHeld != lastPeek {
						lastPeek = altHeld
						applyWinTransparent(!altHeld)
						if cb != nil {
							cb(altHeld)
						}
					}
				}
			}
		}(stopCh)
	}
}

// SetupPlatformShell registers a global Ctrl+Alt+L hotkey on Windows to toggle click-through lock.
func SetupPlatformShell(onAction func(string), onPeek func(bool)) {
	winLockMu.Lock()
	winPeekCB = onPeek
	winLockMu.Unlock()

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		r, _, _ := procRegisterHotKey.Call(0, hotkeyToggleLock, modControl|modAlt, uintptr('L'))
		if r == 0 {
			return
		}
		var msg winMSG
		for {
			ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if int32(ret) <= 0 {
				break
			}
			if msg.Message == wmHotKey && msg.WParam == hotkeyToggleLock && onAction != nil {
				onAction("toggle-lock")
			}
		}
	}()
}
