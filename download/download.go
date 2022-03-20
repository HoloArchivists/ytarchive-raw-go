package download

import (
    "fmt"
    "io"
    "net/http"
    "net/url"
    "os"
    "path"
    "strconv"
    "strings"
    "sync"
    "time"

    "github.com/notpeko/ytarchive-raw-go/log"
)

type QueueMode int
const (
    QueueAuto       QueueMode = iota
    QueueSequential
    QueueOutOfOrder
)

const DefaultFailThreshold = 20
const DefaultRetryThreshold = 3

var defaultClient = &http.Client {}

type DownloadResult struct {
    Error         error
    LostSegments  []int
    TotalSegments int
}

type DownloadTask struct {
    Client         *http.Client
    DeleteSegments bool
    FailThreshold  uint
    Logger         *log.Logger
    MergeFile      string
    QueueMode      QueueMode
    RetryThreshold uint
    SegmentDir     string
    Threads        uint
    Url            string
    wg             sync.WaitGroup
    result         DownloadResult
    started        bool
    id             string
    itag           int
}

func (d *DownloadTask) Start() {
    if d.started {
        return
    }

    if d.FailThreshold < 1 {
        d.FailThreshold = DefaultFailThreshold
    }
    if d.RetryThreshold < 1 {
        d.RetryThreshold = DefaultRetryThreshold
    }
    if d.Threads < 1 {
        d.Threads = 1
    }

    if len(d.Url) == 0 {
        log.Fatal("Empty URL")
    }
    if len(d.MergeFile) == 0 {
        log.Fatal("Empty MergeFile")
    }
    if len(d.SegmentDir) == 0 {
        log.Fatal("Empty SegmentDir")
    }

    targetUrl, err := url.Parse(d.Url)
    if err != nil {
        d.logger().Fatalf("Failed to parse URL: %v", err)
    }

    query := targetUrl.Query()

    if !query.Has("id") {
        d.logger().Fatal("URL missing 'id' parameter")
    }
    id := query.Get("id")
    if idx := strings.IndexByte(id, '~'); idx > 0 {
        id = id[:idx]
    }
    d.id = id

    if !query.Has("itag") {
        d.logger().Fatal("URL misssing 'itag' parameter")
    }
    itagString := query.Get("itag")
    itag, err := strconv.Atoi(itagString)
    if err != nil {
        d.logger().Fatalf("Unable to parse itag value '%s' into an int", itagString)
    }
    d.itag = itag

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

    segmentStatus, err := newSegStatus(d, d.Url, d.QueueMode)
    if err != nil {
        d.result.Error = err
        return
    }
    d.result.TotalSegments = segmentStatus.end

    pbar := makeProgressBar(segmentStatus.end, func(msg string, finished int, total int) {
        progress := float64(finished) / float64(total)
        d.logger().Infof("|%s| %.2f%% (%d/%d)", msg, progress * 100, finished, total)
    })

    mergeTask := makeMergeTask(d, segmentStatus, d.MergeFile)

    var downloadGroup sync.WaitGroup
    for i := uint(0); i < d.Threads; i++ {
        downloadGroup.Add(1)
        go downloadTask(
            i,
            d,
            &downloadGroup,
            segmentStatus,
            pbar.done,
        )
    }

    downloadGroup.Wait()
    mergeTask.wait()
    d.result.LostSegments = mergeTask.notMerged
}

func downloadTask(
    threadNumber uint,
    task *DownloadTask,
    wg *sync.WaitGroup,
    status *segmentStatus,
    done func(int),
) {
    defer wg.Done()
    queue := status.createQueue(int(threadNumber))

    failCount := uint(0)
    seg := -1
    for {
        if seg == -1 {
            var ok bool
            seg, ok = queue.NextSegment()
            if !ok {
                task.logger().Infof("Thread %d done", threadNumber)
                break
            }
            if seg == -1 {
                panic("Segment == -1")
            }
        }

        //the last segment often isn't available, so use less retries for it
        fails := task.FailThreshold
        if seg == status.end - 1 {
            //at least 5
            fails = task.FailThreshold / 4
            if fails < 5 {
                fails = 5
            }
        }

        if failCount >= fails {
            task.logger().Warnf("Giving up segment %d", seg)

            status.downloaded(seg, segmentResult { ok: false })
            done(seg)

            seg = -1
            failCount = 0
            continue
        }

        task.logger().Debugf("Current segment: %d", seg)

        ok := downloadSegment(task, status, seg)
        if ok {
            done(seg)

            seg = -1
            failCount = 0
        } else {
            failCount++
            task.logger().Debugf("Failed segment %d [%d/%d]", seg, failCount, fails)

            //exponential backoff, up to 4 seconds between retries
            sleepShift := failCount
            if sleepShift > 2 {
                sleepShift = 2
            }

            time.Sleep(time.Duration(1 << sleepShift) * time.Second)
        }
    }
}

func segmentBaseFileName(task *DownloadTask, segment int) string {
    return path.Join(
        task.SegmentDir,
        fmt.Sprintf(
            "segment-%s_%d.%d",
            task.id,
            task.itag,
            segment,
        ),
    )
}

func downloadSegment(task *DownloadTask, status *segmentStatus, segment int) bool {
    segmentBasePath := segmentBaseFileName(task, segment)
    segmentDownloadPath := segmentBasePath + ".incomplete"
    segmentDonePath := segmentBasePath + ".done"

    //already downloaded
    if info, err := os.Stat(segmentDonePath); err == nil && info.Size() > 0 {
        task.logger().Debugf("Segment %d already downloaded", segment)
        status.downloaded(segment, segmentResult {
            ok: true,
            filename: segmentDonePath,
        })
        return true
    }

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

    file, err := os.OpenFile(segmentDownloadPath, os.O_RDWR|os.O_CREATE, 0644)
    if err != nil {
        task.logger().Warnf("Unable to create temp file for segment %d: %v", segment, err)
        return false
    }
    defer file.Close()

    if _, err = io.Copy(file, resp.Body); err != nil {
        os.Remove(file.Name())
        task.logger().Errorf("Unable to write segment %d: %v", segment, err)
        return false
    }

    if err = file.Sync(); err != nil {
        os.Remove(file.Name())
        task.logger().Errorf("Unable to sync segment %d: %v", segment, err)
        return false
    }
    file.Close()

    if err = os.Rename(segmentDownloadPath, segmentDonePath); err != nil {
        os.Remove(segmentDownloadPath)
        task.logger().Errorf("Unable to rename segment %d: %v", segment, err)
        return false
    }
    task.logger().Debugf("Downloaded segment %d", segment)

    status.downloaded(segment, segmentResult {
        ok: true,
        filename: segmentDonePath,
    })

    return true
}

func doRequest(task *DownloadTask, req *http.Request) (*http.Response, error) {
    for i := uint(0); i < task.RetryThreshold; i++ {
        resp, err := task.Client.Do(req)
        if err == nil {
            return resp, nil
        }
    }
    return nil, fmt.Errorf("All requests failed")
}

