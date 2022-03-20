package main

import (
    "fmt"
    "io/ioutil"
    "net/http"
    "os"

    "github.com/lucas-clemente/quic-go/http3"

    "github.com/notpeko/ytarchive-raw-go/download"
    "github.com/notpeko/ytarchive-raw-go/log"
    "github.com/notpeko/ytarchive-raw-go/merge"
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
    parseArgs()

    if tempDir == "" {
        var err error
        tempDir, err = ioutil.TempDir("", fmt.Sprintf("ytarchive-%s-", fregData.Metadata.Id))
        if err != nil {
            log.Fatalf("Unable to create temp dir: %v", err)
        }
        log.Info("Storing temporary files in %s", tempDir)
    } else {
        if err := os.MkdirAll(tempDir, 0755); err != nil {
            log.Fatalf("Unable to create temp dir at '%s': %v", tempDir, err)
        }
    }

    var rt http.RoundTripper
    if useQuic {
        rt = &http3.RoundTripper {}
    }
    client := &http.Client {
        Transport: rt,
    }

    muxer, err := merge.CreateBestMuxer(&merge.MuxerOptions {
        DeleteSegments: !keepFiles,
        FinalFile:      output,
        FregData:       &fregData,
        Logger:         log.New("muxer.0"),
        OverwriteTemp:  overwriteTemp,
        TempDir:        tempDir,
    })
    if err != nil {
        log.Fatalf("Unable to create muxer: %v", err)
    }

    audioTask := &download.DownloadTask {
        Client:         client,
        FailThreshold:  failThreshold,
        Fsync:          fsync,
        Logger:         log.New("audio.0"),
        Merger:         muxer.AudioMerger(),
        QueueMode:      queueMode,
        RetryThreshold: retryThreshold,
        SegmentDir:     tempDir,
        Threads:        threads,
        Url:            fregData.BestAudio(),
    }
    videoTask := &download.DownloadTask {
        Client:         client,
        FailThreshold:  failThreshold,
        Fsync:          fsync,
        Logger:         log.New("video.0"),
        Merger:         muxer.VideoMerger(),
        QueueMode:      queueMode,
        RetryThreshold: retryThreshold,
        SegmentDir:     tempDir,
        Threads:        threads,
        Url:            fregData.BestVideo(),
    }

    audioTask.Start()
    videoTask.Start()

    //start muxer early so segments can be deleted if keep-files is disabled
    //for the tcp muxer
    muxerResult := make(chan error)
    go func() {
        muxerResult <- muxer.Mux()
    }()

    audioRes := audioTask.Wait()
    videoRes := videoTask.Wait()

    printResult(audioTask.Logger, audioRes)
    printResult(videoTask.Logger, videoRes)

    log.Info("Waiting for muxing to finish")
    res := <-muxerResult
    if res != nil {
        log.Fatalf("Muxing failed: %v", res)
    }
    log.Info("Success!")
}

