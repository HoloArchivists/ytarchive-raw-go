package download

import (
    "fmt"
    "math"
    "sync"
    "time"

    "github.com/notpeko/ytarchive-raw-go/log"
)

const (
    colorGreen  = "\033[32m"
    colorRed    = "\033[91m"
    colorReset  = "\033[0m"
    colorYellow = "\033[93m"
)

type Progress struct {
    parent     *TotalProgress
    cached     int
    downloaded int
    total      int
    start      time.Time
    end        time.Time
    expire     *time.Time
}

func (p *Progress) init(totalSegments int, expire *time.Time) {
    p.parent.mu.Lock()
    defer p.parent.mu.Unlock()

    p.total = totalSegments
    p.start = time.Now()
    p.expire = expire
}

func (p *Progress) done(cached bool) {
    p.parent.mu.Lock()
    defer p.parent.mu.Unlock()

    if cached {
        p.cached++
    } else {
        p.downloaded++
    }

    if p.cached + p.downloaded == p.total {
        p.end = time.Now()
    }

    //we hold the lock, safe to call
    p.parent.printProgress()
}

//NOT thread safe, should NOT acquire locks
func (p *Progress) fmt() string {
    if p.total == -1 {
        return fmt.Sprintf("%s0%% (0/???, not started yet)%s", colorYellow, colorReset)
    }

    finished := p.cached + p.downloaded
    if finished == p.total {
        return fmt.Sprintf("%s100%% (%d/%d in %v)%s", colorGreen, finished, p.total, p.end.Sub(p.start).Round(time.Second), colorReset)
    }

    progress := float64(finished) / float64(p.total)

    //don't include eta without downloading a bit
    if p.downloaded > 100 {
        elapsed := time.Since(p.start)

        etaProgress := float64(p.downloaded) / float64(p.total - p.cached)
        etaSeconds := (1.0 / etaProgress) * elapsed.Seconds()
        eta := (time.Duration(int64(etaSeconds)) * time.Second) - elapsed

        color := colorYellow
        if p.expire != nil && p.start.Add(eta).After(*p.expire) {
            color = colorRed
        }
        return fmt.Sprintf("%s%.2f%% (%d/%d, eta %s)%s", color, progress * 100, finished, p.total, formatDuration(eta), colorReset)
    } else {
        return fmt.Sprintf("%s%.2f%% (%d/%d, eta unknown)%s", colorYellow, progress * 100, finished, p.total, colorReset)
    }

}

type TotalProgress struct {
    mu      sync.Mutex
    audio   *Progress
    video   *Progress
}

func NewProgress() *TotalProgress {
    p := &TotalProgress {}
    p.audio = &Progress {
        parent: p,
        total:  -1,
    }
    p.video = &Progress {
        parent: p,
        total:  -1,
    }
    return p
}

func (p *TotalProgress) Audio() *Progress {
    return p.audio
}

func (p *TotalProgress) Video() *Progress {
    return p.video
}

//NOT thread safe, should NOT acquire locks
func (p *TotalProgress) printProgress() {
    log.Progress(fmt.Sprintf("Audio: %s, Video: %s", p.audio.fmt(), p.video.fmt()))
}

func formatDuration(d time.Duration) string {
    d = time.Duration(int64(significantFigures(d.Seconds(), 3))) * time.Second

    h := d / time.Hour
    d -= h * time.Hour
    m := d / time.Minute
    d -= m * time.Minute
    s := d / time.Second

    if h > 0 {
        return fmt.Sprintf("%dh%02dm%02ds", h, m, s)
    } else {
        return fmt.Sprintf("%02dm%02ds", m, s)
    }
}

func significantFigures(v float64, n int) float64 {
    exp := math.Pow(10, math.Floor(math.Log10(math.Abs(v))) - float64(n - 1))
    return exp * math.Round(v / exp)
}

