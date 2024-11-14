package providers

import (
	"github.com/larkwiot/booker/internal/book"
)

type SearchTerms struct {
	Isbn10s  []book.ISBN10
	Isbn13s  []book.ISBN13
	Filepath string
}

func (s *SearchTerms) HasAnyTerms() bool {
	return len(s.Isbn10s) > 0 || len(s.Isbn13s) > 0
}

type Provider interface {
	Name() string
	GetBookMetadata(search *SearchTerms) ([]book.BookResult, error)
	ClearCache()
	Shutdown()
	Disabled() bool
}
