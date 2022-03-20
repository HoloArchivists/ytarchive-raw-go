package merge

import (
    "fmt"
    "strings"

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
}

func CreateBestMuxer(opts *MuxerOptions) (Muxer, error) {
    if err := testFfmpeg(); err != nil {
        return nil, fmt.Errorf("Unable to find FFmpeg: %v", err)
    }

    switch strings.ToLower(opts.Merger) {
    case "tcp":
        return CreateTcpMuxer(opts)
    case "concat":
        return CreateConcatMuxer(opts)
    case "":
        // nothing, autodetect below
    default:
        return nil, fmt.Errorf("Unknown merger '%s'", opts.Merger)
    }

    if hasProtocol("concatf") {
//        opts.Logger.Info("Using concatf protocol")
    }
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

type MuxerOptions struct {
    // should segments be deleted after merging?
    DeleteSegments  bool
    // where to save the muxed file
    FinalFile       string
    // video metadata
    FregData        *util.FregJson
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

