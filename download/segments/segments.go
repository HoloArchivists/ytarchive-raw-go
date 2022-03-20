package segments

import (
    "sync"
    "time"
)

type QueueMode int
const (
    QueueSequential QueueMode = iota
    QueueOutOfOrder
)

type SegmentStatus struct {
    mu           sync.Mutex
    end          int
    mergedCount  int
    scheduler    workScheduler
    segments     map[int]SegmentResult
    missed       []int
}

type SegmentResult struct {
    Filename string
    Ok       bool
}

// each worker has it's own queue of segments to download
func (s *SegmentStatus) CreateQueue(worker int) WorkQueue {
    return s.scheduler.CreateQueue(worker)
}

func (s *SegmentStatus) IsLast(segment int) bool {
    return segment == s.end - 1
}

func (s *SegmentStatus) MissedSegments() []int {
    return s.missed
}

// retrieves the next segment to be merged, if available
// and advances the merge position (so the next call will attempt
// to fetch the next segment)
func (s *SegmentStatus) NextToMerge() (SegmentResult, int, bool) {
    s.mu.Lock()
    defer s.mu.Unlock()

    number := s.mergedCount
    r, ok := s.segments[number]
    if ok {
        delete(s.segments, number)
        s.mergedCount++
    }
    return r, number, ok
}

// download task done downloading a segment
func (s *SegmentStatus) Downloaded(number int, result SegmentResult) {
    s.mu.Lock()
    defer s.mu.Unlock()
    if !result.Ok {
        s.missed = append(s.missed, number)
    }
    s.segments[number] = result
}

// are all segments merged?
func (s *SegmentStatus) Done() bool {
    s.mu.Lock()
    defer s.mu.Unlock()
    return s.mergedCount == s.end
}

func Create(segmentCount int, threads int, mode QueueMode, requeueDelay time.Duration) *SegmentStatus {
    var scheduler workScheduler
    switch mode {
    case QueueOutOfOrder:
        scheduler = makeBatchedScheduler(segmentCount, requeueDelay, threads)
    case QueueSequential:
        scheduler = makeSequentialScheduler(segmentCount, requeueDelay)
    }

    ret := &SegmentStatus {
        end:         segmentCount,
        mergedCount: 0,
        scheduler:   scheduler,
        segments:    make(map[int]SegmentResult),
    }

    return ret
}

