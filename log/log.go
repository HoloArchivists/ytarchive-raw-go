package log

import (
    "fmt"
    "os"
    "runtime"
    stdlog "log"
    "strings"
    "sync"
    "time"
)

type Level int
const (
    LevelDebug Level = iota
    LevelInfo
    LevelWarn
    LevelError
    LevelFatal //error + exit
)
const EndColor = "\033[0m"

type ProgressCategory int
const (
    ProgressAudioDownload ProgressCategory = iota
    ProgressVideoDownload
    ProgressMerge
)
var progressOrder = []ProgressCategory {
    ProgressAudioDownload,
    ProgressVideoDownload,
    ProgressMerge,
}
var progressNames = map[ProgressCategory]string {
    ProgressAudioDownload: "audio",
    ProgressVideoDownload: "video",
    ProgressMerge:         "merge",
}

type levelInfo struct {
    name  string
    color string
}
var levels = map[Level]levelInfo {
    LevelDebug: levelInfo { name: "debug", color: "\033[36m" },
    LevelInfo:  levelInfo { name: "info",  color: "\033[32m" },
    LevelWarn:  levelInfo { name: "warn",  color: "\033[93m" },
    LevelError: levelInfo { name: "error", color: "\033[91m" },
    LevelFatal: levelInfo { name: "fatal", color: "\033[91m" },
}
const eraseRestOfLine  = "\033[K"

func moveCursorUp(buf *[]byte, lines int) {
    *buf = append(*buf, "\033["...)
    itoa(buf, lines, -1)
    *buf = append(*buf, 'A')
}

func ParseLevel(name string) (Level, error) {
    name = strings.ToLower(name)
    for level, info := range levels {
        if name == info.name {
            return level, nil
        }
    }
    return LevelFatal, fmt.Errorf("Invalid log level '%s'", name)
}

type Logger struct {
    buf         []byte
    extraFrames int
    mu          sync.Mutex
    minLevel    Level
    tag         string
}

type progressStatus struct {
    title   string
    message string
}

var progress struct {
    mu          sync.Mutex
    buf         []byte
    titleBuf    []byte
    status      map[ProgressCategory]progressStatus
    windowName  string
    wroteStatus bool
}

var DefaultLogger *Logger

func init() {
    DefaultLogger = &Logger {
        extraFrames: 1,
    }
    progress.status = make(map[ProgressCategory]progressStatus)
    stdlog.SetFlags(stdlog.Ldate | stdlog.Lmicroseconds | stdlog.Lshortfile)
    stdlog.SetOutput(stdLogProxy {})
}

func doWrite(isProgress bool, data []byte) (int, error) {
    progress.mu.Lock()
    defer progress.mu.Unlock()

    progress.buf = progress.buf[:0]
    progress.titleBuf = progress.titleBuf[:0]

    if progress.wroteStatus {
        moveCursorUp(&progress.buf, len(progressOrder))
    }

    if len(data) > 0 {
        progress.buf = append(progress.buf, data...)
        progress.buf = append(progress.buf, eraseRestOfLine...)
        progress.buf = append(progress.buf, '\n')
    }

    for i, c := range progressOrder {
        if i > 0 {
            progress.titleBuf = append(progress.titleBuf, '/')
        }

        progress.buf = append(progress.buf, progressNames[c]...)
        progress.buf = append(progress.buf, ": "...)
        s, ok := progress.status[c]
        if !ok {
            progress.buf = append(progress.buf, "???"...)
            progress.titleBuf = append(progress.titleBuf, "???"...)
        } else {
            progress.buf = append(progress.buf, s.message...)
            progress.titleBuf = append(progress.titleBuf, s.title...)
        }
        progress.buf = append(progress.buf, eraseRestOfLine...)
        progress.buf = append(progress.buf, '\n')
    }
    progress.buf = append(progress.buf, "\033]0;"...)
    progress.buf = append(progress.buf, progress.titleBuf...)
    if progress.windowName != "" {
        progress.buf = append(progress.buf, ' ')
        progress.buf = append(progress.buf, progress.windowName...)
    }
    progress.buf = append(progress.buf, '\007')
    progress.wroteStatus = true

    os.Stderr.Write(progress.buf)

    return len(data), nil
}

type stdLogProxy struct {}

func (_ stdLogProxy) Write(p []byte) (int, error) {
    return doWrite(false, p)
}

func SetWindowName(name string) {
    progress.mu.Lock()
    defer progress.mu.Unlock()
    progress.windowName = name
}

func Progress(category ProgressCategory, title string, message string) {
    func() {
        progress.mu.Lock()
        defer progress.mu.Unlock()
        progress.status[category] = progressStatus {
            title:   title,
            message: message,
        }
    }()
    doWrite(true, nil)
}

func SetDefaultLevel(level Level) {
    DefaultLogger.minLevel = level
}

func New(tag string) *Logger {
    return &Logger {
        minLevel: DefaultLogger.minLevel,
        tag:      tag,
    }
}

func (l *Logger) SubLogger(tag string) *Logger {
    return New(fmt.Sprintf("%s.%s", l.tag, tag))
}

func (l *Logger) output(level Level, calldepth int, s string) {
    now := time.Now().UTC()
    var file string
    var line int

    if len(l.tag) == 0 {
        var ok bool
        _, file, line, ok = runtime.Caller(calldepth + l.extraFrames)
        if !ok {
            file = "???"
            line = 0
        }
    }
    l.mu.Lock()
    defer l.mu.Unlock()

    l.buf = l.buf[:0]

    info := levels[level]
    l.buf = append(l.buf, info.color...)
    formatTime(&l.buf, now)
    l.buf = append(l.buf, info.name...)
    l.buf = append(l.buf, ": "...)
    for i := len(info.name); i < 5; i++ {
        l.buf = append(l.buf, ' ')
    }

    formatHeader(&l.buf, l.tag, file, line)
    l.buf = append(l.buf, s...)
    if len(s) > 0 && s[len(s)-1] == '\n' {
        l.buf = l.buf[:len(l.buf) - 1]
    }
    l.buf = append(l.buf, EndColor...)
    doWrite(false, l.buf)
}

func (l *Logger) logf(level Level, format string, v ...interface{}) {
    if int(level) >= int(l.minLevel) {
        l.output(level, 3, fmt.Sprintf(format, v...))
    }
    if level == LevelFatal {
        os.Exit(1)
    }
}

func (l *Logger) log(level Level, v ...interface{}) {
    if int(level) >= int(l.minLevel) {
        l.output(level, 3, fmt.Sprint(v...))
    }
    if level == LevelFatal {
        os.Exit(1)
    }
}

func (l *Logger) Debug(v ...interface{}) {
    l.log(LevelDebug, v...)
}
func (l *Logger) Debugf(format string, v ...interface{}) {
    l.logf(LevelDebug, format, v...)
}

func (l *Logger) Info(v ...interface{}) {
    l.log(LevelInfo, v...)
}
func (l *Logger) Infof(format string, v ...interface{}) {
    l.logf(LevelInfo, format, v...)
}

func (l *Logger) Warn(v ...interface{}) {
    l.log(LevelWarn, v...)
}
func (l *Logger) Warnf(format string, v ...interface{}) {
    l.logf(LevelWarn, format, v...)
}

func (l *Logger) Error(v ...interface{}) {
    l.log(LevelError, v...)
}
func (l *Logger) Errorf(format string, v ...interface{}) {
    l.logf(LevelError, format, v...)
}

func (l *Logger) Fatal(v ...interface{}) {
    l.log(LevelFatal, v...)
}
func (l *Logger) Fatalf(format string, v ...interface{}) {
    l.logf(LevelFatal, format, v...)
}

func Debug(v ...interface{}) {
    DefaultLogger.Debug(v...)
}
func Debugf(format string, v ...interface{}) {
    DefaultLogger.Debugf(format, v...)
}

func Info(v ...interface{}) {
    DefaultLogger.Info(v...)
}
func Infof(format string, v ...interface{}) {
    DefaultLogger.Infof(format, v...)
}

func Warn(v ...interface{}) {
    DefaultLogger.Warn(v...)
}
func Warnf(format string, v ...interface{}) {
    DefaultLogger.Warnf(format, v...)
}

func Error(v ...interface{}) {
    DefaultLogger.Error(v...)
}
func Errorf(format string, v ...interface{}) {
    DefaultLogger.Errorf(format, v...)
}

func Fatal(v ...interface{}) {
    DefaultLogger.Fatal(v...)
}
func Fatalf(format string, v ...interface{}) {
    DefaultLogger.Fatalf(format, v...)
}

