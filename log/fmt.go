package log

import "time"

//code below is from https://cs.opensource.google/go/go/+/refs/tags/go1.17.2:src/log/log.go;l=103;drc=refs%2Ftags%2Fgo1.17.2

// Cheap integer to fixed-width decimal ASCII. Give a negative width to avoid zero-padding.
func itoa(buf *[]byte, i int, wid int) {
    // Assemble decimal in reverse order.
    var b [20]byte
    bp := len(b) - 1
    for i >= 10 || wid > 1 {
        wid--
        q := i / 10
        b[bp] = byte('0' + i - q*10)
        bp--
        i = q
    }
    // i < 10
    b[bp] = byte('0' + i)
    *buf = append(*buf, b[bp:]...)
}

func formatTime(buf *[]byte, t time.Time) {
    //Ldate
    year, month, day := t.Date()
    itoa(buf, year, 4)
    *buf = append(*buf, '/')
    itoa(buf, int(month), 2)
    *buf = append(*buf, '/')
    itoa(buf, day, 2)
    *buf = append(*buf, ' ')

    //Lmicroseconds
    hour, min, sec := t.Clock()
    itoa(buf, hour, 2)
    *buf = append(*buf, ':')
    itoa(buf, min, 2)
    *buf = append(*buf, ':')
    itoa(buf, sec, 2)
    *buf = append(*buf, '.')
    itoa(buf, t.Nanosecond()/1e3, 6)
    *buf = append(*buf, ' ')
}

func formatHeader(buf *[]byte, tag string, file string, line int) {
    if len(tag) == 0 {
        //Lshortfile
        for i := len(file) - 1; i > 0; i-- {
            if file[i] == '/' {
                file = file[i+1:]
                break
            }
        }
        *buf = append(*buf, file...)
        *buf = append(*buf, ':')
        itoa(buf, line, -1)
        *buf = append(*buf, ": "...)
    } else {
        *buf = append(*buf, tag...)
        *buf = append(*buf, ": "...)
    }
}
