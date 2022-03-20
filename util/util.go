package util

import (
    "encoding/base64"
    "fmt"
    "os"
    "regexp"
    "strings"
    "sync"
    "time"

    "github.com/notpeko/ytarchive-raw-go/log"
)

func FileNotEmpty(path string) bool {
    if info, err := os.Stat(path); err == nil && info.Size() > 0 {
        return true
    }
    return false
}

var bestVideoFormats = []int{
    337, 315, 266, 138, // 2160p60
    313, 336, // 2160p
    308, // 1440p60
    271, 264, // 1440p
    335, 303, 299, // 1080p60
    248, 169, 137, // 1080p
    334, 302, 298, // 720p60
    247, 136, // 720p
}
var videoFormatNames = map[int]string {
    337: "2160p60 VP9 HDR",
    315: "2160p60 VP9",
    266: "2160p60 H264",
    138: "2160p60 H264", // duplicate?
    313: "2160p VP9",
    336: "1440p60 VP9 HDR",
    308: "1440p60 VP9",
    271: "1440p VP9",
    264: "1440p H264",
    335: "1080p60 VP9 HDR",
    303: "1080p60 VP9",
    299: "1080p60 H264",
    248: "1080p VP9",
    169: "1080p VP8",
    137: "1080p H264",
    334: "720p60 VP9 HDR",
    302: "720p60 VP9",
    298: "720p60 H264",
    247: "720p VP9",
    136: "720p H264",
}

var bestAudioFormats = []int{
    251, 141, 171, 140, 250, 249, 139,
}
var audioFormatNames = map[int]string {
    251: "Opus 160 Kbps",
    141: "AAC 256 Kbps",
    171: "Opus 128 Kbps",
    140: "AAC 128 Kbps",
    250: "Opus 70 Kbps",
    249: "Opus 50 Kbps",
    139: "AAC 48 Kbps",
}

type FregMetadata struct {
    Title          string    `json:"title"`
    Id             string    `json:"id"`
    ChannelName    string    `json:"channelName"`
    ChannelURL     string    `json:"channelURL"`
    Description    string    `json:"description"`
    Thumbnail      string    `json:"thumbnail"`
    ThumbnailURL   string    `json:"thumbnailUrl"`
    StartTimestamp time.Time `json:"startTimestamp"`
}

type FregJson struct {
    Video      map[int]string    `json:"video"`
    Audio      map[int]string    `json:"audio"`
    Metadata   FregMetadata      `json:"metadata"`
    Version    string            `json:"version"`
    CreateTime time.Time         `json:"createTime"`
    formatVals map[string]string
    formatLock sync.Mutex
}

func pickBestID(urls map[int]string, order []int, guess bool) int {
    for _, v := range order {
        _, ok := urls[v]
        if ok {
            return v
        }
    }
    //no format is known, pick whatever is highest to maybe get the best quality
    if guess {
        log.Warnf("Unable to find best format, choosing highest itag value as a guess for best codec")
        var max int = -100000
        for k, _ := range urls {
            if k > max {
                max = k
            }
        }
        if _, ok := urls[max]; ok {
            return max
        }
        log.Fatalf("Unable to find a suitable codec (tried %v, picking highest)", order)
    } else {
        log.Fatalf("Unable to find a suitable codec (tried %v)", order)
    }
    return -1
}

func pickBest(urls map[int]string, preferredFormats []int, order []int, names map[int]string, which string) string {
    guess := true
    if preferredFormats != nil {
        order = preferredFormats
        guess = false
    }

    id := pickBestID(urls, order, guess)
    name, ok := names[id]
    if !ok {
        name = "unknown codec"
    }
    log.Infof("Using format %d (%s) for %s", id, name, which)
    return urls[id]
}

func (f *FregJson) BestVideo(preferredFormats []int) string {
    return pickBest(f.Video, preferredFormats, bestVideoFormats, videoFormatNames, "video")
}

func (f *FregJson) BestAudio(preferredFormats []int) string {
    return pickBest(f.Audio, preferredFormats, bestAudioFormats, audioFormatNames, "audio")
}

func (f *FregJson) fillFormatVals() {
    f.formatLock.Lock()
    defer f.formatLock.Unlock()
    if len(f.formatVals) > 0 {
        return
    }
    vals := make(map[string]string)
    vals["id"] = f.Metadata.Id
    vals["url"] = fmt.Sprintf("https://youtu.be/%s", f.Metadata.Id)
    vals["title"] = f.Metadata.Title
    vals["channel"] = f.Metadata.ChannelName
    vals["upload_date"] = f.Metadata.StartTimestamp.Format("20060102")
    vals["start_date"] = vals["upload_date"]
    vals["publish_date"] = vals["upload_date"]
    vals["start_timestamp"] = f.Metadata.StartTimestamp.Format(time.RFC3339)
    vals["description"] = f.Metadata.Description

    channelUrlRegex := regexp.MustCompile(`^https?://(?:www\.)youtube.com/channel/([a-zA-Z0-9\-_]+)$`)
    channelIdMatch := channelUrlRegex.FindStringSubmatch(f.Metadata.ChannelURL)
    if len(channelIdMatch) < 2 {
        log.Fatalf("Unable to parse channel url '%s'", f.Metadata.ChannelURL)
    }
    vals["channel_url"] = f.Metadata.ChannelURL
    vals["channel_id"] = channelIdMatch[1]

    f.formatVals = vals
}

func (f *FregJson) FormatTemplate(template string, filename bool) (string, error) {
    f.fillFormatVals()
    pythonMapKey := regexp.MustCompile(`%\((\w+)\)s`)
    for {
        match := pythonMapKey.FindStringSubmatch(template)
        if match == nil {
            return template, nil
        }

        key := strings.ToLower(match[1])
        if _, ok := f.formatVals[key]; !ok {
            return "", fmt.Errorf("Unknown format key '%s'", key)
        }
        val := f.formatVals[key]

        if filename && (key == "description" || key == "url" || key == "channel_url") {
            val = ""
        }

        if filename {
            val = sanitizeFilename(val)
        }

        template = strings.ReplaceAll(template, match[0], val)
    }
}

func (f *FregJson) WriteThumbnail(path string) error {
    b64 := f.Metadata.Thumbnail
    if idx := strings.IndexByte(b64, ','); idx >= 0 {
        b64 = b64[idx + 1:]
    }

    dec, err := base64.StdEncoding.DecodeString(b64)
    if err != nil {
        return err
    }

    file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
    if err != nil {
        return err
    }
    defer file.Close()

    if _, err := file.Write(dec); err != nil {
        return err
    }

    if err = file.Sync(); err != nil {
        return err
    }

    return nil
}

var fnameReplacer = strings.NewReplacer(
	"<",  "_",
	">",  "_",
	":",  "_",
	`"`,  "_",
	"/",  "_",
	"\\", "_",
	"|",  "_",
	"?",  "_",
	"*",  "_",
)

func sanitizeFilename(s string) string {
    return fnameReplacer.Replace(s)
}

