package merge

import (
    "fmt"
    "sync"

    "github.com/HoloArchivists/ytarchive-raw-go/log"
)

const (
    colorGreen   = "\033[32m"
    colorReset   = "\033[0m"
    colorYellow  = "\033[93m"
)

type mergeProgress struct {
    mu    sync.Mutex
    ended bool
    total int
    audio int
    video int
}

func newProgress() *mergeProgress {
    return &mergeProgress {
        total: -1,
    }
}

func (m *mergeProgress) initTotal(total int) {
    m.mu.Lock()
    defer m.mu.Unlock()
    if m.total < 0 {
        m.total = total
        m.updated()
    }
}

func (m *mergeProgress) done() {
    m.mu.Lock()
    defer m.mu.Unlock()

    m.ended = true
    m.updated()
}

func (m *mergeProgress) updated() {
    done := m.audio + m.video

    var pct float64
    if m.total == 0 {
        m.ended = true
    } else {
        pct = float64(done) / float64(m.total) * 50
    }

    var color string
    if m.ended {
        color = colorGreen
        pct = 100.0
    } else {
        color = colorYellow
    }

    title := fmt.Sprintf("%.1f%%", pct)
    msg := fmt.Sprintf("%s%.2f%% (%d audio, %d video)%s", color, pct, m.audio, m.video, colorReset)

    log.Progress(log.ProgressMerge, title, msg)
}

func (m *mergeProgress) mergedAudio() {
    m.mu.Lock()
    defer m.mu.Unlock()

    m.audio++
    m.updated()
}

func (m *mergeProgress) mergedVideo() {
    m.mu.Lock()
    defer m.mu.Unlock()

    m.video++
    m.updated()
}

