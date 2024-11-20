package internal

import (
	"encoding/json"
	"fmt"
	"github.com/larkwiot/booker/internal/book"
	"github.com/larkwiot/booker/internal/config"
	"github.com/larkwiot/booker/internal/extractors"
	"github.com/larkwiot/booker/internal/pipeline"
	"github.com/larkwiot/booker/internal/providers"
	"github.com/larkwiot/booker/internal/service"
	"github.com/larkwiot/booker/internal/util"
	"github.com/samber/lo"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var acceptedFileTypes = []string{
	".pdf",
	".epub",
	".mobi",
	".chm",
	".htm",
	".html",
	".rst",
	".rtf",
	".txt",
	".doc",
	".docx",
}

type BookManager struct {
	providers         []providers.Provider
	extractors        []extractors.Extractor
	pipe              *pipeline.Pipeline
	maxCharacters     uint
	bookStateLock     *sync.RWMutex
	books             map[string]book.Book
	dryRun            bool
	writer            util.ObjectWriter[*book.Book]
	extractorsManager *service.ServiceManager
	providersManager  *service.ServiceManager
}

func NewBookManager(conf *config.Config, threads int64) (*BookManager, error) {
	err := conf.Validate()
	if err != nil {
		return nil, err
	}

	var bm = BookManager{
		providers:         make([]providers.Provider, 0),
		extractors:        make([]extractors.Extractor, 0),
		maxCharacters:     conf.Advanced.MaxCharactersToSearchForIsbn,
		bookStateLock:     &sync.RWMutex{},
		books:             make(map[string]book.Book),
		dryRun:            false,
		extractorsManager: service.NewServiceManager(15 * time.Second),
		providersManager:  service.NewServiceManager(15 * time.Second),
	}

	if conf.Tika.Enable {
		bm.extractors = append(bm.extractors, extractors.NewTikaServer(&conf.Tika))
	}

	if conf.Google.Enable {
		bm.providers = append(bm.providers, providers.NewGoogle(&conf.Google))
	}

	if len(bm.extractors) == 0 {
		return nil, fmt.Errorf("at least one extractor must be enabled")
	}

	if len(bm.providers) == 0 {
		return nil, fmt.Errorf("at least one provider must be enabled")
	}

	for _, extractor := range bm.extractors {
		bm.extractorsManager.Manage(extractor)
	}
	for _, provider := range bm.providers {
		bm.providersManager.Manage(provider)
	}

	if threads == 0 {
		threads = int64(bm.bestThreadCount())
		log.Printf("info: determined best thread count as: %d\n", threads)
	}
	if threads > 2000 {
		threads = 2000
		log.Printf("info: capping thread count to %d\n", threads)
	}
	if threads&1 > 0 {
		log.Printf("info: making thread count even to %d\n", threads)
		threads += 1
	}

	bm.pipe = pipeline.NewPipeline(threads)
	bm.pipe.AppendStage("extract", bm.extract)
	bm.pipe.AppendStage("search", bm.search)
	bm.pipe.AppendStage("collate", bm.collate)
	bm.pipe.CollectorStage(bm.finishBook)

	return &bm, nil
}

func (bm *BookManager) Shutdown() {
	for _, provider := range bm.providers {
		provider.Shutdown()
	}
	for _, extractor := range bm.extractors {
		extractor.Shutdown()
	}
	bm.pipe.Wait()
}

func (bm *BookManager) bestThreadCount() int {
	if len(bm.providers) == 0 {
		log.Printf("warning: cannot calculate best thread count without any providers initialized. Please create an issue for this")
		return 0
	}
	return (runtime.NumCPU() / len(bm.extractors)) * len(bm.providers)
}

func (bm *BookManager) finishBook(b any) {
	if bm.writer == nil {
		return
	}

	bk := b.(book.Book)

	if bm.isBookProcessed(bk.Filepath) {
		//log.Printf("error: book %s was already processed\n", bk.Filepath)
		return
	}

	bm.bookStateLock.Lock()
	defer bm.bookStateLock.Unlock()

	bm.writer.WriteObject(&bk)
	bm.books[bk.Filepath] = bk
}

func (bm *BookManager) isBookProcessed(filePath string) bool {
	bm.bookStateLock.RLock()
	defer bm.bookStateLock.RUnlock()
	_, isProcessed := bm.books[filePath]
	return isProcessed
}

func (bm *BookManager) getProcessedBook(filePath string) book.Book {
	bm.bookStateLock.RLock()
	defer bm.bookStateLock.RUnlock()
	return bm.books[filePath]
}

func (bm *BookManager) removeProcessedBook(filePath string) {
	bm.bookStateLock.Lock()
	defer bm.bookStateLock.Unlock()
	delete(bm.books, filePath)
}

func (bm *BookManager) getProcessedBookCount() uint64 {
	bm.bookStateLock.RLock()
	defer bm.bookStateLock.RUnlock()
	return uint64(len(bm.books))
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

func (bm *BookManager) Scan(scanPath string, dryRun bool, writer util.ObjectWriter[*book.Book]) {
	scanPath, err := filepath.Abs(util.ExpandUser(scanPath))
	if err != nil {
		log.Printf("error: could not get absolute scan path: %s\n", err.Error())
		return
	}

	if exists, err := util.PathExists(scanPath); !exists {
		log.Printf("error: could not stat scan path: %s\n", err)
		return
	}

	bm.writer = writer
	defer func() {
		bm.writer.Close()
		bm.writer = nil
	}()

	if dryRun {
		bm.StartDryRun()
		defer bm.EndDryRun()
	}

	log.Printf("book manager: preparing to scan with %d threads\n", bm.pipe.TotalThreadCount)

	// write any existing books back out (mainly if we imported a cache)
	for _, bk := range bm.books {
		bm.writer.WriteObject(&bk)
	}

	log.Printf("book manager: loaded %d cached entries\n", bm.getProcessedBookCount())

	log.Printf("book manager: beginning scan on %s\n", scanPath)

	bm.pipe.Run(bm.failHandler)

	bookCount := bm.getProcessedBookCount()

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

		path, err = filepath.Abs(path)
		if err != nil {
			return err
		}

		if bm.isBookProcessed(path) {
			//log.Printf("book manager: skipping already-processed %s\n", path)
			return nil
		}

		bookCount++

		bm.pipe.Frontend <- book.Book{Filepath: path}
		return nil
	})

	if err != nil {
		log.Printf("error: failed to completely scan %s: %s\n", scanPath, err)
	}

	//log.Printf("%sbook manager: all jobs created, waiting for processing to complete", util.ClearTermLineString())

	for bm.getProcessedBookCount() != bookCount {
		if len(bm.extractorsManager.GetLiveServices()) == 0 {
			log.Println("error: all extractors down")
			bm.pipe.Wait()
			bm.pipe.Close()
			return
		}
		if len(bm.providersManager.GetLiveServices()) == 0 {
			log.Println("error: all providers down")
			bm.pipe.Wait()
			bm.pipe.Close()
			return
		}
		time.Sleep(500 * time.Millisecond)
	}

	bm.pipe.Wait()
	bm.pipe.Close()

	log.Println("book manager: scan complete")
}

func (bm *BookManager) Import(cache string, removeErrored bool) error {
	if exists, err := util.PathExists(cache); !exists || err != nil {
		return fmt.Errorf("error: could not open cache %s: %s", cache, err)
	}
	data, err := os.ReadFile(cache)
	if err != nil {
		return err
	}

	err = json.Unmarshal(data, &bm.books)
	if err != nil {
		return err
	}
	if removeErrored {
		for p, bk := range bm.books {
			if len(bk.ErrorMessage) > 0 {
				bm.removeProcessedBook(p)
			}
		}
	}
	return nil
}

func (bm *BookManager) extract(a any) (any, error) {
	bk := a.(book.Book)

	texts := make([]string, 0)

	liveExtractors := bm.extractorsManager.GetLiveServices()
	if len(liveExtractors) == 0 {
		return nil, fmt.Errorf("error: no live extractors found")
	}

	for _, svc := range liveExtractors {
		extractor := svc.(extractors.Extractor)

		text, err := extractor.ExtractText(&bk, bm.maxCharacters)
		if err != nil {
			//log.Printf("error: failed to extract text from %s: %s\n", bk.Filepath, err)
			continue
		}
		texts = append(texts, text)
	}

	if len(texts) == 0 {
		return bk, fmt.Errorf("no texts extracted")
	}

	isbn10s := make([]book.ISBN10, 0)
	isbn13s := make([]book.ISBN13, 0)

	for _, text := range texts {
		isbn10s = append(isbn10s, util.IdentifyIsbn10s(text)...)
		isbn13s = append(isbn13s, util.IdentifyIsbn13s(text)...)
	}

	search := providers.SearchTerms{
		Isbn10s:  isbn10s,
		Isbn13s:  isbn13s,
		Filepath: bk.Filepath,
	}

	return search, nil
}

func (bm *BookManager) search(a any) (any, error) {
	search := a.(providers.SearchTerms)

	if bm.IsDryRun() {
		return nil, fmt.Errorf("dry run")
	}

	results := make([]book.BookResult, 0)

	liveProviders := bm.providersManager.GetLiveServices()
	if len(liveProviders) == 0 {
		return nil, fmt.Errorf("error: no live providers found")
	}

	for _, svc := range liveProviders {
		provider := svc.(providers.Provider)
		res, err := provider.GetBookMetadata(&search)
		if err != nil {
			continue
		}
		results = append(results, res...)
	}

	if len(results) == 0 {
		return results, fmt.Errorf("error: no results found")
	}

	return results, nil
}

func (bm *BookManager) collate(a any) (any, error) {
	results := a.([]book.BookResult)
	result, err := book.ChooseBestResult(results)
	if err != nil {
		return book.Book{}, fmt.Errorf("could not collate: %s", err.Error())
	}

	return result.ToBook(), nil
}

func (bm *BookManager) failHandler(a any, err error) {
	if a == nil {
		if strings.Contains(err.Error(), "dry run") {
			return
		}
		log.Println(err.Error())
		return
	}

	switch a.(type) {
	case book.Book:
		b := a.(book.Book)
		b.ErrorMessage = err.Error()
		bm.finishBook(b)
	case book.BookResult:
	case []book.BookResult:
	case providers.SearchTerms:
	default:
		log.Printf("warning: fail handler cannot handle type %s with %s\n", a, err.Error())
	}
}
