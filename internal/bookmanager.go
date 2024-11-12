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
	"sync/atomic"
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
	providers          []providers.Provider
	extractors         []extractors.Extractor
	extractQueue       chan book.Book
	searchQueue        chan providers.SearchTerms
	collateQueue       chan []book.BookResult
	finishQueue        chan book.Book
	maxCharacters      uint
	maxAttempts        uint
	bookStateLock      *sync.Mutex
	books              map[string]book.Book
	dryRun             bool
	scanWaitGroup      *sync.WaitGroup
	pb                 *progressbar.ProgressBar
	outputWriter       *util.JsonStreamWriter
	threads            int64
	currentThreadCount atomic.Int64
}

func NewBookManager(conf *config.Config, threads int) (*BookManager, error) {
	err := conf.Validate()
	if err != nil {
		return nil, err
	}

	var bm = BookManager{
		providers:          make([]providers.Provider, 0),
		extractors:         make([]extractors.Extractor, 0),
		maxCharacters:      conf.Advanced.MaxCharactersToSearchForIsbn,
		maxAttempts:        conf.Advanced.MaxAttemptsToProcessBook,
		bookStateLock:      &sync.Mutex{},
		books:              make(map[string]book.Book),
		dryRun:             false,
		scanWaitGroup:      &sync.WaitGroup{},
		currentThreadCount: atomic.Int64{},
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

	if threads == 0 {
		bm.threads = int64(bm.bestThreadCount())
		log.Printf("info: determined best thread count as: %d\n", bm.threads)
	} else {
		bm.threads = int64(threads)
	}
	if threads > 2000 {
		bm.threads = 2000
	}

	bm.extractQueue = make(chan book.Book, bm.threads+1)
	bm.searchQueue = make(chan providers.SearchTerms, bm.threads+1)
	bm.collateQueue = make(chan []book.BookResult, bm.threads+1)
	bm.finishQueue = make(chan book.Book, bm.threads+1)

	return &bm, nil
}

func (bm *BookManager) Shutdown() {
	for _, provider := range bm.providers {
		provider.Shutdown()
	}
	for _, extractor := range bm.extractors {
		extractor.Shutdown()
	}
	close(bm.extractQueue)
	close(bm.searchQueue)
	close(bm.collateQueue)
	close(bm.finishQueue)
	bm.scanWaitGroup.Wait()
}

func (bm *BookManager) bestThreadCount() int {
	return runtime.NumCPU() * len(bm.providers) * 2
}

func (bm *BookManager) queueDepth() int {
	return len(bm.extractQueue) + len(bm.searchQueue) + len(bm.collateQueue) + len(bm.finishQueue)
}

func (bm *BookManager) finishBook(bk *book.Book) {
	defer bm.finishThread()

	if bm.isBookProcessed(bk.Filepath) {
		//log.Printf("error: book %s was already processed\n", bk.Filepath)
		return
	}

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

func (bm *BookManager) getProcessedBookCount() uint64 {
	bm.bookStateLock.Lock()
	defer bm.bookStateLock.Unlock()
	return uint64(len(bm.books))
}

func (bm *BookManager) addThread() {
	for bm.getThreadCount() >= bm.threads {
		time.Sleep(500 * time.Millisecond)
	}
	bm.scanWaitGroup.Add(1)
	bm.currentThreadCount.Add(1)
}

func (bm *BookManager) getThreadCount() int64 {
	return bm.currentThreadCount.Load()
}

func (bm *BookManager) finishThread() {
	bm.currentThreadCount.Add(-1)
	bm.scanWaitGroup.Done()
}

func (bm *BookManager) dispatch(extractorsCount *atomic.Int64, searchersCount *atomic.Int64, quit chan struct{}) {
	for {
		select {
		case bk, isOpen := <-bm.extractQueue:
			if !isOpen {
				return
			}
			bm.addThread()
			extractorsCount.Add(1)
			go bm.extract(bk, extractorsCount)
		case srch, isOpen := <-bm.searchQueue:
			if !isOpen {
				return
			}
			bm.addThread()
			searchersCount.Add(1)
			go bm.search(srch, searchersCount)
		case res, isOpen := <-bm.collateQueue:
			if !isOpen {
				return
			}
			bm.addThread()
			go bm.collate(res)
		case bk, isOpen := <-bm.finishQueue:
			if !isOpen {
				return
			}
			bm.addThread()
			go bm.finishBook(&bk)
		case <-quit:
			return
		}
	}
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

func (bm *BookManager) Scan(scanPath string, cache string, dryRun bool, output string, retry bool) {
	scanPath, err := filepath.Abs(util.ExpandUser(scanPath))
	if err != nil {
		log.Printf("error: could not get absolute scan path: %s\n", err.Error())
		return
	}

	_, err = os.Stat(scanPath)
	if err != nil {
		log.Printf("error: could not stat scan path: %s\n", err.Error())
		return
	}

	if dryRun {
		bm.StartDryRun()
		defer bm.EndDryRun()
	}

	log.Printf("book manager: preparing to scan with %d threads\n", bm.threads)

	if len(cache) != 0 {
		err := bm.importFrom(cache)
		if err != nil {
			log.Printf("error: book manager failed to import cache %s\n", cache)
			return
		}

		if retry {
			for p, bk := range bm.books {
				if len(bk.ErrorMessage) > 0 {
					bm.removeProcessedBook(p)
				}
			}
		}
	}

	outputWriter, err := util.NewJsonStreamWriter(output)
	if err != nil {
		log.Printf("error: unable to open to output path %s\n", output)
		return
	}
	bm.outputWriter = outputWriter
	defer bm.outputWriter.Close()

	// write any existing books back out (mainly if we imported a cache)
	for _, bk := range bm.books {
		bm.outputWriter.Input <- makeJsonStreamItem(&bk)
	}

	log.Printf("book manager: loaded %d cached entries\n", bm.getProcessedBookCount())

	log.Println("book manager: beginning scan")

	fsWalkStatus := make(chan string)
	go func() {
		for {
			select {
			case d, isOpen := <-fsWalkStatus:
				if !isOpen {
					fmt.Printf(util.ClearTermLineString())
					return
				}
				fmt.Printf("%sscanning: %s", util.ClearTermLineString(), d)
			}
		}
	}()

	extractorsCounter := atomic.Int64{}
	searchersCounter := atomic.Int64{}

	dispatchStop := make(chan struct{})
	go bm.dispatch(&extractorsCounter, &searchersCounter, dispatchStop)

	bookCount := bm.getProcessedBookCount()

	err = filepath.WalkDir(scanPath, func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() {
			fsWalkStatus <- path
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
			//log.Printf("book manager: skipping already-processed %s\n", path)
			return nil
		}

		bookCount++

		bm.extractQueue <- book.Book{Filepath: path}
		return nil
	})

	close(fsWalkStatus)

	if err != nil {
		log.Printf("error: failed to completely scan %s: %s\n", scanPath, err)
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	go func() {
		for {
			select {
			case _, isOpen := <-ticker.C:
				if !isOpen {
					return
				}
				fmt.Printf("%sstatus: queued %d -> extracting %d -> searching %d -> finished %d", util.ClearTermLineString(), len(bm.extractQueue), extractorsCounter.Load(), searchersCounter.Load(), bm.getProcessedBookCount())
			}
		}
	}()

	for bm.getProcessedBookCount() != bookCount {
		time.Sleep(500 * time.Millisecond)
	}
	bm.scanWaitGroup.Wait()

	dispatchStop <- struct{}{}

	ticker.Stop()

	fmt.Printf(util.ClearTermLineString())

	log.Println("book manager: scan complete")
}

func (bm *BookManager) importFrom(outputPath string) error {
	data, err := os.ReadFile(outputPath)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &bm.books)
}

func (bm *BookManager) extract(bk book.Book, counter *atomic.Int64) {
	defer func() {
		bm.finishThread()
		counter.Add(-1)
	}()

	texts := make([]string, 0)

	for _, extractor := range bm.extractors {
		text, err := extractor.ExtractText(&bk, bm.maxCharacters)
		if err != nil {
			//log.Printf("error: failed to extract text from %s: %s\n", bk.Filepath, err)
			continue
		}
		texts = append(texts, text)
	}

	if len(texts) == 0 {
		bk.ErrorMessage = "no texts extracted"
		bm.finishQueue <- bk
		return
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

	bm.searchQueue <- search
}

func (bm *BookManager) search(search providers.SearchTerms, counter *atomic.Int64) {
	defer func() {
		bm.finishThread()
		counter.Add(-1)
	}()

	if bm.IsDryRun() {
		return
	}

	results := make([]book.BookResult, 0)

	for _, provider := range bm.providers {
		res, err := provider.GetBookMetadata(&search)
		if err != nil {
			continue
		}
		results = append(results, res...)
	}

	if len(results) == 0 {
		bm.finishQueue <- book.Book{
			Filepath:     search.Filepath,
			ErrorMessage: "no results found",
		}
		return
	}

	bm.collateQueue <- results
}

func (bm *BookManager) collate(results []book.BookResult) {
	defer bm.finishThread()

	result, err := book.ChooseBestResult(results)
	if err != nil {
		bm.finishQueue <- book.Book{
			Filepath:     results[0].Filepath,
			ErrorMessage: fmt.Sprintf("could not collate: %s", err.Error()),
		}
		return
	}

	bm.finishQueue <- result.ToBook()
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
