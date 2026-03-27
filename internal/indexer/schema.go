package indexer

import (
	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
)

type IndexDocument struct {
	Account   string `json:"account"`
	Folder    string `json:"folder"`
	UID       uint32 `json:"uid"`
	MessageID string `json:"message_id"`
	From      string `json:"from"`
	To        string `json:"to"`
	CC        string `json:"cc"`
	Subject   string `json:"subject"`
	Body      string `json:"body"`
	Date      string `json:"date"`
	Flags     string `json:"flags"`
	Size      uint32 `json:"size"`
}

func buildIndexMapping() mapping.IndexMapping {
	textFieldMapping := bleve.NewTextFieldMapping()
	textFieldMapping.Analyzer = "standard"

	keywordFieldMapping := bleve.NewKeywordFieldMapping()

	dateFieldMapping := bleve.NewDateTimeFieldMapping()

	numericFieldMapping := bleve.NewNumericFieldMapping()

	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("account", keywordFieldMapping)
	docMapping.AddFieldMappingsAt("folder", keywordFieldMapping)
	docMapping.AddFieldMappingsAt("uid", numericFieldMapping)
	docMapping.AddFieldMappingsAt("message_id", keywordFieldMapping)
	docMapping.AddFieldMappingsAt("from", textFieldMapping)
	docMapping.AddFieldMappingsAt("to", textFieldMapping)
	docMapping.AddFieldMappingsAt("cc", textFieldMapping)
	docMapping.AddFieldMappingsAt("subject", textFieldMapping)
	docMapping.AddFieldMappingsAt("body", textFieldMapping)
	docMapping.AddFieldMappingsAt("date", dateFieldMapping)
	docMapping.AddFieldMappingsAt("flags", keywordFieldMapping)
	docMapping.AddFieldMappingsAt("size", numericFieldMapping)

	indexMapping := bleve.NewIndexMapping()
	indexMapping.DefaultMapping = docMapping
	indexMapping.DefaultAnalyzer = "standard"

	return indexMapping
}
