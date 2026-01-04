package types

const (
	DefaultLimit  = 20
	DefaultOffset = 0
)

type PaginationInput struct {
	Limit  *int `query:"limit"`
	Offset *int `query:"offset"`
}

func (p *PaginationInput) GetLimit() int {
	if p == nil || p.Limit == nil {
		return DefaultLimit
	}
	return *p.Limit
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
