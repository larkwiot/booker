package main

import (
	"github.com/jessevdk/go-flags"
	"github.com/larkwiot/booker/internal"
	"github.com/larkwiot/booker/internal/config"
	"log"
)

func main() {
	var opts struct {
		ConfigPath  string `short:"c" long:"config" description:"filepath to configuration file" default:"./booker.toml"`
		ScanPath    string `short:"s" long:"scan" description:"directory path to scan" default:"./"`
		OutputPath  string `short:"o" long:"output" description:"filepath to write JSON output to" default:"./books.json"`
		Cache       string `long:"cache" description:"filepath to previous JSON output to use as cache"`
		Threads     int    `short:"t" long:"threads" description:"number of threads to use, set to 0 to automatically determine best count" default:"0"`
		DryRun      bool   `long:"dry-run" description:"do a dry-run (don't make any requests to providers)'"`
		RetryFailed bool   `long:"retry" descrption:"retry failed books (must also specify --cache)"`
	}

	_, err := flags.Parse(&opts)
	if err != nil {
		log.Fatal(err)
	}

	conf, err := config.NewConfig(opts.ConfigPath)
	if err != nil {
		log.Fatal(err)
	}

	bm, err := internal.NewBookManager(conf)
	if err != nil {
		log.Fatal(err)
	}
	defer bm.Shutdown()

	bm.Scan(opts.ScanPath, opts.Cache, opts.Threads, opts.DryRun, opts.OutputPath, opts.RetryFailed)
}
