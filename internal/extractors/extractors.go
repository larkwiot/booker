package extractors

import (
	"github.com/larkwiot/booker/internal/book"
	"github.com/larkwiot/booker/internal/service"
)

type Extractor interface {
	service.Service
	Name() string
	ExtractText(bk *book.Book, maxCharacters uint) (string, error)
	Shutdown()
}
