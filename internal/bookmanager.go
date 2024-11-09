package internal

import (
	"encoding/json"
	"fmt"
	"github.com/larkwiot/booker/internal/book"
	"github.com/larkwiot/booker/internal/config"
	"github.com/larkwiot/booker/internal/extractors"
	"github.com/larkwiot/booker/internal/providers"
	"github.com/larkwiot/booker/internal/util"
	"github.com/samber/lo"
	"github.com/schollz/progressbar/v3"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

var acceptedFileTypes = []string{
	".pdf",
	".epub",
	".mobi",
	".chm",
	".rst",
	".txt",
}

type BookManager struct {
	providers     []providers.Provider
	extractors    []extractors.Extractor
	workQueue     chan book.Book
	quit          chan struct{}
	maxCharacters uint
	maxAttempts   uint
	bookStateLock *sync.Mutex
	books         map[string]book.Book
	dryRun        bool
	waitGroup     *sync.WaitGroup
	pb            *progressbar.ProgressBar
	outputWriter  *util.JsonStreamWriter
}

func NewBookManager(conf *config.Config) (*BookManager, error) {
	err := conf.Validate()
	if err != nil {
		return nil, err
	}

	var bm = BookManager{
		providers:     make([]providers.Provider, 0),
		extractors:    make([]extractors.Extractor, 0),
		quit:          make(chan struct{}),
		maxCharacters: conf.Advanced.MaxCharactersToSearchForIsbn,
		maxAttempts:   conf.Advanced.MaxAttemptsToProcessBook,
		bookStateLock: &sync.Mutex{},
		books:         make(map[string]book.Book),
		dryRun:        false,
		waitGroup:     &sync.WaitGroup{},
	}

	if conf.Tika.Enable {
		bm.extractors = append(bm.extractors, extractors.NewTikaServer(&conf.Tika))
	}

	if conf.Google.Enable {
		bm.providers = append(bm.providers, providers.NewGoogle(&conf.Google, conf.Advanced.GooglePriority))
	}

	if len(bm.extractors) == 0 {
		return nil, fmt.Errorf("at least one extractor must be enabled")
	}

	if len(bm.providers) == 0 {
		return nil, fmt.Errorf("at least one provider must be enabled")
	}

	return &bm, nil
}

func (bm *BookManager) Shutdown() {
	for _, provider := range bm.providers {
		provider.Shutdown()
	}
	for _, extractor := range bm.extractors {
		extractor.Shutdown()
	}
	close(bm.quit)
	bm.waitGroup.Wait()
}

func makeJsonStreamItem(bk *book.Book) *util.JsonStreamWriterItem {
	bkData, err := json.Marshal(bk)
	if err != nil {
		return nil
	}
	return &util.JsonStreamWriterItem{
		Key:  bk.Filepath,
		Data: bkData,
	}
}

func (bm *BookManager) finishBook(bk *book.Book) {
	if bm.isBookProcessed(bk.Filepath) {
		//log.Printf("error: book %s was already processed\n", bk.Filepath)
		return
	}

	bm.pb.Add(1)

	bm.bookStateLock.Lock()
	defer bm.bookStateLock.Unlock()

	bm.outputWriter.Input <- makeJsonStreamItem(bk)
	bm.books[bk.Filepath] = *bk
}

func (bm *BookManager) isBookProcessed(filePath string) bool {
	bm.bookStateLock.Lock()
	defer bm.bookStateLock.Unlock()
	_, isProcessed := bm.books[filePath]
	return isProcessed
}

func (bm *BookManager) getProcessedBook(filePath string) book.Book {
	bm.bookStateLock.Lock()
	defer bm.bookStateLock.Unlock()
	return bm.books[filePath]
}

func (bm *BookManager) removeProcessedBook(filePath string) {
	bm.bookStateLock.Lock()
	defer bm.bookStateLock.Unlock()
	delete(bm.books, filePath)
}

func (bm *BookManager) dispatch() {
	//log.Println("book manager: dispatcher launched")

	defer func() {
		//log.Println("book manager: dispatcher quitting")
		bm.waitGroup.Done()
	}()

	for {
		select {
		case bk, isOpen := <-bm.workQueue:
			if !isOpen {
				return
			}
			bm.waitGroup.Add(1)
			go bm.ingest(&bk)
		case <-bm.quit:
			return
		}
	}
}

func (bm *BookManager) bestThreadCount() int {
	return runtime.NumCPU() * len(bm.providers) * 2
}

func (bm *BookManager) ingest(bk *book.Book) {
	defer bm.waitGroup.Done()

	texts := bm.extract(bk)

	if len(texts) == 0 {
		bk.ErrorMessage = fmt.Sprintf("book had no texts: %s", bk.Filepath)
		//log.Println(reason)
		bm.finishBook(bk)
		return
	}

	isbn10s := make([]book.ISBN10, 0)
	isbn13s := make([]book.ISBN13, 0)

	for _, result := range texts {
		if result.Error != nil {
			bk.ErrorMessage = fmt.Sprintf("error: extractor %s failed with: %s", result.Name, result.Error)
			bm.finishBook(bk)
			continue
		}

		isbn10s = append(isbn10s, util.IdentifyIsbn10s(result.Text)...)
		isbn13s = append(isbn13s, util.IdentifyIsbn13s(result.Text)...)
	}

	search := providers.SearchTerms{
		Isbn10s:  isbn10s,
		Isbn13s:  isbn13s,
		Filepath: bk.Filepath,
	}

	if !search.HasAnyTerms() {
		return
	}

	results := bm.getBookMetadata(&search)

	bookResults := make([]book.BookResult, 0)

	for _, providerResults := range results {
		if providerResults.Error != nil {
			//log.Printf("warning: provider %s failed to get metadata with %s\n", providerName, providerResults.Error)
			continue
		}

		identifiedResults := lo.Filter(providerResults.Results, func(result book.BookResult, _ int) bool {
			return !result.IsUnidentified()
		})

		bookResults = append(bookResults, identifiedResults...)
	}

	if len(bookResults) == 0 {
		return
	}

	bestOverall, err := book.ChooseBestResult(bookResults)
	if err != nil {
		bk.ErrorMessage = fmt.Sprintf("error: could not calculate best total result: %s", err.Error())
		//log.Println(reason)
		bm.finishBook(bk)
		return
	}

	newBook := bestOverall.ToBook()
	bm.finishBook(&newBook)
}

func (bm *BookManager) StartDryRun() {
	bm.dryRun = true
}

func (bm *BookManager) EndDryRun() {
	bm.dryRun = false
}

func (bm *BookManager) IsDryRun() bool {
	return bm.dryRun
}

func (bm *BookManager) Scan(scanPath string, cache string, threads int, dryRun bool, output string, retry bool) {
	scanPath, err := filepath.Abs(scanPath)
	if err != nil {
		log.Printf("error: could not get absolute scan path: %s\n", err.Error())
		return
	}

	if dryRun {
		bm.StartDryRun()
		defer bm.EndDryRun()
	}

	if threads == 0 {
		threads = bm.bestThreadCount()
	}
	if threads > 2000 {
		threads = 2000
	}

	log.Printf("book manager: preparing to scan with %d threads\n", threads)

	if len(cache) != 0 {
		err := bm.importFrom(cache)
		if err != nil {
			log.Printf("error: book manager failed to import cache %s\n", cache)
			return
		}
		log.Printf("book manager: loaded %d cached entries\n", len(bm.books))
	}

	outputWriter, err := util.NewJsonStreamWriter(output)
	if err != nil {
		log.Printf("error: unable to open to output path %s\n", output)
		return
	}
	bm.outputWriter = outputWriter
	defer bm.outputWriter.Close()

	log.Println("book manager: beginning scan")

	bm.pb = progressbar.NewOptions(
		-1,
		progressbar.OptionSetElapsedTime(true),
		progressbar.OptionSetDescription("scanning books"),
		progressbar.OptionSetTheme(progressbar.ThemeASCII),
		progressbar.OptionSetWidth(40),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionShowIts(),
	)

	bm.pb.Set(len(bm.books))

	// write any existing books back out (mainly if we imported a cache)
	for _, bk := range bm.books {
		bm.outputWriter.Input <- makeJsonStreamItem(&bk)
	}

	bm.workQueue = make(chan book.Book, threads)

	bm.waitGroup.Add(1)
	go bm.dispatch()

	time.Sleep(1 * time.Second)

	err = filepath.WalkDir(scanPath, func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() {
			return nil
		}

		if d.Type() == os.ModeSymlink {
			return nil
		}

		ext := filepath.Ext(d.Name())
		if !lo.Contains(acceptedFileTypes, ext) {
			//log.Printf("%s is not an accepted filetype\n", ext)
			return nil
		}

		if bm.isBookProcessed(path) {
			if retry {
				bk := bm.getProcessedBook(path)
				if len(bk.ErrorMessage) != 0 {
					bm.removeProcessedBook(path)
					bk.ErrorMessage = ""
					bm.workQueue <- bk
				}
			}
			//log.Printf("book manager: skipping already-processed %s\n", path)
			return nil
		}

		bm.workQueue <- book.Book{Filepath: path}
		return nil
	})

	if err != nil {
		log.Printf("error: failed to scan %s: %s\n", scanPath, err)
	}

	close(bm.workQueue)

	bm.waitGroup.Wait()

	bm.pb.Close()
	bm.pb = nil

	log.Println("book manager: scan complete")
}

func (bm *BookManager) importFrom(outputPath string) error {
	data, err := os.ReadFile(outputPath)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &bm.books)
}

type extractorResult struct {
	Name  string
	Text  string
	Error error
}

func (bm *BookManager) extract(bk *book.Book) []extractorResult {
	return lo.Map(bm.extractors, func(extractor extractors.Extractor, _ int) extractorResult {
		r, err := extractor.ExtractText(bk, bm.maxCharacters)
		return extractorResult{Name: extractor.Name(), Text: r, Error: err}
	})
}

type providerResult struct {
	Results []book.BookResult
	Error   error
}

func (bm *BookManager) getBookMetadata(search *providers.SearchTerms) map[string]providerResult {
	if bm.IsDryRun() {
		return map[string]providerResult{}
	}

	return lo.SliceToMap(bm.providers, func(provider providers.Provider) (string, providerResult) {
		results, err := provider.GetBookMetadata(search)
		return provider.Name(), providerResult{
			Results: results, Error: err,
		}
	})
}
