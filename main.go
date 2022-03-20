package main

import (
    "fmt"
    "io/ioutil"
    "net/http"
    "os"
    "path"

    "github.com/gofrs/flock"
    "github.com/lucas-clemente/quic-go/http3"
    "github.com/mattn/go-colorable"

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
    colorable.EnableColorsStdout(nil)
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
    lock := flock.New(path.Join(tempDir, fregData.Metadata.Id + ".lock"))
    locked, err := lock.TryLock()
    if err != nil {
        log.Fatalf("Failed to lock file: %v", err)
    }
    if !locked {
        log.Error("This video is already being downloaded by another instance.")
        log.Error("Running two instances on the same video with the same temporary directory is not supported.")
        os.Exit(1)
    }
    defer lock.Unlock()

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

    progress := download.NewProgress()

    audioTask := &download.DownloadTask {
        Client:         client,
        FailThreshold:  failThreshold,
        Fsync:          fsync,
        Logger:         log.New("audio.0"),
        Merger:         muxer.AudioMerger(),
        Progress:       progress.Audio(),
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
        Progress:       progress.Video(),
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

