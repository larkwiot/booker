package providers

import (
	"encoding/json"
	"fmt"
	"github.com/larkwiot/booker/internal/book"
	"github.com/larkwiot/booker/internal/config"
	"github.com/larkwiot/booker/internal/util"
	"github.com/samber/lo"
	"github.com/samber/mo"
	"log"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

type Google struct {
	url         string
	priority    int
	rateLimiter <-chan time.Time
	cache       sync.Map
}

func NewGoogle(conf *config.GoogleConfig, priority int) *Google {
	g := &Google{
		url:         fmt.Sprintf("https://%s", conf.Url),
		priority:    priority,
		rateLimiter: time.Tick(time.Duration(conf.MillisecondsPerRequest) * time.Millisecond),
		cache:       sync.Map{},
	}

	return g
}

func (g *Google) Name() string {
	return "Google"
}

func (g *Google) Priority() int {
	return g.priority
}

func (g *Google) GetBookMetadata(search *SearchTerms) ([]book.BookResult, error) {
	results := make([]book.BookResult, 0)

	isbn10s := lo.Map(search.Isbn10s, func(isbn book.ISBN10, _ int) book.ISBN {
		return book.ISBN(isbn)
	})

	isbn13s := lo.Map(search.Isbn13s, func(isbn book.ISBN13, _ int) book.ISBN {
		return book.ISBN(isbn)
	})

	allIsbns := slices.Concat(isbn10s, isbn13s)

	for _, isbn := range allIsbns {
		var result book.BookResult
		if cachedResult, cached := g.cache.Load(isbn); cached {
			result = cachedResult.(book.BookResult)
		} else {
			uncachedResult, err := g.fetchMetadata(isbn, search.Filepath)
			if err != nil {
				return nil, err
			}
			g.cache.Store(isbn, uncachedResult)
			result = uncachedResult
		}
		results = append(results, result)
	}

	return results, nil
}

type googleIdentifier struct {
	Type       string `json:"type"`
	Identifier string `json:"identifier"`
}

type googleVolumeInfo struct {
	Title               string             `json:"title"`
	Authors             []string           `json:"authors"`
	IndustryIdentifiers []googleIdentifier `json:"industryIdentifiers"`
	PublishedDate       string             `json:"publishedDate"`
}

type googleItem struct {
	VolumeInfo googleVolumeInfo `json:"volumeInfo"`
}

type googleResponse struct {
	TotalItems int `json:"totalItems"`
	Items      []googleItem
}

func (g *Google) fetchMetadata(isbn book.ISBN, filePath string) (book.BookResult, error) {
	<-g.rateLimiter

	queryUrl := fmt.Sprintf("%s?q=isbn:%s", g.url, isbn)
	response, err := http.Get(queryUrl)
	if err != nil {
		return book.BookResult{}, err
	}

	if response.StatusCode != http.StatusOK {
		return book.BookResult{}, fmt.Errorf("error: google returned status code %d: %s", response.StatusCode, response.Body)
	}

	var result googleResponse

	err = json.NewDecoder(response.Body).Decode(&result)
	if err != nil {
		return book.BookResult{}, err
	}

	if result.TotalItems == 0 {
		return book.BookResult{}, nil
	}

	var bestResult googleItem
	magic := 999999999
	bestMatch := magic

	filename := filepath.Base(filePath)
	for _, item := range result.Items {
		distance := util.LevenshteinDistance(item.VolumeInfo.Title, filename)
		if distance < bestMatch {
			bestMatch = distance
			bestResult = item
		}
	}
	if bestMatch == magic {
		return book.BookResult{}, fmt.Errorf("unable to identify a good match from multiple returned works")
	}

	var isbn10 mo.Option[book.ISBN10]
	var isbn13 mo.Option[book.ISBN13]
	var uom mo.Option[string]

	for _, identifier := range bestResult.VolumeInfo.IndustryIdentifiers {
		switch strings.ToLower(identifier.Type) {
		case "isbn_10":
			isbn10 = mo.Some(book.ISBN10(identifier.Identifier))
		case "isbn_13":
			isbn13 = mo.Some(book.ISBN13(identifier.Identifier))
		case "uom":
			uom = mo.Some(identifier.Identifier)
		case "other":
			break
		default:
			log.Printf("info: google returned unsupported identifier type %s: %s", identifier.Type, identifier.Identifier)
		}
	}

	return book.BookResult{
		Filepath:           filePath,
		Title:              mo.Some(bestResult.VolumeInfo.Title),
		Authors:            mo.Some(bestResult.VolumeInfo.Authors),
		Isbn10:             isbn10,
		Isbn13:             isbn13,
		Uom:                uom,
		PublishDate:        mo.Some(bestResult.VolumeInfo.PublishedDate),
		Confidence:         100,
		SourceProviderName: "google",
	}, nil
}

func (g *Google) ClearCache() {
	g.cache = sync.Map{}
}

func (g *Google) Shutdown() {
}
