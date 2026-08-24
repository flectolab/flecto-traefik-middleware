package client

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/flectolab/flecto-manager/common/types"
)

const (
	// DefaultRedirectsLimit is how many redirects a listing request asks for when
	// Config.RedirectsLimit is not set. It matches the Manager's own default.
	DefaultRedirectsLimit = 500
	// DefaultPagesLimit is how many pages a listing request asks for when
	// Config.PagesLimit is not set.
	DefaultPagesLimit = 500
)

type HTTPConfig struct {
	Client                  HTTPClient
	HeaderAuthorizationName string
	TokenJWT                string
}

type Config struct {
	ManagerUrl    string
	NamespaceCode string
	ProjectCode   string

	AgentName string
	AgentType types.AgentType

	Http *HTTPConfig

	IntervalCheck time.Duration

	// RedirectsLimit is how many redirects each listing request asks for.
	// Zero or negative falls back to DefaultRedirectsLimit.
	RedirectsLimit int
	// PagesLimit is how many pages each listing request asks for.
	// Zero or negative falls back to DefaultPagesLimit.
	PagesLimit int
}

func NewDefaultConfig() *Config {
	name, _ := os.Hostname()
	return &Config{
		Http: &HTTPConfig{
			Client:                  http.DefaultClient,
			HeaderAuthorizationName: "Authorization",
		},
		AgentName:      name,
		IntervalCheck:  5 * time.Minute,
		RedirectsLimit: DefaultRedirectsLimit,
		PagesLimit:     DefaultPagesLimit,
	}
}

// GetRedirectsLimit returns the limit to send on redirect listing requests,
// falling back to DefaultRedirectsLimit rather than a limit the server would
// reject.
func (c *Config) GetRedirectsLimit() int {
	if c.RedirectsLimit <= 0 {
		return DefaultRedirectsLimit
	}
	return c.RedirectsLimit
}

// GetPagesLimit returns the limit to send on page listing requests, falling back
// to DefaultPagesLimit.
func (c *Config) GetPagesLimit() int {
	if c.PagesLimit <= 0 {
		return DefaultPagesLimit
	}
	return c.PagesLimit
}

func (c *Config) GetUrlApi() string {
	return fmt.Sprintf("%s/api", c.ManagerUrl)
}

func (c *Config) GetUrlApiProject() string {
	return fmt.Sprintf("%s/namespace/%s/project/%s", c.GetUrlApi(), c.NamespaceCode, c.ProjectCode)
}

func (c *Config) GetUrlApiVersion() string {
	return fmt.Sprintf("%s/version", c.GetUrlApiProject())
}

func (c *Config) GetUrlApiRedirects() string {
	return fmt.Sprintf("%s/redirects", c.GetUrlApiProject())
}

func (c *Config) GetUrlApiPages() string {
	return fmt.Sprintf("%s/pages", c.GetUrlApiProject())
}
func (c *Config) GetUrlApiAgents() string {
	return fmt.Sprintf("%s/agents", c.GetUrlApiProject())
}

func (c *Config) GetUrlApiAgentsHit(name string) string {
	return fmt.Sprintf("%s/%s/hit", c.GetUrlApiAgents(), name)
}
