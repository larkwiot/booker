package util

import (
	"github.com/larkwiot/booker/internal/book"
	"github.com/samber/lo"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
)

const Isbn10Pattern = "([0-9\\-\\s]+[0-9Xx])"
const Isbn13Pattern = "([0-9\\-\\s]+[0-9])"

func identifyIsbns[I any](text string, pattern string, maker func(string) I) []I {
	ws := regexp.MustCompile("[\\s\\-]+")
	identifier := regexp.MustCompile(pattern)
	occurrences := identifier.FindAllString(text, -1)
	return lo.FilterMap(occurrences, func(occ string, _ int) (I, bool) {
		clean := string(ws.ReplaceAll([]byte(occ), []byte("")))
		if book.IsIsbnCandidate(clean) {
			isbn := maker(strings.ToUpper(clean))
			return isbn, true
		}
		return maker(""), false
	})
}

func IdentifyIsbn10s(text string) []book.ISBN10 {
	return lo.Filter(identifyIsbns(text, Isbn10Pattern, func(s string) book.ISBN10 {
		return book.ISBN10(s)
	}), func(isbn book.ISBN10, _ int) bool {
		return isbn.IsValid()
	})
}

func IdentifyIsbn13s(text string) []book.ISBN13 {
	return lo.Filter(identifyIsbns(text, Isbn13Pattern, func(s string) book.ISBN13 {
		return book.ISBN13(s)
	}), func(isbn book.ISBN13, _ int) bool {
		return isbn.IsValid()
	})
}

// https://en.wikipedia.org/wiki/Levenshtein_distance#Iterative_with_two_matrix_rows
func LevenshteinDistance(a, b string) int {
	m := len(a)
	n := len(b)

	previousDistances := make([]int, n)
	currentDistances := make([]int, n)

	for i := 0; i < n; i++ {
		previousDistances[i] = i
	}

	for i := 0; i < m-1; i++ {
		currentDistances[0] = i + 1

		for j := 0; j < n-1; j++ {
			deletionCost := previousDistances[j+1] + 1
			insertionCost := currentDistances[j] + 1
			var substitutionCost int
			if a[i] == b[j] {
				substitutionCost = previousDistances[j]
			} else {
				substitutionCost = previousDistances[j] + 1
			}

			currentDistances[j+1] = min(deletionCost, insertionCost, substitutionCost)
		}

		previousDistances = currentDistances
	}

	return previousDistances[n-1]
}

func RecursiveFileCount(directory string, shouldBeCounted func(string, fs.DirEntry) bool) (uint, error) {
	var count uint = 0
	err := filepath.WalkDir(directory, func(path string, d fs.DirEntry, err error) error {
		if shouldBeCounted(path, d) {
			count++
		}
		return nil
	})
	return count, err
}
