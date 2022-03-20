package merge

import (
    "encoding/json"
    "fmt"
    "io/ioutil"
    "sync"
    "time"

    "github.com/notpeko/ytarchive-raw-go/download/segments"
    "github.com/notpeko/ytarchive-raw-go/log"
    "github.com/notpeko/ytarchive-raw-go/util"
)

var _ Muxer = &DownloadOnlyMuxer {}
type DownloadOnlyMuxer struct {
    opts        *MuxerOptions
    audioMerger *downloadOnlyTask
    videoMerger *downloadOnlyTask
}

type downloadJson struct {
    FregData          *util.FregJson
    AudioSegments     []segments.SegmentResult
    VideoSegments     []segments.SegmentResult
}

func feedMerger(merger Merger, data []segments.SegmentResult) {
    s := segments.Create(len(data), 1, segments.QueueSequential, 0)
    go merger.Merge(s)
    for idx, v := range data {
        s.Downloaded(idx, v)
    }
}

func MergeDownloadInfoJson(options *MuxerOptions, path string) error {
    data, err := ioutil.ReadFile(path)
    if err != nil {
        return err
    }

    var info downloadJson
    if err = json.Unmarshal(data, &info); err != nil {
        return fmt.Errorf("Unable to parse json (is it a file created by the download-only merger?): %v", err)
    }

    options.FregData = info.FregData

    output, err := options.FregData.FormatTemplate(options.FinalFileBase, true)
    if err != nil {
        return err
    }
    options.FinalFileBase = output

    defer util.LockFile(output + ".lock", func() {
        log.Error("Another instance is already writing to this output file.")
    })()

    options.Logger.Infof("Saving output to %s", output)

    mux, err := CreateBestMuxer(options)
    if err != nil {
        return fmt.Errorf("Unable to create muxer: %v", err)
    }

    go feedMerger(mux.AudioMerger(), info.AudioSegments)
    go feedMerger(mux.VideoMerger(), info.VideoSegments)

    return mux.Mux()
}

func CreateDownloadOnlyMuxer(options *MuxerOptions) (Muxer, error) {
    return &DownloadOnlyMuxer {
        opts:        options,
        audioMerger: createDownloadOnlyTask(options, "audio"),
        videoMerger: createDownloadOnlyTask(options, "video"),
    }, nil
}

func (m *DownloadOnlyMuxer) AudioMerger() Merger {
    return m.audioMerger
}

func (m *DownloadOnlyMuxer) VideoMerger() Merger {
    return m.videoMerger
}

func (m *DownloadOnlyMuxer) Mux() error {
    m.audioMerger.wg.Wait()
    m.videoMerger.wg.Wait()

    d := downloadJson {
        FregData:      m.opts.FregData,
        AudioSegments: m.audioMerger.segments,
        VideoSegments: m.videoMerger.segments,
    }

    j, err := json.Marshal(d)
    if err != nil {
        return err
    }
    return ioutil.WriteFile(m.opts.FinalFileBase + ".json", j, 0644)
}

func (m *DownloadOnlyMuxer) OutputFilePath() string {
    return m.opts.FinalFileBase + ".json"
}

var _ Merger = &downloadOnlyTask {}
type downloadOnlyTask struct {
    logger   *log.Logger
    wg       sync.WaitGroup
    segments []segments.SegmentResult
}

func createDownloadOnlyTask(options *MuxerOptions, which string) *downloadOnlyTask {
    task := &downloadOnlyTask {
        logger: options.Logger.SubLogger(which),
    }
    task.wg.Add(1)
    return task
}

func (t *downloadOnlyTask) Merge(status *segments.SegmentStatus) {
    defer t.wg.Done()

    misses := 0
    for {
        if status.Done() {
            break
        }
        result, number, done := status.NextToMerge()
        if !done {
            t.logger.Debugf("Waiting for segment %d to be ready for merging", number)
            time.Sleep(time.Duration(misses + 1) * time.Second)
            misses++
            //up to 10s wait
            if misses > 9 {
                misses = 9
            }
            continue
        }
        misses = 0

        t.segments = append(t.segments, result)
    }
}

