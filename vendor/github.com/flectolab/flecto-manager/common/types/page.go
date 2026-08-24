package types

type PageType string

const (
	PageTypeBasic     PageType = "BASIC"
	PageTypeBasicHost PageType = "BASIC_HOST"
)

type PageContentType string

const (
	PageContentTypeTextPlain PageContentType = "TEXT_PLAIN"
	PageContentTypeXML       PageContentType = "XML"
)

type Page struct {
	Type        PageType        `json:"type" gorm:"size:50"`
	Path        string          `json:"path" gorm:"size:600"`
	Content     string          `json:"content"`
	ContentType PageContentType `json:"contentType" gorm:"size:50"`
}

func (p Page) HTTPContentType() string {
	switch p.ContentType {
	case PageContentTypeTextPlain:
		return "text/plain"
	case PageContentTypeXML:
		return "application/xml"
	default:
		return "text/plain"
	}
}

type PageList struct {
	Items  []Page
	Total  int
	Limit  int
	Offset int
	// Next is the cursor to pass as PaginationInput.Cursor to get the following
	// page. Empty means there is nothing more to fetch. Clients that paginate by
	// offset never receive it and can keep using HasMore.
	Next string `json:",omitempty"`
}

func (pl PageList) HasMore() bool {
	return pl.Offset+len(pl.Items) < pl.Total
}
