package segments

import (
    "fmt"
    "sync"
    "time"

    "github.com/HoloArchivists/ytarchive-raw-go/log"
)

type failedSeg struct {
    seg       int
    fails     uint
    timestamp time.Time
}

func (f failedSeg) isReady() bool {
    return time.Now().After(f.timestamp)
}

func (f failedSeg) wait() {
    delay := f.timestamp.Sub(time.Now())
    if delay.Seconds() > 1 {
        log.Debugf("Waiting %v before retrying segment %d", delay.Round(time.Second), f.seg)
    }
    time.Sleep(delay)
}

func makeFailedSeg(seg int, fails uint, delay time.Duration) failedSeg {
    return failedSeg {
        seg:       seg,
        fails:     fails,
        timestamp: time.Now().Add(delay),
    }
}

type WorkQueue interface {
    NextSegment() (int, uint, bool)
    RequeueFailed(seg int, fails uint)
}

type workScheduler interface {
    CreateQueue(worker int) WorkQueue
}

// Simple, sequential scheduler. Workers get the next segment from a shared counter
var _ workScheduler = &sequentialScheduler {}
type sequentialScheduler struct {
    mu           sync.Mutex
    max          int
    next         int
    failed       []failedSeg
    requeueDelay time.Duration
}

func makeSequentialScheduler(totalSegments int, requeueDelay time.Duration) workScheduler {
    return &sequentialScheduler {
        max:          totalSegments,
        next:         0,
        requeueDelay: requeueDelay,
    }
}

func (s *sequentialScheduler) CreateQueue(_ int) WorkQueue {
    return &sequentialQueue { sched: s }
}

var _ WorkQueue = &sequentialQueue {}
type sequentialQueue struct {
    sched *sequentialScheduler
}

func (s *sequentialQueue) nextInternal() (failedSeg, int, bool) {
    s.sched.mu.Lock()
    defer s.sched.mu.Unlock()

    if s.sched.next < s.sched.max {
        seg := s.sched.next
        s.sched.next++
        return failedSeg{}, seg, true
    }

    if len(s.sched.failed) > 0 {
        seg := s.sched.failed[0]
        s.sched.failed = s.sched.failed[1:]
        return seg, -1, true
    }

    return failedSeg{}, 0, false
}

func (s *sequentialQueue) NextSegment() (int, uint, bool) {
    //don't hold lock while waiting for a failed segment
    f, seg, ok := s.nextInternal()
    if !ok {
        return -1, 0, false
    }
    if seg >= 0 {
        return seg, 0, true
    }
    f.wait()
    return f.seg, f.fails, true
}

func (s *sequentialQueue) RequeueFailed(seg int, fails uint) {
    s.sched.mu.Lock()
    defer s.sched.mu.Unlock()

    s.sched.failed = append(s.sched.failed, makeFailedSeg(seg, fails, s.sched.requeueDelay))
}

// Splits the work in batches, each worker goes through it's own batch, but if it's
// done it can steal from other workers.
var _ workScheduler = &batchedScheduler {}
type batchedScheduler struct {
    batches      []*batchRange
    requeueDelay time.Duration
}

func makeBatchedScheduler(segments int, requeueDelay time.Duration, threads int) workScheduler {
    s := &batchedScheduler {
        batches:      make([]*batchRange, 0),
        requeueDelay: requeueDelay,
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
    for len(s.batches) < threads {
        // start > end so it just steals
        b := &batchRange {
            sched: s,
            start: -1,
            end:   -2,
        }
        s.batches = append(s.batches, b)
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
    failed   []failedSeg
}

//requires lock to be held before calling
func (b *batchRange) tryGetFailed(hasNonFailed bool) (failedSeg, int, bool) {
    if len(b.failed) > 0 {
        seg := b.failed[0]
        if hasNonFailed && !seg.isReady() {
            return failedSeg{}, -1, false
        }
        b.failed = b.failed[1:]
        return seg, -1, true
    }
    return failedSeg{}, -1, false
}

func (b *batchRange) tryGetNext() (failedSeg, int, bool) {
    b.mu.Lock()
    defer b.mu.Unlock()
    if b.start > b.end {
        //no more normal segments here anyway
        return b.tryGetFailed(false)
    }
    seg := b.start
    b.start++
    return failedSeg{}, seg, true
}

func (b *batchRange) trySteal() (failedSeg, int, bool) {
    b.mu.Lock()
    defer b.mu.Unlock()
    //try stealing from the failed queue before stealing from
    //the normal queue
    //if the delay on the first segment still isn't up and
    //there's some other segment that can be stolen, leave
    //the failed segment waiting a bit longer
    f, seg, ok := b.tryGetFailed(b.start <= b.end)
    if ok {
        return f, seg, true
    }

    if b.start > b.end {
        return failedSeg{}, -1, false
    }
    seg = b.end
    b.end--
    return failedSeg{}, seg, true
}

func (b *batchRange) nextInternal() (failedSeg, int, bool) {
    f, seg, ok := b.tryGetNext()
    if ok {
        return f, seg, true
    }
    for _, v := range b.sched.batches {
        if v != b {
            f, seg, ok = v.trySteal()
            if ok {
                return f, seg, true
            }
        }
    }
    return failedSeg {}, -1, false
}

func (b *batchRange) NextSegment() (int, uint, bool) {
    //don't hold lock while waiting for a failed segment
    f, seg, ok := b.nextInternal()
    if !ok {
        return -1, 0, false
    }
    if seg >= 0 {
        return seg, 0, true
    }
    f.wait()
    return f.seg, f.fails, true
}

func (b *batchRange) RequeueFailed(seg int, fails uint) {
    b.mu.Lock()
    defer b.mu.Unlock()

    b.failed = append(b.failed, makeFailedSeg(seg, fails, b.sched.requeueDelay))
}

