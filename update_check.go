package main

import (
    "encoding/json"
    "fmt"
    "io/ioutil"
    "net/http"
    "runtime"
    "strconv"
    "strings"

    "github.com/notpeko/ytarchive-raw-go/log"
)

type githubReleaseAsset struct {
    DownloadUrl string `json:"browser_download_url"`
}

type githubReleaseData struct {
    TagName string               `json:"tag_name"`
    Assets  []githubReleaseAsset `json:"assets"`
}

func (d *githubReleaseData) updateUrl() (string, bool) {
    target := runtime.GOOS + "-" + runtime.GOARCH
    for _, v := range d.Assets {
        if strings.Contains(v.DownloadUrl, target) {
            return v.DownloadUrl, true
        }
    }
    return "", false
}

func parseVersion(version string) ([3]int, bool) {
    version = strings.TrimPrefix(version, "v")
    parts := strings.Split(version, ".")
    if len(parts) != 3 {
        return [3]int{}, false
    }
    var numbers [3]int
    for i, v := range parts {
        var err error
        numbers[i], err = strconv.Atoi(v)
        if err != nil {
            return [3]int{}, false
        }
    }
    return numbers, true
}

func versionCheck() (string, bool) {
    resp, err := http.Get("https://api.github.com/repos/notpeko/ytarchive-raw-go/releases/latest")
    if err != nil {
        log.Warnf("Unable to fetch latest release: %v", err)
        return "", false
    }
    defer resp.Body.Close()
    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        log.Warnf("Unable to fetch latest release: %v", err)
        return "", false
    }

    var result githubReleaseData
    if err := json.Unmarshal(body, &result); err != nil {
        log.Warnf("Unable to parse github release: %v", err)
        return "", false
    }

    latestVersion, ok := parseVersion(result.TagName)
    if !ok {
        log.Warnf("Unable to parse latest release version %s", result.TagName)
        return "", false
    }
    version := [3]int{VersionMajor, VersionMinor, VersionPatch}

    for i, v := range version {
        if latestVersion[i] > v {
            if url, ok := result.updateUrl(); ok {
                return fmt.Sprintf("%s (%s)", result.TagName, url), true
            }
            return result.TagName, true
        }
        if latestVersion[i] < v {
            return "", false
        }
    }
    return "", false
}

