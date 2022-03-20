package util

import (
    "os"

    "github.com/gofrs/flock"

    "github.com/notpeko/ytarchive-raw-go/log"
)

func LockFile(path string, printFailureMessage func()) func() {
    lock := flock.New(path)
    locked, err := lock.TryLock()
    if err != nil {
        log.Fatalf("Failed to lock file: %v", err)
    }
    if !locked {
        printFailureMessage()
        os.Exit(1)
    }
    return func() {
        lock.Unlock()
        os.Remove(path)
    }
}

