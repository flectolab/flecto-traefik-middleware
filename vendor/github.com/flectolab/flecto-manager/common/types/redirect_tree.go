package types

import (
	"regexp"
	"regexp/syntax"
	"sort"
	"strings"

	"github.com/armon/go-radix"
)

type compiledRedirect struct {
	*Redirect
	regex *regexp.Regexp
}

type regexBucket struct {
	redirects []*compiledRedirect
}

type RedirectTreeMatcher interface {
	Insert(r *Redirect) error
	Match(host, uri string) (*Redirect, string)
}

type RedirectTree struct {
	basicHost *radix.Tree
	basic     *radix.Tree

	regexHost     *radix.Tree
	regex         *radix.Tree
	regexHostRoot []*compiledRedirect
	regexRoot     []*compiledRedirect
}

func NewRedirectTreeMatcher() RedirectTreeMatcher {
	return &RedirectTree{
		basicHost:     radix.New(),
		basic:         radix.New(),
		regexHost:     radix.New(),
		regex:         radix.New(),
		regexHostRoot: make([]*compiledRedirect, 0),
		regexRoot:     make([]*compiledRedirect, 0),
	}
}

func (rt *RedirectTree) Insert(r *Redirect) error {
	switch r.Type {
	case RedirectTypeBasicHost:
		rt.basicHost.Insert(r.Source, &compiledRedirect{Redirect: r})

	case RedirectTypeBasic:
		rt.basic.Insert(r.Source, &compiledRedirect{Redirect: r})

	case RedirectTypeRegexHost, RedirectTypeRegex:
		re, err := regexp.Compile(r.Source)
		if err != nil {
			return err
		}

		cr := &compiledRedirect{Redirect: r, regex: re}
		prefix := extractRegexPrefix(r.Source)
		tree := rt.regex
		rootBucket := &rt.regexRoot

		if r.Type == RedirectTypeRegexHost {
			tree = rt.regexHost
			rootBucket = &rt.regexHostRoot
		}

		if prefix == "" {
			*rootBucket = append(*rootBucket, cr)
		} else {
			if val, found := tree.Get(prefix); found {
				bucket := val.(*regexBucket)
				bucket.redirects = append(bucket.redirects, cr)
			} else {
				tree.Insert(prefix, &regexBucket{redirects: []*compiledRedirect{cr}})
			}
		}
	}
	return nil
}

func (rt *RedirectTree) Match(host, uri string) (*Redirect, string) {
	hostURI := host + uri

	if val, found := rt.basicHost.Get(hostURI); found {
		cr := val.(*compiledRedirect)
		return cr.Redirect, cr.Target
	}

	if val, found := rt.basic.Get(uri); found {
		cr := val.(*compiledRedirect)
		return cr.Redirect, cr.Target
	}

	if r, target := rt.matchRegex(rt.regexHost, rt.regexHostRoot, hostURI); r != nil {
		return r, target
	}

	if r, target := rt.matchRegex(rt.regex, rt.regexRoot, uri); r != nil {
		return r, target
	}

	return nil, ""
}

func (rt *RedirectTree) matchRegex(tree *radix.Tree, rootBucket []*compiledRedirect, input string) (*Redirect, string) {
	var candidates []*compiledRedirect

	tree.WalkPrefix(input[:minInt(len(input), 1)], func(prefix string, val interface{}) bool {
		if strings.HasPrefix(input, prefix) {
			bucket := val.(*regexBucket)
			candidates = append(candidates, bucket.redirects...)
		}
		return false
	})

	candidates = append(candidates, rootBucket...)

	sortBySourceLength(candidates)

	for _, cr := range candidates {
		if matches := cr.regex.FindStringSubmatch(input); matches != nil {
			target := resolveTarget(cr.Target, matches)
			return cr.Redirect, target
		}
	}

	return nil, ""
}
func resolveTarget(target string, matches []string) string {
	result := target
	for i := len(matches) - 1; i >= 1; i-- {
		placeholder := "$" + string(rune('0'+i))
		result = strings.ReplaceAll(result, placeholder, matches[i])
	}
	return result
}

func extractRegexPrefix(pattern string) string {
	pattern = strings.TrimPrefix(pattern, "^")

	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return ""
	}

	return extractLiteralPrefix(re)
}

func extractLiteralPrefix(re *syntax.Regexp) string {
	switch re.Op {
	case syntax.OpLiteral:
		return string(re.Rune)

	case syntax.OpConcat:
		var prefix strings.Builder
		for _, sub := range re.Sub {
			if sub.Op == syntax.OpLiteral {
				prefix.WriteString(string(sub.Rune))
			} else if sub.Op == syntax.OpCapture && len(sub.Sub) > 0 {
				inner := extractLiteralPrefix(sub.Sub[0])
				if inner == "" {
					break
				}
				prefix.WriteString(inner)
			} else {
				break
			}
		}
		return prefix.String()

	case syntax.OpCapture:
		if len(re.Sub) > 0 {
			return extractLiteralPrefix(re.Sub[0])
		}
	}

	return ""
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func sortBySourceLength(candidates []*compiledRedirect) {
	sort.Slice(candidates, func(i, j int) bool {
		return len(candidates[i].Source) > len(candidates[j].Source)
	})
}
