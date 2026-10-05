package domain

type ImportSummary struct {
	ID                               ID
	State                            ImportState
	Source                           Source
	Total, Done, Failed, NeedsReview int64
}
type CollectionSummary struct {
	ID    ID
	Title string
	Kind  DestinationCollectionKind
}
