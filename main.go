package main

import (
    "fmt"
    "io/ioutil"
    "os"
    "path/filepath"

    "github.com/mattn/go-colorable"

    "github.com/notpeko/ytarchive-raw-go/download"
    "github.com/notpeko/ytarchive-raw-go/log"
    "github.com/notpeko/ytarchive-raw-go/merge"
    "github.com/notpeko/ytarchive-raw-go/util"
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
    disableQuickEditMode()
    parseArgs()
    increaseOpenFileLimit()

    latestVersion, printNewVersion := versionCheck()
    if printNewVersion {
        log.Warnf("New version available: %s", latestVersion)
    }

    deleteTempDir := false
    if tempDir == "" {
        var err error
        tempDir, err = ioutil.TempDir("", fmt.Sprintf("ytarchive-%s-", fregData.Metadata.Id))
        if err != nil {
            log.Fatalf("Unable to create temp dir: %v", err)
        }
        log.Infof("Storing temporary files in %s", tempDir)
        deleteTempDir = !keepFiles
    } else {
        if err := os.MkdirAll(tempDir, 0755); err != nil {
            log.Fatalf("Unable to create temp dir at '%s': %v", tempDir, err)
        }
    }

    defer util.LockFile(filepath.Join(tempDir, fregData.Metadata.Id + ".lock"), func() {
        log.Error("This video is already being downloaded by another instance.")
        log.Error("Running two instances on the same video with the same temporary directory is not supported.")
    })()

    muxerOpts := &merge.MuxerOptions {
        DeleteSegments:  !keepFiles,
        DisableResume:   disableResume,
        FinalFileBase:   output,
        FregData:        &fregData,
        // this looks wrong but is correct
        IgnoreAudio:     onlyVideo,
        IgnoreVideo:     onlyAudio,
        Logger:          log.New("muxer"),
        Merger:          merger,
        MergerArguments: mergerArgs,
        OverwriteTemp:   overwriteTemp,
        TempDir:         tempDir,
    }

    if mergeOnlyFile != "" {
        log.Infof("Merging video from %s", mergeOnlyFile)
        if err := merge.MergeDownloadInfoJson(muxerOpts, mergeOnlyFile); err != nil {
            log.Fatalf("Failed to merge: %v", err)
        }
        log.Info("Success!")
        return
    }

    if keepFiles {
        log.Warnf("Temporary files are configured to not be deleted. This will fill up your temporary storage over time.");
    }

    var ipPool *util.IPPool
    if ipPoolFile != "" {
        var err error
        if ipPool, err = util.ParseIPPool(ipPoolFile); err != nil {
            log.Fatalf("Failed to parse IP pool: %v", err)
        }
    }

    client := util.NewClient(&util.HttpClientConfig {
        IPPool:  ipPool,
        Network: network,
        UseQuic: useQuic,
    })

    muxer, err := merge.CreateBestMuxer(muxerOpts)
    if err != nil {
        log.Fatalf("Unable to create muxer: %v", err)
    }

    dir := filepath.Dir(muxer.OutputFilePath())
    err = os.MkdirAll(dir, 0755)
    if err != nil {
        log.Fatalf("Unable to create parent directories for output file: %v", err)
    }

    defer util.LockFile(muxer.OutputFilePath() + ".lock", func() {
        log.Error("Another instance is already writing to this output file.")
    })()

    log.SetWindowName(windowName)
    progress := download.NewProgress()

    audioTask := &download.DownloadTask {
        Client:         client,
        FailThreshold:  failThreshold,
        Fsync:          fsync,
        Logger:         log.New("download.audio"),
        Merger:         muxer.AudioMerger(),
        Progress:       progress.Audio(),
        QueueMode:      queueMode,
        RequeueDelay:   requeueDelay,
        RequeueFailed:  requeueFailed,
        RequeueLast:    requeueLast,
        RetryThreshold: retryThreshold,
        SegmentCount:   segmentCount,
        SegmentDir:     tempDir,
        Threads:        threads,
        Url:            fregData.BestAudio(preferredAudio),
    }
    videoTask := &download.DownloadTask {
        Client:         client,
        FailThreshold:  failThreshold,
        Fsync:          fsync,
        Logger:         log.New("download.video"),
        Merger:         muxer.VideoMerger(),
        Progress:       progress.Video(),
        QueueMode:      queueMode,
        RequeueDelay:   requeueDelay,
        RequeueFailed:  requeueFailed,
        RequeueLast:    requeueLast,
        RetryThreshold: retryThreshold,
        SegmentCount:   segmentCount,
        SegmentDir:     tempDir,
        Threads:        threads,
        Url:            fregData.BestVideo(preferredVideo),
    }

    if onlyAudio {
        videoTask = nil
        merge.MergeNothing(muxer.VideoMerger())
    } else if onlyVideo {
        audioTask = nil
        merge.MergeNothing(muxer.AudioMerger())
    }

    if audioTask != nil {
        audioTask.Start()
    }
    if videoTask != nil {
        videoTask.Start()
    }

    //start muxer early so segments can be deleted if keep-files is disabled
    //for the tcp muxer
    muxerResult := make(chan error)
    go func() {
        muxerResult <- muxer.Mux()
    }()

    var audioRes, videoRes *download.DownloadResult

    if audioTask != nil {
        audioRes = audioTask.Wait()
    }
    if videoTask != nil {
        videoRes = videoTask.Wait()
    }

    if audioTask != nil {
        printResult(audioTask.Logger, audioRes)
    }
    if videoTask != nil {
        printResult(videoTask.Logger, videoRes)
    }

    log.Info("Waiting for muxing to finish")
    log.Info("This can take a while for long videos, do NOT restart or all muxing progress will be lost")
    res := <-muxerResult

    //print again once it's done so it doesn't get buried in newer logs
    if printNewVersion {
        log.Warnf("New version available: %s", latestVersion)
    }
    if keepFiles {
        log.Warnf("Temporary files are configured to not be deleted. This will fill up your temporary storage over time.");
    }

    if res != nil {
        log.Fatalf("Muxing failed: %v", res)
    }

    if deleteTempDir {
        if err = os.RemoveAll(tempDir); err != nil {
            log.Warnf("Failed to delete temp dir: %v", err)
        }
    }

    log.Info("Success!")
    fmt.Fprintf(os.Stderr, "\n")
}

