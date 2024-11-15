package providers

import (
	"encoding/json"
	"fmt"
	"github.com/larkwiot/booker/internal/book"
	"github.com/larkwiot/booker/internal/config"
	"github.com/larkwiot/booker/internal/util"
	"github.com/samber/mo"
	"log"
	"net/http"
	"path/filepath"
	"strings"
)

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

type Google struct {
	url          string
	apiKey       string
	isbnQueryUrl string
}

func NewGoogle(conf *config.GoogleConfig) Provider {
	google := Google{
		url:    fmt.Sprintf("https://%s", conf.Url),
		apiKey: conf.ApiKey,
	}
	if google.apiKey != "" {
		google.isbnQueryUrl = fmt.Sprintf("%s?key=%s", google.url, google.apiKey)
	} else {
		google.isbnQueryUrl = fmt.Sprintf("%s?", google.url)
	}
	return NewGeneric(&google, conf.MillisecondsPerRequest)
}

func (g *Google) Name() string {
	return "Google"
}

func (g *Google) FindResult(isbn book.ISBN, filePath string) (book.BookResult, error, int) {
	queryUrl := fmt.Sprintf("%s&q=isbn:%s", g.isbnQueryUrl, isbn)
	response, err := http.Get(queryUrl)
	if err != nil {
		return book.BookResult{}, err, 0
	}

	if response.StatusCode != http.StatusOK {
		return book.BookResult{}, fmt.Errorf("google returned bad status code %d: %s", response.StatusCode, response.Body), response.StatusCode
	}

	var result googleResponse

	err = json.NewDecoder(response.Body).Decode(&result)
	if err != nil {
		return book.BookResult{}, err, response.StatusCode
	}

	if result.TotalItems == 0 {
		return book.BookResult{}, nil, response.StatusCode
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
		return book.BookResult{}, fmt.Errorf("unable to identify a good match from multiple returned works"), response.StatusCode
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
	}, nil, response.StatusCode
}

func (g *Google) Shutdown() {
}

func (g *Google) HealthCheck() (bool, string) {
	return true, ""
}
