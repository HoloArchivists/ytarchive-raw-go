package merge

import (
    "fmt"

    "github.com/notpeko/ytarchive-raw-go/download/segments"
    "github.com/notpeko/ytarchive-raw-go/log"
    "github.com/notpeko/ytarchive-raw-go/util"
)

type Merger interface {
    Merge(*segments.SegmentStatus)
    Wait()
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
    if hasProtocol("concatf") {
//        opts.Logger.Info("Using concatf protocol")
    }
    if hasProtocol("tcp") {
//        opts.Logger.Info("Using tcp protocol")
    }
    if hasProtocol("file") {
        opts.Logger.Warn("Using concat merger")
        return CreateConcatMuxer(opts)
    }
    return nil, fmt.Errorf("No supported protocol in ffmpeg, tried concatf, tcp and file")
}

type MuxerOptions struct {
    // should segments be deleted after merging?
    DeleteSegments bool
    // where to save the muxed file
    FinalFile      string
    // video metadata
    FregData       *util.FregJson
    Logger         *log.Logger
    // if temporary files already exist, should they be overwritten?
    OverwriteTemp  bool
    // directory to store temporary files
    TempDir        string
}

