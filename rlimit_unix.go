//go:build (linux || android)
// +build (linux || android)

package main

import (
    "syscall"

    "github.com/notpeko/ytarchive-raw-go/log"
)

func increaseOpenFileLimit() {
    var rLimit syscall.Rlimit
    if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
        log.Warn("Unable to get current rlimit")
        return
    }
    rLimit.Cur = rLimit.Max
    if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
        log.Warn("Unable to set rlimit")
        return
    }
    if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
        log.Warn("Unable to get updated rlimit")
        return
    }
    log.Infof("Set open file limit to %d", rLimit.Cur)
}

