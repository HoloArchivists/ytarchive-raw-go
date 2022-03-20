package merge

import (
    "fmt"
    "io"
    "net"
    "os"
    "sync"
    "time"

    "github.com/notpeko/ytarchive-raw-go/download/segments"
    "github.com/notpeko/ytarchive-raw-go/log"
)

var _ Muxer = &TcpMuxer {}
type TcpMuxer struct {
    opts        *MuxerOptions
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


    audioMerger, err := createTcpTask(bindAddress, options, "audio")
    if err != nil {
        return nil, err
    }

    videoMerger, err := createTcpTask(bindAddress, options, "video")
    if err != nil {
        return nil, err
    }

    return &TcpMuxer {
        opts:        options,
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
    if err := muxFfmpeg(m.opts, m.audioMerger.addr(), m.videoMerger.addr()); err != nil {
        return err
    }

    m.audioMerger.listener.Close()
    m.videoMerger.listener.Close()

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
    deleteSegments bool
    listener       net.Listener
    logger         *log.Logger
    segments       []string
    wg             sync.WaitGroup
}

func createTcpTask(bindAddress string, options *MuxerOptions, which string) (*tcpTask, error) {
    l, err := net.Listen("tcp", net.JoinHostPort(bindAddress, "0"))
    if err != nil {
        return nil, fmt.Errorf("Unable to start listening: %v", err)
    }

    task := &tcpTask {
        deleteSegments: options.DisableResume,
        listener:       l,
        logger:         options.Logger.SubLogger(which),
    }

    task.logger.Debugf("Listening on address %v", l.Addr())
    task.wg.Add(1)
    return task, nil
}

func (t *tcpTask) addr() string {
    return "tcp://" + t.listener.Addr().String()
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
    conn, err := t.listener.Accept()
    if err != nil {
        t.logger.Errorf("Unable to accept connection: %v", err)
        return
    }
    t.logger.Info("Got connection")

    misses := 0
    for {
        if status.Done() {
            conn.Close()
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

        if result.Ok {
            err := sendFile(result.Filename, conn)
            if err != nil {
                t.logger.Errorf("Unable to send file '%s' to muxer: %v", result.Filename, err)
            } else {
                if t.deleteSegments {
                    os.Remove(result.Filename)
                } else {
                    t.segments = append(t.segments, result.Filename)
                }
            }
        }
    }
}

