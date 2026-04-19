package apitypes

type PageRequest struct {
	Limit  int
	Offset int
}

type CursorPageRequest struct {
	Limit  int
	Cursor string
}

type PageInfo struct {
	Limit      int
	Offset     int
	Returned   int
	Total      int
	HasMore    bool
	NextOffset int
}

type CursorPageInfo struct {
	Limit      int
	Returned   int
	HasMore    bool
	NextCursor string
}

type Page[T any] struct {
	Items []T
	Page  PageInfo
}

type CursorPage[T any] struct {
	Items []T
	Page  CursorPageInfo
}
