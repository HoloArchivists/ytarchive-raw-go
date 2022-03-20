package merge

import (
    "fmt"
    "io"
    "os"
    "path"
    "sync"
    "time"

    "github.com/notpeko/ytarchive-raw-go/download/segments"
    "github.com/notpeko/ytarchive-raw-go/log"
    "github.com/notpeko/ytarchive-raw-go/util"
)

var _ Muxer = &ConcatMuxer {}
type ConcatMuxer struct {
    opts        *MuxerOptions
    audioMerger *concatTask
    videoMerger *concatTask
}

func CreateConcatMuxer(options *MuxerOptions) (Muxer, error) {
    audioMerger, err := createConcatTask(options, "audio")
    if err != nil {
        return nil, err
    }

    videoMerger, err := createConcatTask(options, "video")
    if err != nil {
        return nil, err
    }

    return &ConcatMuxer {
        opts:        options,
        audioMerger: audioMerger,
        videoMerger: videoMerger,
    }, nil
}

func (m *ConcatMuxer) AudioMerger() Merger {
    return m.audioMerger
}

func (m *ConcatMuxer) VideoMerger() Merger {
    return m.videoMerger
}

func (m *ConcatMuxer) Mux() error {
    // need to wait for merged files to be available before muxing
    m.audioMerger.wg.Wait()
    m.videoMerger.wg.Wait()

    if err := muxFfmpeg(m.opts, m.audioMerger.targetFile, m.videoMerger.targetFile); err != nil {
        return err
    }

    m.opts.Logger.Debug("Download succeeded, removing merged segments")
    if err := os.Remove(m.audioMerger.targetFile); err != nil {
        m.opts.Logger.Warnf("Failed to remove merged audio: %v", err)
    }
    if err := os.Remove(m.videoMerger.targetFile); err != nil {
        m.opts.Logger.Warnf("Failed to remove merged video: %v", err)
    }

    return nil
}

var _ Merger = &concatTask {}
type concatTask struct {
    deleteSegments bool
    logger         *log.Logger
    wg             sync.WaitGroup
    targetFile     string
}

func createConcatTask(options *MuxerOptions, which string) (*concatTask, error) {
    file := path.Join(options.TempDir, fmt.Sprintf("merged-%s.%s", options.FregData.Metadata.Id, which))
    if util.FileNotEmpty(file) {
        if !options.OverwriteTemp {
            return nil, fmt.Errorf("Temporary merge file %s already exists and overwriting is disabled", file)
        }
        if err := os.Remove(file); err != nil {
            return nil, fmt.Errorf("Unable to delete temporary file %s: %v", file, err)
        }
    }

    task := &concatTask {
        deleteSegments: options.DeleteSegments,
        logger:         options.Logger.SubLogger(which),
        targetFile:     file,
    }
    task.wg.Add(1)
    return task, nil
}

func copyFile(from string, to string) error {
    in, err := os.Open(from)
    if err != nil {
        return fmt.Errorf("Unable to open input file: %v", err)
    }
    defer in.Close()

    out, err := os.OpenFile(to, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        return fmt.Errorf("Unable to open output file: %v", err)
    }
    defer out.Close()

    _, err = io.Copy(out, in)
    return err
}

func (t *concatTask) Merge(status *segments.SegmentStatus) {
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

        if result.Ok {
            err := copyFile(result.Filename, t.targetFile)
            if err != nil {
                t.logger.Errorf("Unable to merge file '%s' into '%s': %v", result.Filename, t.targetFile, err)
            } else {
                if t.deleteSegments {
                    os.Remove(result.Filename)
                }
            }
        }
    }
}

