package main

import (
    "encoding/json"
    "flag"
    "fmt"
    "io/ioutil"
    "os"
    "path/filepath"
    "strings"
    "time"

    "github.com/notpeko/ytarchive-raw-go/download"
    "github.com/notpeko/ytarchive-raw-go/download/segments"
    "github.com/notpeko/ytarchive-raw-go/log"
    "github.com/notpeko/ytarchive-raw-go/util"
)

const (
	VersionMajor = 1
	VersionMinor = 5
	VersionPatch = 1
)

var Commit string

const DefaultOutputFormat = "%(upload_date)s %(title)s (%(id)s)"

var (
    disableResume  bool
    flagSet        *flag.FlagSet
    failThreshold  uint
    forceIPv4      bool
    forceIPv6      bool
    fregData       util.FregJson
    fsync          bool
    input          string
    ipPoolFile     string
    keepFiles      bool
    logLevel       string
    mergeOnlyFile  string
    merger         string
    mergerArgs     = make(map[string]map[string]string)
    network        = util.NetworkAny
    output         string
    overwriteTemp  bool
    queue          string
    queueMode      segments.QueueMode
    requeueDelay   time.Duration
    requeueFailed  uint
    requeueLast    bool
    retryThreshold uint
    segmentCount   uint
    tempDir        string
    threads        uint
    useQuic        bool
    verbose        bool
    versionPrint   bool
    windowName     string
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

        -4, --ipv4
            Force use of IPv4.

        -6, --ipv6
            Force use of IPv6.

        --connect-retries AMOUNT
                Amount of times to retry on connection failure.
                Default is 3

        --disable-resume
                Disables resume support. Fragment files will be deleted as
                soon as they have been merged, instead of being deleted only
                after the download is done.
                If both this option and 'keep-files' are passed, segments won't
                be deleted at all.

        --fsync
                If enabled, fsync is called after writing data to segment files.
                This forces the contents to be written to disk by the OS, which
                is usually not required but might help avoid issues with remote
                file systems.

        --input FILE
                Input JSON file. Required.

        --ip-pool FILE
                File containing IP addresses to use for downloading. Each
                line should be either empty or contain an IP address.
                The file may contain both ipv4 and ipv6 addresses.

                If present, --ipv4 and --ipv6 are ignored.

        -k, --keep-files
                Do not delete temporary files.

        --log-level LEVEL
                Log level to use (debug, info, warn, error, fatal).
                Default is 'info'

        --merge DOWNLOAD_INFO_JSON
                Merges a download created with the download-only merger
                (see below) into a video file.

                Most merger related options (such as --merger, -k, -o, --temp-dir)
                still apply.

        --merger NAME
                Selects which merger should be used. Currently implemented
                mergers are 'tcp', 'concat' and 'download-only'.

                If empty, the tcp merger is used if ffmpeg supports tcp://
                inputs, otherwise the concat merger is used.

                The download-only merger doesn't generate a video file. It
                only writes a file with the downloaded video and audio segments.

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

                Default is 'out-of-order'

        --requeue-delay DELAY
                Minimum amount of time to wait before redownloading a segment
                once it's been requeued. Valid delay units are s, m, h.

                Default is 2m.

        --requeue-failed AMOUNT
                How many times should failed segments be requeued for download.
                Requeueing happens *after* a segment has failed 'retries' number
                of times, so that instead of giving it up it gets queued to be
                re-tried later.

                Default is 1.

        --requeue-last
                If used, the last segment is allowed to be requeued, otherwise
                it'll be failed instantly.
        
        --retries AMOUNT
                Amount of times to retry downloading segments on failure.
                Failure includes error responses from youtube and connection
                failures after 'connect-retries' fails.

                Default is 20

        --segment-count COUNT
                Sets how many segments should be downloaded. This is intended
                for testing or as a last effort for merging already downloaded
                segments once the download URLs expire. If 0, the amount of segments
                is fetched from youtube instead.

                Default is 0

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

        --window-name NAME
                Use NAME to identify the window. If empty, only the progress
                is shown in the window title, otherwise the name and progress
                are shown.

                Default is ''.

Examples:
        %[1]s -i dQw4w9WgXcQ.urls.json
        %[1]s --threads 12 -i WTf8-KT6fWA.urls.json
        %[1]s --output '[%%(upload_date)s] %%(title)s [%%(channel)s] (%%(id)s)' -i 5gDw5AWN-Kk.urls.json
        %[1]s --use-quic=false -i efFGPtC-NZU.urls.json
        %[1]s --merger-argument tcp:bind_address=127.69.4.20 -i fvO2NFDIEgk.urls.json

Using the download-only merger:
        %[1]s --merger download-only -i Rr05mghnRMY.urls.json --output "%%(id)s"
        %[1]s --merge Rr05mghnRMY.json --output "[%%(upload_date)s] %%(title)s [%%(channel)s] (%%(id)s)"

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

    flagSet.BoolVar(&forceIPv4, "4", false, "Force use of IPv4.")
    flagSet.BoolVar(&forceIPv4, "ipv4", false, "Force use of IPv4.")

    flagSet.BoolVar(&forceIPv6, "6", false, "Force use of IPv6.")
    flagSet.BoolVar(&forceIPv6, "ipv6", false, "Force use of IPv6.")

    flagSet.UintVar(&retryThreshold, "connect-retries", download.DefaultRetryThreshold, "Amount of times to retry a request on connection failure.")

    flagSet.BoolVar(&disableResume, "disable-resume", false, "Disable resume support.")

    flagSet.BoolVar(&fsync, "fsync", false, "Force flushing of OS buffers after writing segment files.")

    flagSet.StringVar(&input, "i",     "", "Input JSON file.")
    flagSet.StringVar(&input, "input", "", "Input JSON file.")

    flagSet.StringVar(&ipPoolFile, "ip-pool", "", "IP addresses to use.")

    flagSet.BoolVar(&keepFiles, "k",          false, "Do not delete temporary files.")
    flagSet.BoolVar(&keepFiles, "keep-files", false, "Do not delete temporary files.")

    flagSet.StringVar(&logLevel, "log-level", "info", "Log level to use (debug, info, warn, error, fatal).")

    flagSet.StringVar(&mergeOnlyFile, "merge", "", "Merges a file generated by the download-only merger.")

    flagSet.StringVar(&merger, "merger", "", "Which merger to use.")

    flagSet.StringVar(&output, "o",      DefaultOutputFormat, "Output file path.")
    flagSet.StringVar(&output, "output", DefaultOutputFormat, "Output file path.")

    flagSet.BoolVar(&overwriteTemp, "O",              false, "Overwrite temporary merged files.")
    flagSet.BoolVar(&overwriteTemp, "overwrite-temp", false, "Overwrite temporary merged files.")

    flagSet.StringVar(&queue, "q",          "out-of-order", "Order to download segments (sequential, out-of-order).")
    flagSet.StringVar(&queue, "queue-mode", "out-of-order", "Order to download segments (sequential, out-of-order).")

    flagSet.DurationVar(&requeueDelay, "requeue-delay", 2 * time.Minute, "How long to wait before retrying a requeued segment.")

    flagSet.UintVar(&requeueFailed, "requeue-failed", 1, "How many times should failed segments be requeued.")

    flagSet.BoolVar(&requeueLast, "requeue-last", false, "Whether or not the last segment should be requeued.")

    flagSet.UintVar(&failThreshold, "retries", download.DefaultFailThreshold, "Amount of times to retry downloading segments on failure.")

    flagSet.UintVar(&segmentCount, "segment-count", 0, "How many segments to download.")

    flagSet.StringVar(&tempDir, "temp-dir", "", "Directory to store temporary files. A randomly-named one will be created if empty.")

    flagSet.UintVar(&threads, "t",       1, "Multi-threaded download.")
    flagSet.UintVar(&threads, "threads", 1, "Multi-threaded download.")

    flagSet.BoolVar(&useQuic, "use-quic", true, "Whether or not HTTP/3 should be used.")

    flagSet.BoolVar(&verbose, "v",       false, "Enable debug logging. Overrides log-level.")
    flagSet.BoolVar(&verbose, "verbose", false, "Enable debug logging. Overrides log-level.")

    flagSet.BoolVar(&versionPrint, "V",       false, "Print version and exit")
    flagSet.BoolVar(&versionPrint, "version", false, "Print version and exit")

    flagSet.StringVar(&windowName, "window-name", "", "Window name to use.")

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

    if forceIPv4 && forceIPv6 {
        log.Fatalf("--ipv4 and --ipv6 options cannot be combined")
    } else if forceIPv4 {
        network = util.NetworkIPv4
    } else if forceIPv6 {
        network = util.NetworkIPv6
    }

    if input == "" && mergeOnlyFile == "" {
        log.Fatalf("No input file specified")
    }

    //can't parse the mergeOnlyFile struct here because of cyclic dependencies,
    //so only handle the regular info json
    if input != "" {
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
        log.Infof("Saving output to %s", output)
    }
}

