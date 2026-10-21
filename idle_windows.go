package main

import (
	"syscall"
	"unsafe"
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	getLastInputInfo = user32.NewProc("GetLastInputInfo")
	getTickCount     = kernel32.NewProc("GetTickCount")
)

// idleSeconds: time since the last keyboard/mouse input.
func idleSeconds() (float64, bool) {
	var info struct {
		cbSize uint32
		dwTime uint32
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	if r, _, _ := getLastInputInfo.Call(uintptr(unsafe.Pointer(&info))); r == 0 {
		return 0, false
	}
	now, _, _ := getTickCount.Call()
	return float64(uint32(now)-info.dwTime) / 1000, true
}
