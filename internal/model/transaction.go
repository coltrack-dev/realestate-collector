package model

import "time"

// PropertyTransaction is a registered transaction, not a portal listing.
// Its value must never be interpreted as an asking/listing price.
type PropertyTransaction struct {
	Source, ExternalID string

	Matricula              string
	OldCadastralNumber     string
	CurrentCadastralNumber string
	Municipality           string
	Department             string
	Divipola               string
	ORIP                   string

	FilingDateText  string
	OpeningDateText string
	Year            string
	PropertyZone    string
	Rurality        string

	AnnotationNumber string
	FolioStatus      string
	DerivedFolios    string
	NewProperties    *int
	HasMultipleValue *bool

	Value              *int64
	HasValue           *bool
	CountA             *int
	CountDE            *int
	JustifyingDocument string
	LegalActCode       string
	LegalActName       string
	Dynamic            *int
	CollectedAt        time.Time
}
