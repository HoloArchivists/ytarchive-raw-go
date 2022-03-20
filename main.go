package main

import (
    "net/http"

    "github.com/lucas-clemente/quic-go/http3"

    "github.com/notpeko/ytarchive-raw-go/download"
    "github.com/notpeko/ytarchive-raw-go/log"
)

func printResult(logger *log.Logger, res *download.DownloadResult) {
    if len(res.LostSegments) > 0 {
        logger.Warnf("Lost %d segment(s) %v out of %d", len(res.LostSegments), res.LostSegments, res.TotalSegments)
    }
    if res.Error != nil {
        logger.Errorf("Download task failed: %v", res.Error)
    } else {
        logger.Info("Download succeeded")
    }
}

func main() {
    client := &http.Client {
        Transport: &http3.RoundTripper{},
    }
    audioTask := &download.DownloadTask {
        Client:         client,
        DeleteSegments: true,
        Logger:         log.New("audio.0"),
        MergeFile:      "/tmp/out.audio.0",
        QueueMode:      queueMode,
        SegmentDir:     "/tmp/ytarchive_test",
        Threads:        threads,
        Url:            fregData.BestAudio(),
    }
    videoTask := &download.DownloadTask {
        Client:         client,
        DeleteSegments: true,
        Logger:         log.New("video.0"),
        MergeFile:      "/tmp/out.video.0",
        QueueMode:      queueMode,
        SegmentDir:     "/tmp/ytarchive_test",
        Threads:        threads,
        Url:            fregData.BestVideo(),
    }

    audioTask.Start()
    videoTask.Start()

    audioRes := audioTask.Wait()
    videoRes := videoTask.Wait()

    printResult(audioTask.Logger, audioRes)
    printResult(videoTask.Logger, videoRes)
}

