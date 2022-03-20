package download

import (
    "fmt"
    "net/url"
    "strconv"
    "sync"

    "github.com/notpeko/ytarchive-raw-go/log"
)

type segmentStatus struct {
    mu           sync.Mutex
    end          int
    mergedCount  int
    scheduler    workScheduler
    segments     map[int]segmentResult
}

type segmentResult struct {
    filename string
    ok       bool
}

func getSegUrl(baseUrl string, seg int) string {
    url, err := url.Parse(baseUrl)
    if err != nil {
        log.Fatalf("Invalid url '%s': %v", baseUrl, err)
    }

    q := url.Query()
    q.Set("sq", fmt.Sprintf("%d", seg))
    url.RawQuery = q.Encode()

    return url.String()
}

// each worker has it's own queue of segments to download
func (s *segmentStatus) createQueue(worker int) workQueue {
    return s.scheduler.CreateQueue(worker)
}

// is this segment done downloading?
func (s *segmentStatus) result(number int) (segmentResult, bool) {
    s.mu.Lock()
    defer s.mu.Unlock()
    r, ok := s.segments[number]
    return r, ok
}

// merge task done merging a segment
func (s *segmentStatus) merged(number int) {
    s.mu.Lock()
    defer s.mu.Unlock()
    delete(s.segments, number)
    s.mergedCount++
}

// download task done downloading a segment
func (s *segmentStatus) downloaded(number int, result segmentResult) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.segments[number] = result
}

// are all segments merged?
func (s *segmentStatus) done() bool {
    s.mu.Lock()
    defer s.mu.Unlock()
    return s.mergedCount == s.end
}

func newSegStatus(task *DownloadTask, url string, mode QueueMode) (*segmentStatus, error) {
    task.logger().Info("Getting total segments")

    url = getSegUrl(url, 0)
    resp, err := task.client().Get(url)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    header := resp.Header.Get("x-head-seqnum")
    if header == "" {
        return nil, fmt.Errorf("Unable to get segment count, response status: %s", resp.Status)
    }

    segmentCount, err := strconv.Atoi(header)
    if err != nil {
        return nil, fmt.Errorf("Unable to parse x-head-seqnum '%s': %v", header, err)
    }
    task.logger().Infof("Total segments: %d", segmentCount)

    var scheduler workScheduler
    switch mode {
    case QueueOutOfOrder:
        scheduler = makeBatchedScheduler(segmentCount, int(task.Threads))
    case QueueSequential, QueueAuto:
        scheduler = makeSequentialScheduler(segmentCount)
    }

    ret := &segmentStatus {
        end:         segmentCount,
        mergedCount: 0,
        scheduler:   scheduler,
        segments:    make(map[int]segmentResult),
    }

    return ret, nil
}

