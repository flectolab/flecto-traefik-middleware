package types

import (
	"net/url"
	"strings"
)

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

// RedirectOptions: a nil option is not set and behaves like false.
type RedirectOptions struct {
	// Only meaningful for BASIC and BASIC_HOST.
	IgnoreQueryString   *bool `json:"ignoreQueryString,omitempty"`
	PreserveQueryString *bool `json:"preserveQueryString,omitempty"`
}

func (o RedirectOptions) IsIgnoreQueryString() bool {
	return o.IgnoreQueryString != nil && *o.IgnoreQueryString
}

func (o RedirectOptions) IsPreserveQueryString() bool {
	return o.PreserveQueryString != nil && *o.PreserveQueryString
}

// Equal tells a nil option apart from a false one: setting an option to false is
// a change.
func (o RedirectOptions) Equal(other RedirectOptions) bool {
	return equalBoolPtr(o.IgnoreQueryString, other.IgnoreQueryString) &&
		equalBoolPtr(o.PreserveQueryString, other.PreserveQueryString)
}

func equalBoolPtr(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

type Redirect struct {
	Type    RedirectType    `json:"type" gorm:"size:50"`
	Source  string          `json:"source" gorm:"size:600"`
	Target  string          `json:"target" gorm:"size:2048"`
	Status  RedirectStatus  `json:"status" gorm:"size:50"`
	Options RedirectOptions `json:"options" gorm:"type:longtext;serializer:json"`
}

// AppendQueryString inserts rawQuery before the fragment of target. When target
// already has a query string it is kept as is, and only the parameters of rawQuery
// whose key it does not have are appended, raw and in their order. If the target
// query cannot be decoded, rawQuery is appended whole.
func AppendQueryString(target, rawQuery string) string {
	if rawQuery == "" {
		return target
	}

	base, fragment, hasFragment := strings.Cut(target, "#")
	path, targetQuery, hasQuery := strings.Cut(base, "?")

	var result string
	switch {
	case !hasQuery || targetQuery == "":
		result = path + "?" + rawQuery
	default:
		result = path + "?" + appendMissingParams(targetQuery, rawQuery)
	}

	if hasFragment {
		result += "#" + fragment
	}
	return result
}

func appendMissingParams(targetQuery, rawQuery string) string {
	targetValues, err := url.ParseQuery(targetQuery)
	if err != nil {
		return targetQuery + "&" + rawQuery
	}

	result := targetQuery
	for _, param := range strings.Split(rawQuery, "&") {
		if param == "" {
			continue
		}
		key, _, _ := strings.Cut(param, "=")
		if decoded, errKey := url.QueryUnescape(key); errKey == nil {
			key = decoded
		}
		if _, exists := targetValues[key]; exists {
			continue
		}
		result += "&" + param
	}
	return result
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
	// Next is the cursor to pass as PaginationInput.Cursor to get the following
	// page. Empty means there is nothing more to fetch. Clients that paginate by
	// offset never receive it and can keep using HasMore.
	Next string `json:",omitempty"`
}

func (rl RedirectList) HasMore() bool {
	return rl.Offset+len(rl.Items) < rl.Total
}
