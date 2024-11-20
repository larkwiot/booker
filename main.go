package main

import (
	"encoding/json"
	"github.com/jessevdk/go-flags"
	"github.com/larkwiot/booker/internal"
	"github.com/larkwiot/booker/internal/book"
	"github.com/larkwiot/booker/internal/config"
	"github.com/larkwiot/booker/internal/util"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
)

func main() {
	log.SetFlags(0)

	var opts struct {
		ConfigPath  string `short:"c" long:"config" description:"filepath to configuration file" default:"./booker.toml"`
		ScanPath    string `short:"s" long:"scan" description:"directory path to scan" default:"./"`
		OutputPath  string `short:"o" long:"output" description:"filepath to write JSON output to" default:"./books.json"`
		Cache       string `long:"cache" description:"filepath to previous JSON output to use as cache"`
		Threads     int    `short:"t" long:"threads" description:"number of threads to use, set to 0 to automatically determine best count" default:"0"`
		DryRun      bool   `long:"dry-run" description:"do a dry-run (don't make any requests to providers)'"`
		RetryFailed bool   `long:"retry" descrption:"retry failed books (must also specify --cache)"`
		Version     bool   `long:"version" description:"print version"`
	}

	_, err := flags.Parse(&opts)
	if err != nil {
		log.Fatal(err)
	}

	if opts.Version {
		info, ok := debug.ReadBuildInfo()
		if !ok {
			log.Fatal("error: unable to get build info")
		}
		log.Println(info)
		os.Exit(0)
	}

	if opts.RetryFailed && opts.Cache == "" {
		log.Fatal("error: --cache must be specified you want to retry failed files")
	}

	conf, err := config.NewConfig(opts.ConfigPath)
	if err != nil {
		log.Fatal(err)
	}

	output, err := filepath.Abs(util.ExpandUser(opts.OutputPath))
	if err != nil {
		log.Printf("error: could not get absolute output path: %s\n", err.Error())
		return
	}
	if exists, _ := util.PathExists(output); exists {
		log.Printf("error: output filepath %s already exists, refusing to overwrite\n", output)
		return
	}

	outputWriter, err := util.NewJsonStreamWriter[*book.Book](output, func(bk *book.Book) (util.JsonStreamWriterItem, error) {
		bkData, err := json.Marshal(bk)
		if err != nil {
			return util.JsonStreamWriterItem{}, err
		}
		return util.JsonStreamWriterItem{
			Key:  bk.Filepath,
			Data: bkData,
		}, nil
	})
	if err != nil {
		log.Printf("error: unable to open to output path %s\n", output)
		return
	}

	bm, err := internal.NewBookManager(conf, int64(opts.Threads))
	if err != nil {
		log.Fatal(err)
	}
	defer bm.Shutdown()

	if len(opts.Cache) != 0 {
		err = bm.Import(opts.Cache, opts.RetryFailed)
		if err != nil {
			log.Printf("error: book manager failed to import cache %s: %s\n", opts.Cache, err.Error())
			return
		}
	}

	bm.Scan(opts.ScanPath, opts.DryRun, outputWriter)
}
