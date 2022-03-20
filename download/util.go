package download

import (
    "strings"
    "sync"
)

const ProgressBarLength = 80
const ProgressBarFullSymbol = "█"
const ProgressBarEmptySymbol = "-"
const ProgressBarPrintInterval = 5

type progressBar struct {
    finished      int
    mu            sync.Mutex
    printFn       func(string, int, int)
    progress      []bool
    progressIndex map[int]int
    total         int
}

func makeProgressBar(total int, printFn func(string, int, int)) *progressBar {
    bar := &progressBar {
        printFn:       printFn,
        progress:      make([]bool, ProgressBarLength),
        progressIndex: make(map[int]int),
        total:         total,
    }

    for i := 0; i < ProgressBarLength; i++ {
        x := int(float64(total) / float64(ProgressBarLength)) * (i + 1)
        bar.progress[i] = false
        bar.progressIndex[x] = i
    }

    return bar
}

func (p *progressBar) done(index int) {
    p.mu.Lock()
    defer p.mu.Unlock()

    idx, ok := p.progressIndex[index]
    if ok {
        p.progress[idx] = true
    }
    p.finished++
    if p.finished == p.total || (p.finished % ProgressBarPrintInterval == 0) {
        p.printProgress()
    }
}

func (p *progressBar) printProgress() {
    var b strings.Builder
    b.Grow(ProgressBarLength)
    for _, v := range p.progress {
        if v {
            b.WriteString(ProgressBarFullSymbol)
        } else {
            b.WriteString(ProgressBarEmptySymbol)
        }
    }
    p.printFn(b.String(), p.finished, p.total)
}

