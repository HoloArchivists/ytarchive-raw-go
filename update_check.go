package main

import (
    "encoding/json"
    "io/ioutil"
    "net/http"
    "strconv"
    "strings"

    "github.com/notpeko/ytarchive-raw-go/log"
)

type githubReleaseData struct {
    TagName string `json:"tag_name"`
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
            return result.TagName, true
        }
        if latestVersion[i] < v {
            return "", false
        }
    }
    return "", false
}

