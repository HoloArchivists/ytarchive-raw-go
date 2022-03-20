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

    "github.com/notpeko/ytarchive-raw-go/download/segments"
    "github.com/notpeko/ytarchive-raw-go/log"
    "github.com/notpeko/ytarchive-raw-go/merge"
    "github.com/notpeko/ytarchive-raw-go/util"
)


const DefaultFailThreshold = 20
const DefaultRetryThreshold = 3

type DownloadResult struct {
    Error         error
    LostSegments  []int
    TotalSegments int
}

type DownloadTask struct {
    CreateClient   func() *http.Client
    FailThreshold  uint
    Fsync          bool
    Logger         *log.Logger
    Merger         merge.Merger
    Progress       *Progress
    QueueMode      segments.QueueMode
    RequeueDelay   time.Duration
    RequeueFailed  uint
    RequeueLast    bool
    RetryThreshold uint
    SegmentCount   uint
    SegmentDir     string
    Threads        uint
    Url            string
    clientLock     sync.Mutex
    httpClient     *http.Client
    wg             sync.WaitGroup
    result         DownloadResult
    started        bool
    id             string
    itag           int
    expire         *time.Time
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
    if d.Merger == nil {
        log.Fatal("Missing Merger")
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

    //not a failure if missing, just won't warn on ETA >= remaining duration
    if !query.Has("expire") {
        d.logger().Warn("Unable to find 'expire' query parameter")
    } else {
        expireString := query.Get("expire")
        expire, err := strconv.ParseInt(expireString, 10, 64)
        if err != nil {
            d.logger().Warnf("Unable to parse 'expire' parameter: %v", err)
        } else {
            t := time.Unix(expire, 0)
            d.expire = &t
        }
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
    d.clientLock.Lock()
    defer d.clientLock.Unlock()

    if d.httpClient == nil {
        d.httpClient = d.CreateClient()
    }
    return d.httpClient
}

func (d *DownloadTask) replaceClient() {
    d.clientLock.Lock()
    defer d.clientLock.Unlock()

    if c, ok := d.httpClient.Transport.(io.Closer); ok {
        c.Close()
    }
    //next client() call will create a new one
    d.httpClient = nil
}

func (d *DownloadTask) logger() *log.Logger {
    if d.Logger != nil {
        return d.Logger
    }
    return log.DefaultLogger
}

func (d *DownloadTask) getSegmentCount() (int, error) {
    d.logger().Info("Getting total segments")

    url := getSegUrl(d.Url, 0)
    resp, err := d.client().Get(url)
    if err != nil {
        return -1, err
    }
    defer resp.Body.Close()

    header := resp.Header.Get("x-head-seqnum")
    if header == "" {
        return -1, fmt.Errorf("Unable to get segment count, response status: %s", resp.Status)
    }

    segmentCount, err := strconv.Atoi(header)
    if err != nil {
        return -1, fmt.Errorf("Unable to parse x-head-seqnum '%s': %v", header, err)
    }
    d.logger().Infof("Total segments: %d", segmentCount)

    return segmentCount, nil
}

func (d *DownloadTask) run() {
    defer d.wg.Done()

    var segmentCount int
    if d.SegmentCount == 0 {
        var err error
        segmentCount, err = d.getSegmentCount()
        if err != nil {
            d.result.Error = err
            return
        }
    } else {
        segmentCount = int(d.SegmentCount)
    }

    d.result.TotalSegments = segmentCount

    d.Progress.init(segmentCount, d.expire)

    segmentStatus := segments.Create(segmentCount, int(d.Threads), d.QueueMode, d.RequeueDelay)
    go d.Merger.Merge(segmentStatus)

    var downloadGroup sync.WaitGroup
    for i := uint(0); i < d.Threads; i++ {
        downloadGroup.Add(1)
        go downloadTask(
            i,
            d,
            &downloadGroup,
            segmentStatus,
        )
    }

    downloadGroup.Wait()
    d.result.LostSegments = segmentStatus.MissedSegments()
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

func downloadTask(
    threadNumber uint,
    task *DownloadTask,
    wg *sync.WaitGroup,
    status *segments.SegmentStatus,
) {
    defer wg.Done()
    queue := status.CreateQueue(int(threadNumber))

    failCount := uint(0)
    networkFailCount := uint(0)

    seg := -1
    requeues := uint(0)
    for {
        if seg == -1 {
            var ok bool
            seg, requeues, ok = queue.NextSegment()
            if !ok {
                task.logger().Infof("Thread %d done", threadNumber)
                break
            }
            if seg == -1 {
                panic("Segment == -1")
            }
            task.logger().Debugf("Getting segment %d", seg)
        }

        //the last segment often isn't available, so use less retries for it
        fails := task.FailThreshold
        if status.IsLast(seg) {
            //at least 5
            fails = task.FailThreshold / 4
            if fails < 5 {
                fails = 5
            }
        }

        if failCount >= fails {
            //retry again instantly, it'll grab a new client and go from there
            if networkFailCount > failCount / 2 {
                task.logger().Warnf("Suspicious network failures for segment %d, replacing http client", seg)
                task.replaceClient()

                failCount -= networkFailCount
                continue
            }
            if requeues < task.RequeueFailed && (!status.IsLast(seg) || task.RequeueLast) {
                task.logger().Warnf("Failed segment %d, requeue %d/%d", seg, requeues + 1, task.RequeueFailed)
                queue.RequeueFailed(seg, requeues + 1)
                task.Progress.requeued(seg)

                seg = -1
                failCount = 0
                continue
            }

            task.logger().Warnf("Giving up segment %d", seg)

            status.Downloaded(seg, segments.SegmentResult { Ok: false })
            task.Progress.lost()

            seg = -1
            failCount = 0
            continue
        }

        task.logger().Debugf("Current segment: %d", seg)

        ok, cached := downloadSegment(task, status, seg, &networkFailCount)
        if ok {
            task.Progress.done(seg, cached)

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

func downloadSegment(task *DownloadTask, status *segments.SegmentStatus, segment int, networkErrors *uint) (bool, bool) {
    segmentBasePath := segmentBaseFileName(task, segment)
    segmentDownloadPath := segmentBasePath + ".incomplete"
    segmentDonePath := segmentBasePath + ".done"

    //already downloaded
    if util.FileNotEmpty(segmentDonePath) {
        task.logger().Debugf("Segment %d already downloaded", segment)
        status.Downloaded(segment, segments.SegmentResult {
            Ok: true,
            Filename: segmentDonePath,
        })
        return true, true
    }

    targetUrl := getSegUrl(task.Url, segment)

    req, err := http.NewRequest("GET", targetUrl, nil)
    if err != nil {
        task.logger().Fatalf("Unable to create http request: %v", err)
    }
    req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/89.0.4389.90 Safari/537.36")

    resp, err := doRequest(task, req)
    if err != nil {
        *networkErrors++
        task.logger().Debugf("Request for segment %d failed with %v", segment, err)
        return false, false
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
        return false, false
    }

    file, err := os.OpenFile(segmentDownloadPath, os.O_RDWR|os.O_CREATE, 0644)
    if err != nil {
        task.logger().Warnf("Unable to create temp file for segment %d: %v", segment, err)
        return false, false
    }
    defer file.Close()

    if _, err = io.Copy(file, resp.Body); err != nil {
        os.Remove(file.Name())
        task.logger().Errorf("Unable to write segment %d: %v", segment, err)
        return false, false
    }

    if task.Fsync {
        if err = file.Sync(); err != nil {
            os.Remove(file.Name())
            task.logger().Errorf("Unable to sync segment %d: %v", segment, err)
            return false, false
        }
    }
    if err = file.Close(); err != nil {
        os.Remove(file.Name())
        task.logger().Errorf("Unable to close file for segment %d: %v", segment, err)
        return false, false
    }

    if err = os.Rename(segmentDownloadPath, segmentDonePath); err != nil {
        os.Remove(segmentDownloadPath)
        task.logger().Errorf("Unable to rename segment %d: %v", segment, err)
        return false, false
    }
    task.logger().Debugf("Downloaded segment %d", segment)

    status.Downloaded(segment, segments.SegmentResult {
        Ok: true,
        Filename: segmentDonePath,
    })

    return true, false
}

func doRequest(task *DownloadTask, req *http.Request) (*http.Response, error) {
    var errors []error
    for i := uint(0); i < task.RetryThreshold; i++ {
        resp, err := task.client().Do(req)
        if err == nil {
            return resp, nil
        }
        errors = append(errors, err)
    }
    return nil, fmt.Errorf("All requests failed: %v", errors)
}

