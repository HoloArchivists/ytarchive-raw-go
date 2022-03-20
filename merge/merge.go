package merge

import (
    "fmt"
    "os"
    "strings"
    "sync"
    "time"

    "github.com/notpeko/ytarchive-raw-go/download/segments"
    "github.com/notpeko/ytarchive-raw-go/log"
    "github.com/notpeko/ytarchive-raw-go/util"
)

type Merger interface {
    Merge(*segments.SegmentStatus)
}

type Muxer interface {
    AudioMerger() Merger
    VideoMerger() Merger
    Mux() error
    OutputFilePath() string
}

func CreateBestMuxer(opts *MuxerOptions) (Muxer, error) {
    if err := testFfmpeg(); err != nil {
        return nil, fmt.Errorf("Unable to find FFmpeg: %v", err)
    }

    if opts.IgnoreAudio && opts.IgnoreVideo {
        return nil, fmt.Errorf("Ignoring both audio and video")
    }

    switch strings.ToLower(opts.Merger) {
    case "download-only":
        return CreateDownloadOnlyMuxer(opts)
    case "tcp":
        return CreateTcpMuxer(opts)
    case "concat":
        return CreateConcatMuxer(opts)
    case "":
        // nothing, autodetect below
    default:
        return nil, fmt.Errorf("Unknown merger '%s'", opts.Merger)
    }

    //probably not worth implementing, tcp is objectively better,
    //download-only can be used as an alternative if tcp is missing.
//    if hasProtocol("concatf") {
//        opts.Logger.Info("Using concatf protocol")
//    }
    if hasProtocol("tcp") {
        opts.Logger.Info("Using tcp protocol")
        return CreateTcpMuxer(opts)
    }
    if hasProtocol("file") {
        opts.Logger.Warn("Using concat merger")
        return CreateConcatMuxer(opts)
    }
    return nil, fmt.Errorf("No supported protocol in ffmpeg, tried concatf, tcp and file")
}

func MergeNothing(m Merger) {
    s := segments.Create(0, 1, segments.QueueSequential, 0)
    go m.Merge(s)
}

type MuxerOptions struct {
    // should segments be deleted after successfully muxing?
    DeleteSegments  bool
    // should segments be deleted after merging?
    DisableResume   bool
    // where to save the muxed file
    FinalFileBase   string
    // video metadata
    FregData        *util.FregJson
    // don't include audio
    IgnoreAudio     bool
    // don't include video
    IgnoreVideo     bool
    Logger          *log.Logger
    // which merger to use
    Merger          string
    // arguments for the mergers
    MergerArguments map[string]map[string]string
    // if temporary files already exist, should they be overwritten?
    OverwriteTemp   bool
    // directory to store temporary files
    TempDir         string
}

func (opts *MuxerOptions) getMergerArgument(name, arg string) (string, bool) {
    m, ok := opts.MergerArguments[strings.ToLower(name)]
    if !ok {
        return "", false
    }
    v, ok := m[strings.ToLower(arg)]
    return v, ok
}

type taskCommon struct {
    ffmpegInput string
    _logger     *log.Logger
    options     *MuxerOptions
    progress    *mergeProgress
    wg          sync.WaitGroup
    which       string
}

func (t* taskCommon) log() *log.Logger {
    if t._logger == nil {
        t._logger = t.options.Logger.SubLogger(t.which)
    }
    return t._logger
}

func (t* taskCommon) ignored() bool {
    return (t.which == "audio" && t.options.IgnoreAudio) || (t.which == "video" && t.options.IgnoreVideo)
}

func (t* taskCommon) output() string {
    if t.ignored() {
        return ""
    }
    return t.ffmpegInput
}

func (t* taskCommon) do(f func()) {
    if !t.ignored() {
        f()
    }
}

func (t* taskCommon) forEachSegment(s *segments.SegmentStatus, f func(segments.SegmentResult)) {
    if t.ignored() {
        t.progress.initTotal(0)
        return
    }

    t.progress.initTotal(s.Total())
    misses := 0
    for {
        if s.Done() {
            break
        }
        result, number, done := s.NextToMerge()
        if !done {
            t.log().Debugf("Waiting for segment %d to be ready for merging", number)
            if misses < 10 {
                misses++
            }
            time.Sleep(time.Duration(misses) * time.Second)
            continue
        }
        misses = 0

        f(result)

        if t.which == "audio" {
            t.progress.mergedAudio()
        } else {
            t.progress.mergedVideo()
        }
    }
}

func deleteSegmentFiles(paths []string) {
    for _, v := range paths {
        if err := os.Remove(v); err != nil {
            log.Warnf("Failed to remove segment file %s: %v", v, err)
        }
    }
}

