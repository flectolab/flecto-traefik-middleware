package types

type RedirectType string

const (
	RedirectTypeBasic     RedirectType = "BASIC"
	RedirectTypeBasicHost RedirectType = "BASIC_HOST"
	RedirectTypeRegex     RedirectType = "REGEX"
	RedirectTypeRegexHost RedirectType = "REGEX_HOST"
)

type RedirectStatus string

const (
	RedirectStatusMovedPermanent RedirectStatus = "MOVED_PERMANENT"
	RedirectStatusFound          RedirectStatus = "FOUND"
	RedirectStatusTemporary      RedirectStatus = "TEMPORARY_REDIRECT"
	RedirectStatusPermanent      RedirectStatus = "PERMANENT_REDIRECT"
)

type Redirect struct {
	Type   RedirectType   `json:"type"`
	Source string         `json:"source"`
	Target string         `json:"target"`
	Status RedirectStatus `json:"status"`
}

func (r Redirect) HTTPCode() int {
	switch r.Status {
	case RedirectStatusMovedPermanent:
		return 301
	case RedirectStatusFound:
		return 302
	case RedirectStatusTemporary:
		return 307
	case RedirectStatusPermanent:
		return 308
	default:
		return 302
	}
}

type RedirectList struct {
	Items  []Redirect
	Total  int
	Limit  int
	Offset int
}

func (rl RedirectList) HasMore() bool {
	return rl.Offset+len(rl.Items) < rl.Total
}
