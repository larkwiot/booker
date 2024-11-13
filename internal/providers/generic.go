package providers

import (
	"fmt"
	"github.com/larkwiot/booker/internal/book"
	"github.com/samber/lo"
	"slices"
	"sync"
	"time"
)

type GenericImpl interface {
	Name() string
	FindResult(isbn book.ISBN, filePath string) (book.BookResult, error)
	Shutdown()
}

type Generic struct {
	GenericImpl

	cache       sync.Map
	priority    int
	rateLimiter <-chan time.Time
	disabled    bool
}

func NewGeneric(impl GenericImpl, priority int, millisecondsPerRequest uint) Provider {
	g := &Generic{
		GenericImpl: impl,
		priority:    priority,
		rateLimiter: time.Tick(time.Duration(millisecondsPerRequest) * time.Millisecond),
		cache:       sync.Map{},
		disabled:    false,
	}

	return g
}

func (g *Generic) Priority() int {
	return g.priority
}

func (g *Generic) findResult(isbn book.ISBN, filePath string) (book.BookResult, error) {
	if cachedResult, cached := g.cache.Load(isbn); cached {
		return cachedResult.(book.BookResult), nil
	}

	if g.disabled {
		return book.BookResult{}, fmt.Errorf("%s provider self-disabled, probably due to rate limit", g.Name())
	}

	<-g.rateLimiter

	result, err := g.FindResult(isbn, filePath)
	if err != nil {
		g.cache.Store(isbn, result)
	}
	return result, err
}

func (g *Generic) GetBookMetadata(search *SearchTerms) ([]book.BookResult, error) {
	results := make([]book.BookResult, 0)

	isbn10s := lo.Map(search.Isbn10s, func(isbn book.ISBN10, _ int) book.ISBN {
		return book.ISBN(isbn)
	})

	isbn13s := lo.Map(search.Isbn13s, func(isbn book.ISBN13, _ int) book.ISBN {
		return book.ISBN(isbn)
	})

	allIsbns := slices.Concat(isbn10s, isbn13s)

	for _, isbn := range allIsbns {
		result, err := g.findResult(isbn, search.Filepath)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}

	return results, nil
}

func (g *Generic) ClearCache() {
	g.cache = sync.Map{}
}
