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

func ParseLevel(name string) (Level, error) {
    name = strings.ToLower(name)
    for level, info := range levels {
        if name == info.name {
            return level, nil
        }
    }
    return LevelFatal, fmt.Errorf("Invalid log level '%s'", name)
}

func (l Level) Enabled() bool {
    return int(l) >= int(minLevel)
}

var buf []byte
var mu sync.Mutex
var minLevel Level = LevelInfo

func init() {
    stdlog.SetFlags(stdlog.Ldate | stdlog.Lmicroseconds | stdlog.Lshortfile)
}

func Init(level Level) {
    minLevel = level
}

func output(level Level, calldepth int, s string) {
    now := time.Now()
    _, file, line, ok := runtime.Caller(calldepth)
    if !ok {
        file = "???"
        line = 0
    }
    mu.Lock()
    defer mu.Unlock()

    buf = buf[:0]

    info := levels[level]
    buf = append(buf, info.color...)
    buf = append(buf, info.name...)
    buf = append(buf, ": "...)
    for i := len(info.name); i < 5; i++ {
        buf = append(buf, ' ')
    }

    formatHeader(&buf, now, file, line)
    buf = append(buf, s...)
    if len(s) == 0 || s[len(s)-1] != '\n' {
        buf = append(buf, '\n')
    }
    buf = append(buf, EndColor...)
    os.Stderr.Write(buf)
}

func logf(level Level, format string, v ...interface{}) {
    if level.Enabled() {
        output(level, 3, fmt.Sprintf(format, v...))
    }
    if level == LevelFatal {
        os.Exit(1)
    }
}

func log(level Level, v ...interface{}) {
    if level.Enabled() {
        output(level, 3, fmt.Sprint(v...))
    }
    if level == LevelFatal {
        os.Exit(1)
    }
}

func Debug(v ...interface{}) {
    log(LevelDebug, v...)
}
func Debugf(format string, v ...interface{}) {
    logf(LevelDebug, format,v...)
}

func Info(v ...interface{}) {
    log(LevelInfo, v...)
}
func Infof(format string, v ...interface{}) {
    logf(LevelInfo, format,v...)
}

func Warn(v ...interface{}) {
    log(LevelWarn, v...)
}
func Warnf(format string, v ...interface{}) {
    logf(LevelWarn, format,v...)
}

func Error(v ...interface{}) {
    log(LevelError, v...)
}
func Errorf(format string, v ...interface{}) {
    logf(LevelError, format,v...)
}

func Fatal(v ...interface{}) {
    log(LevelFatal, v...)
}
func Fatalf(format string, v ...interface{}) {
    logf(LevelFatal, format,v...)
}

