package main

import (
    "encoding/json"
    "flag"
    "fmt"
    "io/ioutil"
    "os"
    "path/filepath"
    "strings"

    "github.com/notpeko/ytarchive-raw-go/download"
    "github.com/notpeko/ytarchive-raw-go/download/segments"
    "github.com/notpeko/ytarchive-raw-go/log"
    "github.com/notpeko/ytarchive-raw-go/util"
)

const (
	VersionMajor = 1
	VersionMinor = 1
	VersionPatch = 1
)

var Commit string

const DefaultOutputFormat = "%(upload_date)s %(title)s (%(id)s).mkv"

var (
    flagSet        *flag.FlagSet
    failThreshold  uint
    fregData       util.FregJson
    fsync          bool
    input          string
    keepFiles      bool
    logLevel       string
    merger         string
    mergerArgs     = make(map[string]map[string]string)
    output         string
    overwriteTemp  bool
    queue          string
    queueMode      segments.QueueMode
    retryThreshold uint
    tempDir        string
    threads        uint
    useQuic        bool
    verbose        bool
    versionPrint   bool
)

func printVersion() {
    fmt.Printf("ytarchive-raw-go %d.%d.%d commit %s\n", VersionMajor, VersionMinor, VersionPatch, Commit)
}

func printUsage() {
    self := filepath.Base(os.Args[0])
    printVersion()
    fmt.Printf(`
Usage: %[1]s [OPTIONS]

Options:
        -h, --help
                Show this help message.

        --connect-retries AMOUNT
                Amount of times to retry on connection failure.
                Default is 3

        --fsync
                If enabled, fsync is called after writing data to segment files.
                This forces the contents to be written to disk by the OS, which
                is usually not required but might help avoid issues with remote
                file systems.

        --input FILE
                Input JSON file. Required.

        -k, --keep-files
                Do not delete temporary files.

        --log-level LEVEL
                Log level to use (debug, info, warn, error, fatal).
                Default is 'info'

        --merger NAME
                Selects which merger should be used. Currently implemented
                mergers are 'tcp' and 'concat'.

                If empty, the tcp merger is used if ffmpeg supports tcp://
                inputs, otherwise the concat merger is used.

        --merger-argument NAME:KEY=VALUE
                Passes an argument to a merger. This option takes a single
                key-value pair, and can be used multiple times to pass
                multiple arguments. Only the first occurences of the : and =
                characters are treated specially, the value is allowed to
                include those characters.

                Supported arguments:
                    tcp merger:
                        bind_address=<ip address>

                See examples below for an example.

        -o, --output TEMPLATE
                Output file name EXCLUDING THE EXTENSION. Can include
                formatting similar to youtube-dl, with a subset of keys.
                See FORMAT OPTIONS below for a list of available keys.
                Default is '%[2]s'

        -O, --overwrite-temp
                Overwrite temporary files used for merging. If disabled,
                downloading stops if those files already exist and are not
                empty. If enabled, temporary files are deleted and recreated.

                This does not affect raw segment files, only merging files.

        -q, --queue-mode MODE
                Order to download segments (sequential, out-of-order).

                Sequential mode assigns the segments sequentially to the threads.

                Out of order mode splits the segments between threads, with each
                thread that finishes it's work helping the others until all segments
                are done.

                Default is 'sequential'
        
        --retries AMOUNT
                Amount of times to retry downloading segments on failure.
                Failure includes error responses from youtube and connection
                failures after 'connect-retries' fails.

                Default is 20

        --temp-dir PATH
                Temporary directory to store downloaded segments and other
                files used. Will be created if it doesn't exist. If not specified,
                a random temporary directory will be created.

        -t, --threads THREAD_COUNT
                Number of threads to use for downloads. The number of used
                threads will be THREAD_COUNT for audio and THREAD_COUNT for video.

                A high number of threads has a chance to fail the download with 401
                errors. Restarting the download with a smaller number should fix it.

                Default is 1

        --use-quic=QUIC
                Whether or not HTTP/3 should be used. Only disable this if some
                middle box (firewall, etc) is interfering with HTTP/3 downloads.

                Default is 'true'

        -v, --verbose
                Sets log level to 'debug' if present. Overrides the 'log-level' flag.

        -V, --version
                Print the version and exit.

Examples:
        %[1]s -i dQw4w9WgXcQ.urls.json
        %[1]s --threads 12 -i WTf8-KT6fWA.urls.json
        %[1]s --output '[%%(upload_date)s] %%(title)s [%%(channel)s] (%%(id)s)' -i 5gDw5AWN-Kk.urls.json
        %[1]s --use-quic=false -i efFGPtC-NZU.urls.json
        %[1]s --merger-argument tcp:bind_address=127.69.4.20 -i fvO2NFDIEgk.urls.json

Resuming downloads:
        Downloads can be resumed (and reuse already downloaded segments) as long as:
            - Temporary files are kept (--keep-files)
            - The same temporary directory is used (either specify the same --temp-dir value
              for both runs or use the auto created directory on the second run)

FORMAT TEMPLATE OPTIONS
        Format template keys provided are made to be the same as they would be for
        youtube-dl. See https://github.com/ytdl-org/youtube-dl#output-template

        For file names, each template substitution is sanitized by replacing invalid file name
        characters with underscore (_).

        description (string): Video description
        id (string): Video identifier
        title (string): Video title
        url (string): Video URL
        channel (string): Full name of the channel the livestream is on
        channel_id (string): ID of the channel
        channel_url (string): URL of the channel
        publish_date (string: YYYYMMDD): Stream publish date, UTC timezone
        start_date (string: YYYYMMDD): Stream start date, UTC timezone
        upload_date (string: YYYYMMDD): Stream start date, UTC timezone
        start_timestamp (string: RFC3339 timestamp): Stream start date

        The description, url and channel_url fields are substitured by nothing for file names.
`, self, DefaultOutputFormat)
}

func init() {
    flagSet = flag.NewFlagSet("flags", flag.ExitOnError)
    flagSet.Usage = printUsage

    flagSet.StringVar(&input, "i",     "", "Input JSON file.")
    flagSet.StringVar(&input, "input", "", "Input JSON file.")

    flagSet.StringVar(&output, "o",      DefaultOutputFormat, "Output file path.")
    flagSet.StringVar(&output, "output", DefaultOutputFormat, "Output file path.")

    flagSet.UintVar(&threads, "t",       1, "Multi-threaded download.")
    flagSet.UintVar(&threads, "threads", 1, "Multi-threaded download.")

    flagSet.UintVar(&retryThreshold, "connect-retries", download.DefaultRetryThreshold, "Amount of times to retry a request on connection failure.")

    flagSet.UintVar(&failThreshold, "retries", download.DefaultFailThreshold, "Amount of times to retry downloading segments on failure.")

    flagSet.BoolVar(&keepFiles, "k",          false, "Do not delete temporary files.")
    flagSet.BoolVar(&keepFiles, "keep-files", false, "Do not delete temporary files.")

    flagSet.StringVar(&tempDir, "temp-dir", "", "Directory to store temporary files. A randomly-named one will be created if empty.")

    flagSet.BoolVar(&useQuic, "use-quic", true, "Whether or not HTTP/3 should be used.")

    flagSet.StringVar(&queue, "q",          "sequential", "Order to download segments (sequential, out-of-order).")
    flagSet.StringVar(&queue, "queue-mode", "sequential", "Order to download segments (sequential, out-of-order).")

    flagSet.BoolVar(&verbose, "v",       false, "Enable debug logging. Overrides log-level.")
    flagSet.BoolVar(&verbose, "verbose", false, "Enable debug logging. Overrides log-level.")

    flagSet.BoolVar(&overwriteTemp, "O",              false, "Overwrite temporary merged files.")
    flagSet.BoolVar(&overwriteTemp, "overwrite-temp", false, "Overwrite temporary merged files.")

    flagSet.BoolVar(&fsync, "fsync", false, "Force flushing of OS buffers after writing segment files.")

    flagSet.BoolVar(&versionPrint, "V",       false, "Print version and exit")
    flagSet.BoolVar(&versionPrint, "version", false, "Print version and exit")

    flagSet.StringVar(&logLevel, "log-level", "info", "Log level to use (debug, info, warn, error, fatal).")

    flagSet.StringVar(&merger, "merger", "", "Which merger to use.")

    flagSet.Func("merger-argument", "Pass an argument to a merger.", func(s string) error {
        parts := strings.SplitN(s, ":", 2)
        if len(parts) < 2 {
            return fmt.Errorf("Invalid merger argument '%s', format is NAME:KEY=VALUE", s)
        }
        kv := strings.SplitN(parts[1], "=", 2)
        if len(kv) < 2 {
            return fmt.Errorf("Invalid merger argument '%s', format is NAME:KEY=VALUE", s)
        }
        name := strings.ToLower(parts[0])
        key := strings.ToLower(kv[0])
        value := kv[1]

        m, ok := mergerArgs[name]
        if !ok {
            m = make(map[string]string)
            mergerArgs[name] = m
        }

        m[key] = value

        return nil
    })
}

func parseArgs() {
    flagSet.Parse(os.Args[1:])

    if versionPrint {
        printVersion()
        os.Exit(1)
    }

    if verbose {
        logLevel = "debug"
    }

    level, err := log.ParseLevel(logLevel)
    if err != nil {
        fmt.Printf("%v", err)
        os.Exit(1)
    }
    log.SetDefaultLevel(level)

    switch strings.ToLower(queue) {
    case "sequential":
        queueMode = segments.QueueSequential
    case "out-of-order":
        queueMode = segments.QueueOutOfOrder
    default:
        log.Fatalf("Invalid queue mode '%s'", queue)
    }

    if input == "" {
        log.Fatalf("No input file specified")
    }
    inputData, err := ioutil.ReadFile(input)
    if err != nil {
        log.Fatalf("Unable to read file '%s': %v", input, err)
    }

    if err = json.Unmarshal(inputData, &fregData); err != nil {
        log.Fatalf("Unable to parse freg json: %v", err)
    }

    output, err = fregData.FormatTemplate(output, true)
    if err != nil {
        log.Fatalf("Invalid output template: %v", err)
    }
    output = output + ".mkv"

    log.Infof("Saving output to %s", output)
}

