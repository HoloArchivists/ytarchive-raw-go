package main

import (
    "net/http"

    "github.com/lucas-clemente/quic-go/http3"

    "github.com/notpeko/ytarchive-raw-go/download"
    "github.com/notpeko/ytarchive-raw-go/log"
)

func main() {
    client := &http.Client {
        Transport: &http3.RoundTripper{},
    }
    task := &download.DownloadTask {
        Client:  client,
        Url:     fregData.Audio["140"],
        TmpFile: "/tmp/out.audio.0",
        Logger:  log.New("audio.0"),
        Threads: threads,
    }
    task.Start()
    res := task.Wait()
    if res.Error != nil {
        log.Errorf("Download task failed: %v", res.Error)
    } else {
        log.Info("Download succeeded")
    }
}

