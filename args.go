package main

import (
    "encoding/json"
    "flag"
    "fmt"
    "io/ioutil"
    "os"
    "strings"
    "time"

    "github.com/notpeko/ytarchive-raw-go/download"
    "github.com/notpeko/ytarchive-raw-go/log"
)

const DefaultOutputFormat = "%(upload_date)s %(title)s (%(id)s).mkv"

var (
    output    string
    threads   uint
    timeout   time.Duration
    logLevel  string
    fregData  FregJson
    queueMode download.QueueMode
)

func init() {
    flags := flag.NewFlagSet("flags", flag.ExitOnError)

    var input string
    flags.StringVar(&input, "i",     "", "Input JSON file.")
    flags.StringVar(&input, "input", "", "Input JSON file.")

    flags.StringVar(&output, "o",      DefaultOutputFormat, "Output file path.")
    flags.StringVar(&output, "output", DefaultOutputFormat, "Output file path.")

    flags.UintVar(&threads, "t",       1, "Multi-threaded download.")
    flags.UintVar(&threads, "threads", 1, "Multi-threaded download.")

    flags.DurationVar(&timeout, "T",       20 * time.Second, "Secs for retrying when encounter HTTP errors. Default 20.")
    flags.DurationVar(&timeout, "timeout", 20 * time.Second, "Secs for retrying when encounter HTTP errors. Default 20.")

    var queue string
    flags.StringVar(&queue, "q",          "auto", "Order to download segments (sequential, out-of-order, auto).")
    flags.StringVar(&queue, "queue-mode", "auto", "Order to download segments (sequential, out-of-order, auto).")

    var verbose bool
    flags.BoolVar(&verbose, "v",       false, "Enable debug logging. Overrides log-level.")
    flags.BoolVar(&verbose, "verbose", false, "Enable debug logging. Overrides log-level.")

    flags.StringVar(&logLevel, "log-level", "info", "Log level to use (debug, info, warn, error, fatal).")

    flags.Parse(os.Args[1:])
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
    case "auto":
        queueMode = download.QueueAuto
    case "sequential":
        queueMode = download.QueueSequential
    case "out-of-order":
        queueMode = download.QueueOutOfOrder
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

    if !strings.HasSuffix(output, ".mkv") {
        log.Fatal("Output should be a .mkv file")
    }

    log.Infof("Saving output to %s", output)
}

