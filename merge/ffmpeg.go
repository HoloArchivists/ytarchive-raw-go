package merge

import (
    "bufio"
    "bytes"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "strings"

    "github.com/HoloArchivists/ytarchive-raw-go/log"
)

func ffmpeg(logger *log.Logger, args ...string) *exec.Cmd {
    argv := make([]string, 0)
    argv = append(argv, "-v", "warning")
    argv = append(argv, args...)
    if logger != nil {
        logger.Debugf("FFmpeg command: %v", argv)
    }
    return exec.Command("ffmpeg", argv...)
}

func testFfmpeg() error {
    cmd := ffmpeg(log.DefaultLogger, "-h")
    cmd.Stdin = nil
    cmd.Stdout = nil
    cmd.Stderr = nil
    return cmd.Run()
}

func hasProtocol(name string) bool {
    cmd := ffmpeg(log.DefaultLogger, "--help", "protocol=" + name)
    cmd.Stdin = nil
    output, err := cmd.Output()
    if err != nil {
        log.Fatalf("Unable to find ffmpeg: %v", err)
    }
    return !bytes.Contains(output, []byte("Unknown protocol "))
}

func muxFfmpeg(options *MuxerOptions, audio, video string) error {
    if audio == "" && video == "" {
        return fmt.Errorf("No audio or video inputs provided")
    }
    args := make([]string, 0)
    args = append(
        args,
        "-loglevel",
        "level+40",
        "-y",
    )
    if audio != "" {
        args = append(args, "-i", audio)
    }
    if video != "" {
        args = append(args, "-i", video)
    }
    args = append(args, "-c", "copy")

    thumbnail := options.FinalFileBase + ".jpg"
    if err := options.FregData.WriteThumbnail(thumbnail); err != nil {
        return fmt.Errorf("Unable to write thumbnail file: %v", err)
    }
    args = append(
        args,
        "-metadata",
        "date=" + options.FregData.Metadata.StartTimestamp.Format("20060201"),
        "-metadata",
        "title=" + options.FregData.Metadata.Title,
        "-metadata",
        "comment=" + options.FregData.Metadata.Description,
        "-metadata",
        "author=" + options.FregData.Metadata.ChannelName,
        "-metadata",
        "artist=" + options.FregData.Metadata.ChannelName,
        "-metadata",
        "episode_id=" + options.FregData.Metadata.Id,
        "-attach",
        thumbnail,
        "-metadata:s:t",
        "mimetype=image/jpeg",
        "-metadata:s:t",
        "filename=thumbnail.jpg",
    )
    args = append(args, options.FinalFileBase + ".mkv")

    cmd := ffmpeg(options.Logger, args...)
    logFile := filepath.Join(options.TempDir, fmt.Sprintf("ffmpeg-%s.out", options.FregData.Metadata.Id))
    cmd.Env = append(
        os.Environ(),
        fmt.Sprintf("FFREPORT=file='%s'", logFile),
    )
    cmd.Stdin = nil

    var stderr bytes.Buffer
    cmd.Stdout = nil
    cmd.Stderr = &stderr

    if err := cmd.Run(); err != nil {
        printOutput(options.Logger, &stderr, false)
        options.Logger.Errorf("Check the FFmpeg log file at '%s'", logFile)
        return err
    }
    printOutput(options.Logger, &stderr, true)

    return nil
}

func printOutput(logger *log.Logger, stderr *bytes.Buffer, success bool) {
    warnings := make([]string, 0)
    reader := bufio.NewReader(stderr)

    for {
        line, err := reader.ReadString('\n')
        line = strings.TrimSuffix(line, "\r")
        if len(line) > 0 {
            if !ignoreWarning(line) {
                warnings = append(warnings, line)
            }
        }
        if err != nil {
            break
        }
    }

    if success {
        if len(warnings) == 0 {
            return
        }
        logger.Warn("FFmpeg succeeded with warnings")
    } else {
        logger.Error("FFmpeg failed")
    }
    for _, v := range warnings {
        logger.Warn(v)
    }
}

var ignoredWarnings = []string {
    "    Last message repeated ",
    "Found duplicated MOOV Atom. Skipped it",
    "Found unknown-length element with ID 0x18538067 at pos.",
    "Thread message queue blocking;",
}

var wantedLevels = []string {
    "[panic]",
    "[fatal]",
    "[error]",
    "[warning]",
}

func ignoreWarning(line string) bool {
    var wantedLevel bool
    for _, v := range wantedLevels {
        if strings.Contains(line, v) {
            wantedLevel = true
            break
        }
    }
    if !wantedLevel {
        return true
    }

    for _, v := range ignoredWarnings {
        if strings.Contains(line, v) {
            return true
        }
    }
    return false
}

