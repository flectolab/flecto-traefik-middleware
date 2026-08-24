package types

const (
	DefaultLimit  = 20
	DefaultOffset = 0
)

type PaginationInput struct {
	Limit  *int `query:"limit"`
	Offset *int `query:"offset"`
	// Cursor is the opaque value a previous response returned as Next. When set it
	// replaces Offset: the server walks from that position instead of skipping rows,
	// which keeps the cost of a page independent of how deep it is. Treat it as
	// opaque, its content is an implementation detail of the server.
	Cursor *string `query:"cursor"`
}

func (p *PaginationInput) GetLimit() int {
	if p == nil || p.Limit == nil {
		return DefaultLimit
	}
	return *p.Limit
}

// GetCursor returns the cursor, empty when paginating by offset.
func (p *PaginationInput) GetCursor() string {
	if p == nil || p.Cursor == nil {
		return ""
	}
	return *p.Cursor
}

func (p *PaginationInput) GetOffset() int {
	if p == nil || p.Offset == nil {
		return DefaultOffset
	}
	return *p.Offset
}

type PaginatedResult[T any] struct {
	Items  []T
	Total  int
	Limit  int
	Offset int
}

func (p PaginatedResult[T]) HasMore() bool {
	return p.Offset+len(p.Items) < p.Total
}
