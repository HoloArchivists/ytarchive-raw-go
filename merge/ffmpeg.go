package merge

import (
    "bufio"
    "bytes"
    "encoding/base64"
    "fmt"
    "os"
    "os/exec"
    "path"
    "strings"

    "github.com/notpeko/ytarchive-raw-go/log"
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
    cmd := ffmpeg(nil, "-h")
    cmd.Stdin = nil
    cmd.Stdout = nil
    cmd.Stderr = nil
    return cmd.Run()
}

func hasProtocol(name string) bool {
    cmd := ffmpeg(nil, "--help", "protocol=" + name)
    cmd.Stdin = nil
    output, err := cmd.Output()
    if err != nil {
        log.Fatalf("Unable to find ffmpeg: %v", err)
    }
    return !bytes.Contains(output, []byte("Unknown protocol "))
}

func writeThumbnail(options *MuxerOptions) (string, error) {
    b64 := options.FregData.Metadata.Thumbnail
    if idx := strings.IndexByte(b64, ','); idx >= 0 {
        b64 = b64[idx + 1:]
    }

    dec, err := base64.StdEncoding.DecodeString(b64)
    if err != nil {
        return "", err
    }

    thumbnail := path.Join(options.TempDir, fmt.Sprintf("thumbnail-%s.jpg", options.FregData.Metadata.Id))
    f, err := os.OpenFile(thumbnail, os.O_RDWR|os.O_CREATE, 0644)
    if err != nil {
        return "", err
    }
    defer f.Close()

    if _, err := f.Write(dec); err != nil {
        return "", err
    }

    if err = f.Sync(); err != nil {
        return "", err
    }

    return thumbnail, nil
}

func muxFfmpeg(options *MuxerOptions, audio, video string) error {
    args := make([]string, 0)
    args = append(
        args,
        "-y",
        "-i",
        audio,
        "-i",
        video,
        "-c",
        "copy",
    )

    thumbnail, err := writeThumbnail(options)
    if err != nil {
        return fmt.Errorf("Unable to write thumbnail file: %v", err)
    }
    args = append(
        args,
        "-metadata",
        "title=" + options.FregData.Metadata.Title,
        "-metadata",
        "comment=" + options.FregData.Metadata.Description,
        "-metadata",
        "author=" + options.FregData.Metadata.ChannelName,
        "-metadata",
        "episode_id=" + options.FregData.Metadata.Id,
        "-attach",
        thumbnail,
        "-metadata:s:t",
        "mimetype=image/jpeg",
        "-metadata:s:t",
        "filename=thumbnail.jpg",
    )
    args = append(args, options.FinalFile)

    cmd := ffmpeg(options.Logger, args...)
    cmd.Env = append(
        os.Environ(),
        fmt.Sprintf("file='%s':level=32", path.Join(options.TempDir, fmt.Sprintf("ffmpeg-%s.out", options.FregData.Metadata.Id))),
    )
    cmd.Stdin = nil

    var stderr bytes.Buffer
    cmd.Stdout = nil
    cmd.Stderr = &stderr

    if err = cmd.Run(); err != nil {
        return err
    }

    warnings := make([]string, 0)
    reader := bufio.NewReader(&stderr)

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

    if len(warnings) > 0 {
        options.Logger.Warn("FFmpeg succeeded with warnings")
        for _, v := range warnings {
            options.Logger.Warn(v)
        }
    }

    return nil
}

var ignoredWarnings = []string {
    "    Last message repeated ",
    "Found duplicated MOOV Atom. Skipped it",
    "Found unknown-length element with ID 0x18538067 at pos.",
}

func ignoreWarning(line string) bool {
    for _, v := range ignoredWarnings {
        if strings.Contains(line, v) {
            return true
        }
    }
    return false
}

