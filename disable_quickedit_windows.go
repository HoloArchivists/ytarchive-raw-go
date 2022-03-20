//go:build windows
// +build windows

package main

import (
    "os"
    "syscall"
    "unsafe"
)

const (
    cENABLE_QUICK_EDIT_MODE = uint32(0x0040)
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode = kernel32.NewProc("SetConsoleMode")
)

func disableQuickEditMode() {
    var mode uint32
	h := os.Stdin.Fd()
    if r, _, _ := procGetConsoleMode.Call(h, uintptr(unsafe.Pointer(&mode))); r == 0 {
        return
    }
    mode &= ^cENABLE_QUICK_EDIT_MODE

    procSetConsoleMode.Call(h, uintptr(mode))
}

