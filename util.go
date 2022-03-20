package main

import (
    "fmt"
    "regexp"
    "strings"
    "sync"
    "time"

    "github.com/notpeko/ytarchive-raw-go/log"
)

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
var bestAudioFormats = []int{
    251, 141, 171, 140, 250, 249, 139,
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

func pickBest(urls map[int]string, order []int) string {
    for _, v := range order {
        url, ok := urls[v]
        if ok {
            return url
        }
    }
    for _, v := range urls {
        return v
    }
    panic("No URLs found")
}

func (f *FregJson) BestVideo() string {
    return pickBest(f.Video, bestVideoFormats)
}

func (f *FregJson) BestAudio() string {
    return pickBest(f.Audio, bestAudioFormats)
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

