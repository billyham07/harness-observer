//go:build windows

package gpui

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// HideOwnedConsole hides the console window when this process is the only one
// attached to it — the double-click-from-Explorer case. A terminal session
// shares the console with the shell, so it stays visible and the URL is still
// printed there.
func HideOwnedConsole() {
	var pids [2]uint32
	n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	if n != 1 {
		return
	}
	hwnd, err := getConsoleWindow()
	if err != nil || hwnd == 0 {
		return
	}
	const swHide = 0
	_, _ = showWindow(hwnd, swHide)
}

var (
	kernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	user32                    = windows.NewLazySystemDLL("user32.dll")
	procGetConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
	procGetConsoleWnd         = kernel32.NewProc("GetConsoleWindow")
	procShowWindow            = user32.NewProc("ShowWindow")
)

func getConsoleWindow() (windows.Handle, error) {
	r, _, err := procGetConsoleWnd.Call()
	if r == 0 {
		if err != windows.ERROR_SUCCESS {
			return 0, err
		}
		return 0, nil
	}
	return windows.Handle(r), nil
}

func showWindow(hwnd windows.Handle, cmd int) (bool, error) {
	r, _, err := procShowWindow.Call(uintptr(hwnd), uintptr(cmd))
	if err != windows.ERROR_SUCCESS {
		return r != 0, err
	}
	return r != 0, nil
}
