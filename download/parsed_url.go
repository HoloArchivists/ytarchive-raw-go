package download

import (
    "fmt"
    "net/url"
    "strconv"
    "strings"
    "time"
)

type urlType int
const (
    urlTypeInvalid urlType = iota
    urlTypeQuery
    urlTypePath
)

type parsedURL struct {
    raw     string
    expire  *time.Time
    id      string
    itag    int
    typ     urlType
}

func parseDownloadURL(rawUrl string) (*parsedURL, error) {
    parsed, err := url.Parse(rawUrl)
    if err != nil {
        return nil, err
    }

    query, err := url.ParseQuery(parsed.RawQuery)
    if err != nil {
        return nil, err
    }

    p := &parsedURL {
        raw: rawUrl,
        typ: urlTypeInvalid,
    }
    var findField func(string) string
    if query.Get("noclen") != "" {
        p.typ = urlTypeQuery
        findField = query.Get
    } else if strings.HasPrefix(parsed.EscapedPath(), "/videoplayback/") {
        if strings.HasSuffix(p.raw, "/") {
            p.raw = p.raw[:len(p.raw) - 1]
        }

        p.typ = urlTypePath
        fields := strings.FieldsFunc(parsed.EscapedPath(), func(c rune) bool {
            return c == '/'
        })
        fields = fields[1:]
        findField = func(name string) string {
            for i := 0; i < len(fields); i += 2 {
                if fields[i] == name {
                    return fields[i + 1]
                }
            }
            return ""
        }
    }

    id := findField("id")
    if id == "" {
        return nil, fmt.Errorf("URL missing 'id' parameter")
    }
    if idx := strings.IndexByte(id, '~'); idx > 0 {
        id = id[:idx]
    }
    p.id = id

    itagString := findField("itag")
    if itagString == "" {
        return nil, fmt.Errorf("URL misssing 'itag' parameter")
    }
    itag, err := strconv.Atoi(itagString)
    if err != nil {
        return nil, fmt.Errorf("Unable to parse itag value '%s' into an int", itagString)
    }
    p.itag = itag

    expireString := findField("expire")
    if expireString != "" {
        expire, err := strconv.ParseInt(expireString, 10, 64)
        if err == nil {
            t := time.Unix(expire, 0)
            p.expire = &t
        }
    }


    if p.typ == urlTypeInvalid {
        return nil, fmt.Errorf("Unknown URL type for '%s'", rawUrl)
    }
    return p, nil
}

func (p *parsedURL) SegmentURL(seg int) string {
    switch(p.typ) {
    case urlTypeQuery:
        url, err := url.Parse(p.raw)
        if err != nil {
            panic(fmt.Sprintf("unreachable, %v", err))
        }

        q := url.Query()
        q.Set("sq", fmt.Sprintf("%d", seg))
        url.RawQuery = q.Encode()

        return url.String()
    case urlTypePath:
        return fmt.Sprintf("%s/%d", p.raw, seg)
    default:
        panic("unreachable")
    }
}

