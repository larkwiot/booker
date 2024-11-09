package book

import (
	"fmt"
	"github.com/samber/mo"
	"math"
	"strings"
)

type ISBN string
type ISBN10 ISBN
type ISBN13 ISBN

var badIsbns = map[string]struct{}{
	"0123456789": {},
	"0000000000": {},
	"1111111111": {},
	"2222222222": {},
	"3333333333": {},
	"4444444444": {},
	"5555555555": {},
	"6666666666": {},
	"7777777777": {},
	"8888888888": {},
	"9999999999": {},
}

func IsIsbnCandidate(s string) bool {
	l := len(s)
	if l != 10 && l != 13 {
		return false
	}

	for _, c := range strings.ToUpper(s) {
		if c != 'X' && c > '9' {
			return false
		}
		if c != 'X' && c < '0' {
			return false
		}
	}

	_, isBad := badIsbns[s]
	return !isBad
}

func (isbn *ISBN10) IsValid() bool {
	ctoi := func(c int32) int {
		return int(c - '0')
	}

	var sum = 0

	sisbn := string(*isbn)

	for i, c := range sisbn {
		multiplier := 10 - i

		if '0' <= c && c <= '9' {
			sum += multiplier * ctoi(c)
		} else if c == 'X' {
			if i != len(sisbn)-1 {
				// X is only permitted to appear at the end
				return false
			}
			sum += 10
		}
	}

	return sum%11 == 0
}

func (isbn *ISBN13) IsValid() bool {
	ctoi := func(c int32) uint {
		return uint(c - '0')
	}

	var multiplier uint = 1
	var sum uint = 0

	sisbn := string(*isbn)

	for _, c := range sisbn {
		sum += multiplier * ctoi(c)
		multiplier ^= 1 ^ 3
	}

	return sum%10 == 0
}

type Book struct {
	Title        string            `json:"title"`
	Authors      []string          `json:"authors"`
	Isbn10       mo.Option[ISBN10] `json:"isbn10"`
	Isbn13       mo.Option[ISBN13] `json:"isbn13"`
	Uom          mo.Option[string] `json:"uom"`
	LowYear      mo.Option[uint]   `json:"low_year"`
	HighYear     mo.Option[uint]   `json:"high_year"`
	Filepath     string            `json:"filepath"`
	ErrorMessage string            `json:"error"`
}

func (b *Book) String() string {
	return fmt.Sprintf("{\"title\": \"%s\", \"authors\": %s, \"isbn10\": %s, \"isbn13\": %s, \"filepath\": %s}", b.Title, b.Authors, b.Isbn10.OrElse(""), b.Isbn13.OrElse(""), b.Filepath)
}

func (b *Book) BestIdentifier() string {
	if b.Isbn13.IsPresent() {
		return string(b.Isbn13.MustGet())
	}
	if b.Isbn10.IsPresent() {
		return string(b.Isbn10.MustGet())
	}
	if b.Uom.IsPresent() {
		return string(b.Uom.MustGet())
	}
	if len(b.Title) != 0 {
		return b.Title
	}
	return b.Filepath
}

type BookResult struct {
	Filepath           string
	Title              mo.Option[string]
	Authors            mo.Option[[]string]
	Isbn10             mo.Option[ISBN10]
	Isbn13             mo.Option[ISBN13]
	Uom                mo.Option[string]
	LowYear            mo.Option[uint]
	HighYear           mo.Option[uint]
	PublishDate        mo.Option[string]
	Confidence         float64
	SourceProviderName string
}

func (br *BookResult) IsUnidentified() bool {
	return br.Title.IsAbsent() && br.Authors.IsAbsent() && br.Isbn10.IsAbsent() && br.Isbn13.IsAbsent()
}

func (br *BookResult) ToBook() Book {
	bk := Book{
		Filepath: br.Filepath,
		Authors:  br.Authors.MustGet(),
		Isbn10:   br.Isbn10,
		Isbn13:   br.Isbn13,
		Uom:      br.Uom,
		LowYear:  br.LowYear,
		HighYear: br.HighYear,
	}

	if br.Title.IsPresent() {
		bk.Title = br.Title.MustGet()
	}

	return bk
}

func ChooseBestResult(results []BookResult) (*BookResult, error) {
	if len(results) == 0 {
		return nil, fmt.Errorf("no results")
	}

	highestConfidence := 0.0
	var bestBook *BookResult = nil

	for _, br := range results {
		confidence := br.Confidence
		if math.IsNaN(confidence) {
			continue
		}

		if confidence > highestConfidence {
			highestConfidence = confidence
			bestBook = &br
		}
	}

	if bestBook == nil {
		return nil, fmt.Errorf("none of the results had a confidence %v", results)
	}

	return bestBook, nil
}
