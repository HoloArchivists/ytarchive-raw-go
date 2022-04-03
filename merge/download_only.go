package merge

import (
    "encoding/json"
    "fmt"
    "io/ioutil"

    "github.com/HoloArchivists/ytarchive-raw-go/download/segments"
    "github.com/HoloArchivists/ytarchive-raw-go/log"
    "github.com/HoloArchivists/ytarchive-raw-go/util"
)

var _ Muxer = &DownloadOnlyMuxer {}
type DownloadOnlyMuxer struct {
    opts        *MuxerOptions
    progress    *mergeProgress
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
    if options.Merger == "download-only" {
        return fmt.Errorf("download-only is not a valid merger for --merge")
    }

    data, err := ioutil.ReadFile(path)
    if err != nil {
        return err
    }

    var info downloadJson
    if err = json.Unmarshal(data, &info); err != nil {
        return fmt.Errorf("Unable to parse json (is it a file created by the download-only merger?): %v", err)
    }

    options.FregData = info.FregData

    if info.AudioSegments == nil {
        options.IgnoreAudio = true
    }
    if info.VideoSegments == nil {
        options.IgnoreVideo = true
    }

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

    // no need to handle deleting segments here, the called merger will deal with that
    return mux.Mux()
}

func CreateDownloadOnlyMuxer(options *MuxerOptions) (Muxer, error) {
    progress := newProgress()
    return &DownloadOnlyMuxer {
        opts:        options,
        progress:    progress,
        audioMerger: createDownloadOnlyTask(options, progress, "audio"),
        videoMerger: createDownloadOnlyTask(options, progress, "video"),
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
    m.progress.done()

    d := &downloadJson {
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
    taskCommon
    segments   []segments.SegmentResult
}

func createDownloadOnlyTask(options *MuxerOptions, progress *mergeProgress, which string) *downloadOnlyTask {
    task := &downloadOnlyTask {
        taskCommon: taskCommon {
            ffmpegInput: "nil",
            options:     options,
            progress:    progress,
            which:       which,
        },
    }
    task.wg.Add(1)
    return task
}

func (t *downloadOnlyTask) Merge(status *segments.SegmentStatus) {
    defer t.wg.Done()

    t.forEachSegment(status, func(result segments.SegmentResult) {
        t.segments = append(t.segments, result)
    })
}

