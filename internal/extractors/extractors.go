package extractors

import "github.com/larkwiot/booker/internal/book"

type Extractor interface {
	Name() string
	ExtractText(bk *book.Book, maxCharacters uint) (string, error)
	Shutdown()
}
