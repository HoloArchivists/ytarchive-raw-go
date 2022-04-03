package merge

import (
    "fmt"
    "io"
    "net"
    "os"

    "github.com/HoloArchivists/ytarchive-raw-go/download/segments"
)

var _ Muxer = &TcpMuxer {}
type TcpMuxer struct {
    opts        *MuxerOptions
    progress    *mergeProgress
    audioMerger *tcpTask
    videoMerger *tcpTask
}

func CreateTcpMuxer(options *MuxerOptions) (Muxer, error) {
    var bindAddress string
    if addr, ok := options.getMergerArgument("tcp", "bind_address"); ok {
        if net.ParseIP(addr) == nil {
            return nil, fmt.Errorf("Invalid ip address '%s'", addr)
        }
        bindAddress = addr
    } else {
        bindAddress = "127.0.0.1"
    }

    progress := newProgress()

    audioMerger, err := createTcpTask(bindAddress, options, progress, "audio")
    if err != nil {
        return nil, err
    }

    videoMerger, err := createTcpTask(bindAddress, options, progress, "video")
    if err != nil {
        return nil, err
    }

    return &TcpMuxer {
        opts:        options,
        progress:    progress,
        audioMerger: audioMerger,
        videoMerger: videoMerger,
    }, nil
}

func (m *TcpMuxer) AudioMerger() Merger {
    return m.audioMerger
}

func (m *TcpMuxer) VideoMerger() Merger {
    return m.videoMerger
}

func (m *TcpMuxer) Mux() error {
    if err := muxFfmpeg(m.opts, m.audioMerger.output(), m.videoMerger.output()); err != nil {
        return err
    }
    m.progress.done()

    if m.audioMerger.listener != nil {
        m.audioMerger.listener.Close()
    }
    if m.videoMerger.listener != nil {
        m.videoMerger.listener.Close()
    }

    if m.opts.DeleteSegments {
        deleteSegmentFiles(m.audioMerger.segments)
        deleteSegmentFiles(m.videoMerger.segments)
    }

    return nil
}

func (m *TcpMuxer) OutputFilePath() string {
    return m.opts.FinalFileBase + ".mkv"
}

var _ Merger = &tcpTask {}
type tcpTask struct {
    taskCommon
    deleteSegments bool
    listener       net.Listener
    segments       []string
}

func createTcpTask(bindAddress string, options *MuxerOptions, progress *mergeProgress, which string) (*tcpTask, error) {
    task := &tcpTask {
        taskCommon:     taskCommon {
            options:     options,
            progress:    progress,
            which:       which,
        },
        deleteSegments: options.DisableResume,
    }

    if !task.ignored() {
        l, err := net.Listen("tcp", net.JoinHostPort(bindAddress, "0"))
        if err != nil {
            return nil, fmt.Errorf("Unable to start listening: %v", err)
        }
        task.log().Debugf("Listening on address %v", l.Addr())
        task.listener = l
        task.ffmpegInput = "tcp://" + l.Addr().String()
    }

    return task, nil
}

func sendFile(file string, conn net.Conn) error {
    f, err := os.Open(file)
    if err != nil {
        return err
    }
    defer f.Close()

    _, err = io.Copy(conn, f)
    return err
}

func (t *tcpTask) Merge(status *segments.SegmentStatus) {
    if t.listener == nil {
        t.forEachSegment(status, func(_ segments.SegmentResult) {})
        return
    }

    conn, err := t.listener.Accept()
    if err != nil {
        t.log().Errorf("Unable to accept connection: %v", err)
        return
    }
    defer conn.Close()

    t.log().Info("Got connection")
    t.forEachSegment(status, func(result segments.SegmentResult) {
        if result.Ok {
            err := sendFile(result.Filename, conn)
            if err != nil {
                t.log().Errorf("Unable to send file '%s' to muxer: %v", result.Filename, err)
            } else {
                if t.deleteSegments {
                    os.Remove(result.Filename)
                } else {
                    t.segments = append(t.segments, result.Filename)
                }
            }
        }
    })
}

