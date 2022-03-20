package segments

import (
    "fmt"
    "sync"
)

type WorkQueue interface {
    NextSegment() (int, bool)
}

type workScheduler interface {
    CreateQueue(worker int) WorkQueue
}

// Simple, sequential scheduler. Workers get the next segment from a shared counter
var _ workScheduler = &sequentialScheduler {}
type sequentialScheduler struct {
    mu   sync.Mutex
    max  int
    next int
}

func makeSequentialScheduler(totalSegments int) workScheduler {
    return &sequentialScheduler {
        max:  totalSegments,
        next: 0,
    }
}

func (s *sequentialScheduler) CreateQueue(_ int) WorkQueue {
    return &sequentialQueue { sched: s }
}

var _ WorkQueue = &sequentialQueue {}
type sequentialQueue struct {
    sched *sequentialScheduler
}

func (s *sequentialQueue) NextSegment() (int, bool) {
    s.sched.mu.Lock()
    defer s.sched.mu.Unlock()

    if s.sched.next < s.sched.max {
        seg := s.sched.next
        s.sched.next++
        return seg, true
    }
    return -1, false
}

// Splits the work in batches, each worker goes through it's own batch, but if it's
// done it can steal from other workers.
var _ workScheduler = &batchedScheduler {}
type batchedScheduler struct {
    batches []*batchRange
}

func makeBatchedScheduler(segments int, threads int) workScheduler {
    s := &batchedScheduler {
        batches: make([]*batchRange, 0),
    }
    lastSeg := -1
    interval := segments / threads
    for {
        if lastSeg + 1 + interval < segments {
            b := &batchRange {
                sched: s,
                start: lastSeg + 1,
                end:   lastSeg + 1 + interval,
            }
            s.batches = append(s.batches, b)
            lastSeg = b.end
        } else {
            b := &batchRange {
                sched: s,
                start: lastSeg + 1,
                end:   segments - 1,
            }
            s.batches = append(s.batches, b)
            break
        }
    }
    return s
}

func (s *batchedScheduler) CreateQueue(worker int) WorkQueue {
    if worker < 0 || worker >= len(s.batches) {
        panic(fmt.Sprintf("Invalid worker number %d (worker count: %d)", worker, len(s.batches)))
    }
    b := s.batches[worker]
    b.mu.Lock()
    defer b.mu.Unlock()
    if b.assigned {
        panic(fmt.Sprintf("Queue for worker %d has already been created", worker))
    }
    b.assigned = true
    return b
}

var _ WorkQueue = &batchRange {}
type batchRange struct {
    sched    *batchedScheduler
    mu       sync.Mutex
    start    int
    end      int
    assigned bool
}

func (b *batchRange) tryGetNext() (int, bool) {
    b.mu.Lock()
    defer b.mu.Unlock()
    if b.start > b.end {
        return -1, false
    }
    seg := b.start
    b.start++
    return seg, true
}

func (b *batchRange) tryGetLast() (int, bool) {
    b.mu.Lock()
    defer b.mu.Unlock()
    if b.start > b.end {
        return -1, false
    }
    seg := b.end
    b.end--
    return seg, true
}

func (b *batchRange) NextSegment() (int, bool) {
    seg, ok := b.tryGetNext()
    if ok {
        return seg, true
    }
    for _, v := range b.sched.batches {
        if v != b {
            seg, ok = v.tryGetLast()
            if ok {
                return seg, true
            }
        }
    }
    return -1, false
}

