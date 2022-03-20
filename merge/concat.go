package merge

import (
    "fmt"
    "io"
    "os"
    "path/filepath"

    "github.com/notpeko/ytarchive-raw-go/download/segments"
    "github.com/notpeko/ytarchive-raw-go/util"
)

var _ Muxer = &ConcatMuxer {}
type ConcatMuxer struct {
    opts        *MuxerOptions
    progress    *mergeProgress
    audioMerger *concatTask
    videoMerger *concatTask
}

func CreateConcatMuxer(options *MuxerOptions) (Muxer, error) {
    progress := newProgress()

    audioMerger, err := createConcatTask(options, progress, "audio")
    if err != nil {
        return nil, err
    }

    videoMerger, err := createConcatTask(options, progress, "video")
    if err != nil {
        return nil, err
    }

    return &ConcatMuxer {
        opts:        options,
        progress:    progress,
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

    m.opts.Logger.Info("Merging into final file, progress won't be updated until it's done")

    if err := muxFfmpeg(m.opts, m.audioMerger.output(), m.videoMerger.output()); err != nil {
        return err
    }
    m.progress.done()

    m.opts.Logger.Debug("Download succeeded, removing merged segments")
    m.audioMerger.do(func() {
        if err := os.Remove(m.audioMerger.output()); err != nil {
            m.opts.Logger.Warnf("Failed to remove merged audio: %v", err)
        }
    })
    m.videoMerger.do(func() {
        if err := os.Remove(m.videoMerger.output()); err != nil {
            m.opts.Logger.Warnf("Failed to remove merged video: %v", err)
        }
    })

    if m.opts.DeleteSegments {
        deleteSegmentFiles(m.audioMerger.segments)
        deleteSegmentFiles(m.videoMerger.segments)
    }

    return nil
}

func (m *ConcatMuxer) OutputFilePath() string {
    return m.opts.FinalFileBase + ".mkv"
}

var _ Merger = &concatTask {}
type concatTask struct {
    taskCommon
    deleteSegments bool
    segments       []string
}

func createConcatTask(options *MuxerOptions, progress *mergeProgress, which string) (*concatTask, error) {
    file := filepath.Join(options.TempDir, fmt.Sprintf("merged-%s.%s", options.FregData.Metadata.Id, which))
    if util.FileNotEmpty(file) {
        if !options.OverwriteTemp {
            return nil, fmt.Errorf("Temporary merge file %s already exists and overwriting is disabled", file)
        }
        if err := os.Remove(file); err != nil {
            return nil, fmt.Errorf("Unable to delete temporary file %s: %v", file, err)
        }
    }

    task := &concatTask {
        taskCommon:     taskCommon {
            ffmpegInput: file,
            options:     options,
            progress:    progress,
            which:       which,
        },
        deleteSegments: options.DisableResume,
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

    t.forEachSegment(status, func(result segments.SegmentResult) {
        if result.Ok {
            target := t.ffmpegInput
            err := copyFile(result.Filename, target)
            if err != nil {
                t.log().Errorf("Unable to merge file '%s' into '%s': %v", result.Filename, target, err)
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

