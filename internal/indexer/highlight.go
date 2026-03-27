package indexer

import (
	"fmt"

	"github.com/blevesearch/bleve/v2/registry"
	"github.com/blevesearch/bleve/v2/search/highlight"
	plainFormatter "github.com/blevesearch/bleve/v2/search/highlight/format/plain"
	simpleFragmenter "github.com/blevesearch/bleve/v2/search/highlight/fragmenter/simple"
	simpleHighlighter "github.com/blevesearch/bleve/v2/search/highlight/highlighter/simple"
)

const markdownHighlighterName = "markdown"

func init() {
	err := registry.RegisterFragmentFormatter("markdown", func(config map[string]interface{}, cache *registry.Cache) (highlight.FragmentFormatter, error) {
		return plainFormatter.NewFragmentFormatter("**", "**"), nil
	})
	if err != nil {
		panic(err)
	}

	err = registry.RegisterHighlighter(markdownHighlighterName, func(config map[string]interface{}, cache *registry.Cache) (highlight.Highlighter, error) {
		fragmenter, err := cache.FragmenterNamed(simpleFragmenter.Name)
		if err != nil {
			return nil, fmt.Errorf("error building fragmenter: %v", err)
		}
		formatter, err := cache.FragmentFormatterNamed("markdown")
		if err != nil {
			return nil, fmt.Errorf("error building fragment formatter: %v", err)
		}
		return simpleHighlighter.NewHighlighter(fragmenter, formatter, simpleHighlighter.DefaultSeparator), nil
	})
	if err != nil {
		panic(err)
	}
}
