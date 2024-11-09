package book_test

import (
	"booker/internal/book"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestIsbnCandidacy(t *testing.T) {
	assert.True(t, book.IsIsbnCandidate("9781718501263"))
	assert.True(t, book.IsIsbnCandidate("9781718501270"))
	assert.True(t, book.IsIsbnCandidate("1718501269"))

	assert.False(t, book.IsIsbnCandidate("123"))
	assert.False(t, book.IsIsbnCandidate("11111111111"))
}

func TestIsbn10Validity(t *testing.T) {
	isbn := book.ISBN10("1718501269")
	assert.True(t, isbn.IsValid())
}

func TestIsbn13Validity(t *testing.T) {
	isbn := book.ISBN13("9781718501263")
	assert.True(t, isbn.IsValid())
	isbn = book.ISBN13("9781718501270")
	assert.True(t, isbn.IsValid())

	isbn = book.ISBN13("1234567891123")
	assert.False(t, isbn.IsValid())
}
