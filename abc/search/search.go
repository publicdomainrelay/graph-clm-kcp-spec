package search

import (
	"context"
	"strings"
	"unicode"
)

const DefaultLimit = 10

type Document struct {
	ID int64

	Label string

	Context string

	Text string
}

type Hit struct {
	ID int64

	Label string

	Context string

	Text string

	Score float64
}

type Index interface {
	Put(ctx context.Context, documents []Document) error

	Search(ctx context.Context, terms string, limit int) ([]Hit, error)

	Rebuild(ctx context.Context, documents []Document) error
}

func Terms(query string) []string {
	words := []string{}
	seen := map[string]bool{}
	for _, field := range strings.FieldsFunc(query, func(char rune) bool {
		return !unicode.IsLetter(char) && !unicode.IsDigit(char) && char != '_'
	}) {
		if field == "" || seen[field] {
			continue
		}
		seen[field] = true
		words = append(words, field)
	}
	return words
}

func Query(query string) string {
	words := Terms(query)
	quoted := make([]string, 0, len(words))
	for _, word := range words {
		quoted = append(quoted, `"`+word+`"`)
	}
	return strings.Join(quoted, " OR ")
}
