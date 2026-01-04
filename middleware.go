package flecto_traefik_middleware

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/flectolab/go-client"
)

type Middleware struct {
	name          string
	next          http.Handler
	defaultClient client.Client
	hostClients   map[string]client.Client
	tickers       []chan bool
	debug         bool
}

// clientFactory allows overriding client creation in tests
var clientFactory = func(cfg *client.Config) client.Client {
	return client.New(cfg)
}

func reloadClient(name, key string, c client.Client) func() {
	return func() {
		err := c.Reload()
		if err != nil {
			_, _ = os.Stderr.WriteString(fmt.Sprintf("%s: Failed to reload client for %s: %v", name, key, err))
		}
	}
}

// settingsKey generates a unique key based on the client settings
func settingsKey(settings ClientSettings) string {
	return settings.ManagerUrl + "|" + settings.NamespaceCode + "|" + settings.ProjectCode
}

func startTicker(interval time.Duration, work func()) chan bool {
	ticker := time.NewTicker(interval)
	stop := make(chan bool, 1)
	go func() {
		for {
			select {
			case <-ticker.C:
				work()
			case <-stop:
				ticker.Stop()
				return
			}
		}
	}()
	return stop
}

// createClient creates a new client and starts its reload ticker.
// Init errors are ignored to avoid blocking middleware startup - the ticker will retry via Reload.
func (m *Middleware) createClient(settings ClientSettings) (client.Client, error) {
	key := settingsKey(settings)
	clientCfg, err := transformSettings(m.name, settings)
	if err != nil {
		return nil, err
	}
	c := clientFactory(clientCfg)
	// Ignore Init error to avoid blocking middleware startup
	// The ticker will retry via Reload
	err = c.Init()
	if err != nil {
		_, _ = os.Stderr.WriteString(fmt.Sprintf("%s: Failed to initialize client for %s: %v\n", m.name, key, err))
	}
	ticker := startTicker(clientCfg.IntervalCheck, reloadClient(m.name, key, c))
	m.tickers = append(m.tickers, ticker)

	return c, nil
}

func New(_ context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	if err := validateConfig(config); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}

	m := &Middleware{
		name:        name,
		next:        next,
		hostClients: make(map[string]client.Client),
		tickers:     make([]chan bool, 0),
		debug:       config.Debug,
	}

	// Local cache to reuse clients with same settings within this middleware
	localClients := make(map[string]client.Client)

	// Create default client from base config settings only if ProjectCode is set
	if config.ProjectCode != "" {
		key := settingsKey(config.ClientSettings)
		defaultClient, err := m.createClient(config.ClientSettings)
		if err != nil {
			return nil, err
		}
		m.defaultClient = defaultClient
		localClients[key] = defaultClient
	}

	// Create clients for each host config
	for _, hc := range config.HostConfigs {
		mergedSettings := mergeSettings(config.ClientSettings, hc.ClientSettings)
		key := settingsKey(mergedSettings)

		// Reuse client if same settings already created for this middleware
		hostClient, exists := localClients[key]
		if !exists {
			var err error
			hostClient, err = m.createClient(mergedSettings)
			if err != nil {
				return nil, err
			}
			localClients[key] = hostClient
		}

		for _, host := range hc.Hosts {
			m.hostClients[host] = hostClient
		}
	}

	return m, nil
}

func (m *Middleware) clientForHost(host string) client.Client {
	// Remove port if present (example.com:443 -> example.com)
	h := strings.Split(host, ":")[0]
	if c, ok := m.hostClients[h]; ok {
		return c
	}
	return m.defaultClient
}

func (m *Middleware) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	c := m.clientForHost(req.Host)

	// No client for this host, skip to next handler
	if c == nil {
		m.next.ServeHTTP(rw, req)
		return
	}

	if m.debug {
		rw.Header().Add("X-Middleware-Flecto-Version", fmt.Sprintf("%d", c.GetStateVersion()))
		rw.Header().Add("X-Middleware-Flecto-Url", fmt.Sprintf("%s%s", req.Host, req.URL.RequestURI()))
	}
	redirect, target := c.RedirectMatch(req.Host, req.URL.RequestURI())
	if redirect != nil {
		if m.debug {
			rw.Header().Add("X-Middleware-Flecto-Redirect", fmt.Sprintf("%v", redirect))
		}
		http.Redirect(rw, req, target, redirect.HTTPCode())
		return
	}
	page := c.PageMatch(req.Host, req.URL.RequestURI())
	if page != nil {
		rw.Header().Add("Content-Type", page.HTTPContentType())
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte(page.Content))
		return
	}
	m.next.ServeHTTP(rw, req)
}
