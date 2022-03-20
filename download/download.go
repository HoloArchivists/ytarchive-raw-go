package download

import (
    "fmt"
    "io"
    "io/ioutil"
    "net/http"
    "os"
    "sync"
    "time"

    "github.com/notpeko/ytarchive-raw-go/log"
)

const FailThreshold = 20
const RetryThreshold = 3

var defaultClient = &http.Client {}

type DownloadResult struct {
    Error        error
    LostSegments []int
}

type DownloadTask struct {
    Client        *http.Client
    Url           string
    TmpFile       string
    Logger        *log.Logger
    Threads       uint
    wg            sync.WaitGroup
    result        DownloadResult
    started       bool
}

func (d *DownloadTask) Start() {
    if d.started {
        return
    }
    if d.Threads < 1 {
        d.Threads = 1
    }
    if len(d.Url) == 0 {
        log.Fatal("Empty URL")
    }
    if len(d.TmpFile) == 0 {
        log.Fatal("Empty TmpFile")
    }

    d.wg.Add(1)
    d.started = true
    go d.run()
}

func (d *DownloadTask) Wait() *DownloadResult {
    d.wg.Wait()
    return &d.result
}

func (d *DownloadTask) client() *http.Client {
    if d.Client != nil {
        return d.Client
    }
    return defaultClient
}

func (d *DownloadTask) logger() *log.Logger {
    if d.Logger != nil {
        return d.Logger
    }
    return log.DefaultLogger
}

func (d *DownloadTask) run() {
    defer d.wg.Done()

    segmentStatus, err := newSegStatus(d, d.Url)
    if err != nil {
        d.result.Error = err
        return
    }
    pbar := makeProgressBar(segmentStatus.end, func(msg string, progress float64) {
        d.logger().Infof("|%s| %.2f%%", msg, progress * 100)
    })

    mergeTask := makeMergeTask(d, segmentStatus, d.TmpFile)

    var downloadGroup sync.WaitGroup
    for i := uint(0); i < d.Threads; i++ {
        downloadGroup.Add(1)
        go downloadSegmentGroup(
            i,
            d,
            &downloadGroup,
            segmentStatus.groups[i],
            segmentStatus,
            pbar.done,
        )
    }

    downloadGroup.Wait()
    mergeTask.wait()
    d.result.LostSegments = mergeTask.notMerged
}

func downloadSegmentGroup(
    threadNumber uint,
    task *DownloadTask,
    wg *sync.WaitGroup,
    segments segmentRange,
    status *segmentStatus,
    done func(int),
) {
    defer wg.Done()

    task.logger().Infof("Thread %d downloading segments [%d, %d]", threadNumber, segments.start, segments.end)
    seg := segments.start
    failCount := 0
    for {
        if seg > segments.end {
            task.logger().Infof("Thread %d done", threadNumber)
            break
        }

        if failCount >= FailThreshold {
            task.logger().Warnf("Giving up segment %d", seg)

            status.downloaded(seg, segmentResult { ok: false })
            done(seg)

            seg++
            failCount = 0
            continue
        }

        task.logger().Debugf("Current segment: %d", seg)

        ok := downloadSegment(task, status, seg)
        if ok {
            task.logger().Debugf("Downloaded segment %d", seg)
            done(seg)

            seg++
            failCount = 0
        } else {
            failCount++
            task.logger().Debugf("Failed segment %d [%d/%d]", seg, failCount, FailThreshold)
            time.Sleep(1 * time.Second)
        }
    }
}

func downloadSegment(task *DownloadTask, status *segmentStatus, segment int) bool {
    targetUrl := getSegUrl(task.Url, segment)

    req, err := http.NewRequest("GET", targetUrl, nil)
    if err != nil {
        task.logger().Fatalf("Unable to create http request: %v", err)
    }
    req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/89.0.4389.90 Safari/537.36")

    resp, err := doRequest(task, req)
    if err != nil {
        task.logger().Debugf("Request for segment %d failed with %v", segment, err)
        return false
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        task.logger().Debugf("Non-200 status code %d for segment %d", resp.StatusCode, segment)
        req, err = http.NewRequest("GET", task.Url, nil)
        if err == nil {
            resp, err = doRequest(task, req)
            if resp != nil {
                defer resp.Body.Close()
            }
        }
        return false
    }

    file, err := ioutil.TempFile("/tmp/ytarchive_test", "segment-")
    if err != nil {
        task.logger().Warnf("Unable to create temp file for segment %d: %v", segment, err)
        return false
    }
    defer file.Close()

    _, err = io.Copy(file, resp.Body)
    if err != nil {
        os.Remove(file.Name())
        task.logger().Errorf("Unable to write segment %d: %v", segment, err)
        return false
    }

    file.Close() //ensure writes are done to not race the merge task

    status.downloaded(segment, segmentResult {
        ok: true,
        filename: file.Name(),
    })

    return true
}

func doRequest(task *DownloadTask, req *http.Request) (*http.Response, error) {
    for i := 0; i < RetryThreshold; i++ {
        resp, err := task.Client.Do(req)
        if err == nil {
            return resp, nil
        }
    }
    return nil, fmt.Errorf("All requests failed")
}

