//go:build darwin

package main

/*
#cgo LDFLAGS: -framework Cocoa -framework QuartzCore
void luminaConfigureFloatingWindow(void);
void luminaSetWindowAppearance(int shadowEnabled, float cornerRadius);
void luminaSetClickThrough(int enabled);
void luminaSetupMenuBar(void);
*/
import "C"
import "sync"

var (
	trayMu       sync.Mutex
	trayCallback func(string)
	peekCallback func(bool)
)

//export goTrayAction
func goTrayAction(actionCode C.int) {
	trayMu.Lock()
	cb := trayCallback
	trayMu.Unlock()
	if cb == nil {
		return
	}
	switch int(actionCode) {
	case 1:
		cb("toggle-visible")
	case 2:
		cb("toggle-lock")
	case 3:
		cb("mode:pill")
	case 4:
		cb("mode:card")
	case 5:
		cb("mode:full")
	case 99:
		cb("quit")
	}
}

//export goLockPeek
func goLockPeek(peeking C.int) {
	trayMu.Lock()
	cb := peekCallback
	trayMu.Unlock()
	if cb != nil {
		cb(peeking != 0)
	}
}

// ConfigureFloatingWindow applies always-on-top, all-spaces, and transparent properties on macOS.
func ConfigureFloatingWindow() {
	C.luminaConfigureFloatingWindow()
}

// SetWindowAppearance updates native shadow and corner clipping on macOS.
func SetWindowAppearance(shadowEnabled bool, cornerRadius float64) {
	s := 0
	if shadowEnabled {
		s = 1
	}
	C.luminaSetWindowAppearance(C.int(s), C.float(cornerRadius))
}

// SetClickThrough enables or disables mouse click-through on the floating window.
func SetClickThrough(enabled bool) {
	if enabled {
		C.luminaSetClickThrough(1)
	} else {
		C.luminaSetClickThrough(0)
	}
}

// SetupPlatformShell initializes the native macOS menu bar icon and modifier-peek handler.
func SetupPlatformShell(onAction func(string), onPeek func(bool)) {
	trayMu.Lock()
	trayCallback = onAction
	peekCallback = onPeek
	trayMu.Unlock()
	C.luminaSetupMenuBar()
}
