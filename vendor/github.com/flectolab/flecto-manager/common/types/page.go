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
}

func (pl PageList) HasMore() bool {
	return pl.Offset+len(pl.Items) < pl.Total
}
