package apitypes

type PageRequest struct {
	Limit  int
	Offset int
}

type PageInfo struct {
	Limit      int
	Offset     int
	Returned   int
	Total      int
	HasMore    bool
	NextOffset int
}

type Page[T any] struct {
	Items []T
	Page  PageInfo
}
