package download

import (
    "fmt"
    "io"
    "os"
    "sync"
    "time"
)

type mergeTask struct {
    owner      *DownloadTask
    wg         sync.WaitGroup
    targetFile string
    notMerged  []int
    status     *segmentStatus
}

func makeMergeTask(owner *DownloadTask, status *segmentStatus, targetFile string) *mergeTask {
    task := &mergeTask {
        owner:  owner,
        status: status,
        targetFile: targetFile,
    }
    task.wg.Add(1)
    go task.doMerge()
    return task
}

func (m *mergeTask) wait() {
    m.wg.Wait()
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

func (m *mergeTask) doMerge() {
    defer m.wg.Done()

    currentSeg := 0
    misses := 0
    for {
        if m.status.done() {
            break
        }
        result, done := m.status.result(currentSeg)
        if !done {
            m.owner.logger().Debugf("Waiting for segment %d to be ready for merging", currentSeg)
            time.Sleep(time.Duration(misses + 1) * time.Second)
            misses++
            //up to 10s wait
            if misses > 9 {
                misses = 9
            }
            continue
        }
        misses = 0

        if result.ok {
            err := copyFile(result.filename, m.targetFile)
            if err != nil {
                m.owner.logger().Errorf("Unable to merge file '%s' into '%s': %v", result.filename, m.targetFile, err)
            } else {
                if m.owner.DeleteSegments {
                    os.Remove(result.filename)
                }
            }
        } else {
            m.notMerged = append(m.notMerged, currentSeg)
        }

        m.status.merged(currentSeg)
        currentSeg++
    }
}

