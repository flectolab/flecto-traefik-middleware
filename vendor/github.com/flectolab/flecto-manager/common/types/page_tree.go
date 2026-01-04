package types

import (
	"github.com/armon/go-radix"
)

type PageTreeMatcher interface {
	Insert(p *Page)
	Match(host, uri string) *Page
}

type PageTree struct {
	basicHost *radix.Tree
	basic     *radix.Tree
}

func NewPageTreeMatcher() PageTreeMatcher {
	return &PageTree{
		basicHost: radix.New(),
		basic:     radix.New(),
	}
}

func (pt *PageTree) Insert(p *Page) {
	switch p.Type {
	case PageTypeBasicHost:
		pt.basicHost.Insert(p.Path, p)
	case PageTypeBasic:
		pt.basic.Insert(p.Path, p)
	}
}

func (pt *PageTree) Match(host, uri string) *Page {
	hostURI := host + uri

	if val, found := pt.basicHost.Get(hostURI); found {
		return val.(*Page)
	}

	if val, found := pt.basic.Get(uri); found {
		return val.(*Page)
	}

	return nil
}