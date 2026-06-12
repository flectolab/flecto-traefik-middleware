package flecto_traefik_middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/flectolab/go-client"
)

// logWriter is where the plugin writes its diagnostics. Defaults to stderr;
// overridable in tests to capture output.
var logWriter io.Writer = os.Stderr

type Middleware struct {
	name          string
	next          http.Handler
	defaultClient client.Client
	hostClients   map[string]client.Client
	debug         bool
}

// clientFactory allows overriding client creation in tests
var clientFactory = func(cfg *client.Config) client.Client {
	return client.New(cfg)
}

// cachedMiddleware holds the whole set of clients for a single middleware name,
// together with a signature of the config that produced it, the round it was
// last seen in, and the cancel functions of their reload tickers.
type cachedMiddleware struct {
	signature     string
	round         int
	defaultClient client.Client
	hostClients   map[string]client.Client
	cancels       []context.CancelFunc
}

// middlewareCache shares clients across all middleware instances (routers) by
// middleware name.
//
// Traefik calls New() once per router that references a middleware, and rebuilds
// the whole router tree on any config change (and repeatedly on its own, e.g.
// on ACME events). Previously each call built its own clients with their own
// tickers, and a global cancel-by-name killed the previous instance's tickers -
// so when several routers shared a middleware only the last one kept a live
// ticker; the others froze on their startup state until restart.
//
// The middleware name is provider-qualified (e.g. "flecto@file"), globally
// unique, and has a 1->1 relation with its config. So it is used directly as
// the cache key: all routers sharing a name share one client set + tickers, and
// ANY config change (token, project, interval...) is a wholesale REPLACE of that
// set - no orphan. The only client that can leak is one whose name disappears
// (middleware removed/renamed), which Traefik gives no signal for; a restart
// clears it.
var (
	middlewareCache   = make(map[string]*cachedMiddleware)
	middlewareCacheMu sync.Mutex

	// currentRound and lastNewAt are guarded by middlewareCacheMu. A New() call
	// arriving after >= idleDelay of inactivity starts a new round; entries not
	// re-tagged with the current round (their middleware name disappeared) are
	// swept once things settle. See the middlewareCache comment.
	currentRound int
	lastNewAt    time.Time
	gcOnce       sync.Once
)

var (
	// idleDelay: a New() after this much inactivity starts a new round, and the
	// GC only sweeps after this much inactivity (so a rebuild is never in
	// flight). Overridable via FLECTO_REDIRECT_IDLE_DELAY (e.g. "30s", "10m").
	idleDelay = envDuration("FLECTO_REDIRECT_IDLE_DELAY", 5*time.Minute)
	// debugEnabled (FLECTO_REDIRECT_DEBUG=1|true) logs the cache state on every
	// GC tick plus round bumps and removals, to observe obsolete client cleanup.
	debugEnabled = envBool("FLECTO_REDIRECT_DEBUG")

	gcTick  = time.Minute // how often the GC wakes up to check
	nowFunc = time.Now    // injectable in tests
)

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return def
}

func envBool(key string) bool {
	v := os.Getenv(key)
	return v == "1" || strings.EqualFold(v, "true")
}

func reloadClient(name, key string, c client.Client) func() {
	return func() {
		err := c.Reload()
		if err != nil {
			_, _ = fmt.Fprintf(logWriter, "%s: Failed to reload client for %s: %s\n", name, key, strings.TrimSpace(err.Error()))
		}
	}
}

// settingsKey generates a unique key based on the client settings. Used to
// deduplicate clients with identical settings within one middleware, and for
// log messages.
func settingsKey(settings ClientSettings) string {
	return settings.ManagerUrl + "|" + settings.NamespaceCode + "|" + settings.ProjectCode
}

// configSignature is a deterministic JSON representation of the config, used to
// detect whether a middleware's config changed between two New() calls.
func configSignature(config *Config) (string, error) {
	b, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func startTicker(ctx context.Context, interval time.Duration, work func()) {
	ticker := time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-ticker.C:
				work()
			case <-ctx.Done():
				ticker.Stop()
				return
			}
		}
	}()
}

// buildClients creates the full set of clients for a middleware (default +
// one per host config), deduplicating clients with identical settings. Each
// client's initial load runs asynchronously so this never blocks on the network
// (it is called while holding middlewareCacheMu) - the reload ticker keeps it
// fresh afterwards. On error, any ticker already started is stopped.
func buildClients(name string, config *Config) (*cachedMiddleware, error) {
	cm := &cachedMiddleware{hostClients: make(map[string]client.Client)}
	local := make(map[string]client.Client) // dedup by settingsKey within this build

	create := func(settings ClientSettings) (client.Client, error) {
		key := settingsKey(settings)
		if c, ok := local[key]; ok {
			return c, nil
		}
		clientCfg, err := transformSettings(name, settings)
		if err != nil {
			return nil, err
		}
		c := clientFactory(clientCfg)

		// Load asynchronously: never block New() (and the cache lock) on the
		// network. The ticker keeps the client fresh afterwards.
		go func() {
			if err := c.Init(); err != nil {
				_, _ = fmt.Fprintf(logWriter, "%s: Failed to initialize client for %s: %s\n", name, key, strings.TrimSpace(err.Error()))
			}
		}()

		ctx, cancel := context.WithCancel(context.Background())
		cm.cancels = append(cm.cancels, cancel)
		startTicker(ctx, clientCfg.IntervalCheck, reloadClient(name, key, c))

		local[key] = c
		return c, nil
	}

	// Create default client from base config settings only if ProjectCode is set
	if config.ProjectCode != "" {
		defaultClient, err := create(config.ClientSettings)
		if err != nil {
			cm.stop()
			return nil, err
		}
		cm.defaultClient = defaultClient
	}

	// Create clients for each host config
	for _, hc := range config.HostConfigs {
		mergedSettings := mergeSettings(config.ClientSettings, hc.ClientSettings)
		hostClient, err := create(mergedSettings)
		if err != nil {
			cm.stop()
			return nil, err
		}
		for _, host := range hc.Hosts {
			cm.hostClients[host] = hostClient
		}
	}

	return cm, nil
}

// stop cancels all reload tickers of this client set.
func (cm *cachedMiddleware) stop() {
	for _, cancel := range cm.cancels {
		cancel()
	}
}

func New(_ context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	if err := validateConfig(config); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}

	signature, err := configSignature(config)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}

	m := &Middleware{
		name:        name,
		next:        next,
		hostClients: make(map[string]client.Client),
		debug:       config.Debug,
	}

	middlewareCacheMu.Lock()
	defer middlewareCacheMu.Unlock()

	// A New() after >= idleDelay of inactivity starts a new round (a fresh
	// rebuild wave). All New() calls of one rebuild arrive back-to-back, so
	// they share a round; the next wave, after a gap, bumps it.
	now := nowFunc()
	var idle time.Duration
	if !lastNewAt.IsZero() {
		idle = now.Sub(lastNewAt)
	}
	if lastNewAt.IsZero() || idle >= idleDelay {
		currentRound++
		if debugEnabled {
			_, _ = fmt.Fprintf(logWriter, "flecto[debug]: new round %d (idle %s) on New(%q)\n", currentRound, idle.Round(time.Second), name)
		}
	}
	lastNewAt = now

	cached, ok := middlewareCache[name]
	if !ok || cached.signature != signature {
		// Build the new set BEFORE tearing down the old one, so a build error
		// leaves the existing clients untouched.
		built, err := buildClients(name, config)
		if err != nil {
			return nil, err
		}
		built.signature = signature
		if ok {
			cached.stop() // config changed: stop the previous set's tickers
		}
		middlewareCache[name] = built
		cached = built
	}
	cached.round = currentRound // tag as seen in the current round (on reuse and create)

	m.defaultClient = cached.defaultClient
	m.hostClients = cached.hostClients

	gcOnce.Do(func() { go gcLoop() })
	return m, nil
}

// gcLoop periodically removes client sets whose middleware name has disappeared
// (removed/renamed) - the only orphan case the per-name cache cannot handle on
// its own, since Traefik gives no teardown signal.
func gcLoop() {
	for {
		time.Sleep(gcTick)
		if debugEnabled {
			_, _ = fmt.Fprintf(logWriter, "flecto[debug]: gc tick (interval %s)\n", gcTick)
		}
		sweepOrphans()
	}
}

func sweepOrphans() {
	now := nowFunc()
	middlewareCacheMu.Lock()
	defer middlewareCacheMu.Unlock()

	var idle time.Duration
	if !lastNewAt.IsZero() {
		idle = now.Sub(lastNewAt)
	}

	if debugEnabled {
		debugLogSnapshot(idle)
	}

	// Only sweep once things have settled: no New() for idleDelay means no
	// rebuild is in flight, so every live name has been re-tagged with the
	// current round. Entries left behind (older round) are genuine orphans.
	if lastNewAt.IsZero() || idle < idleDelay {
		return
	}
	for name, cm := range middlewareCache {
		if cm.round < currentRound {
			cm.stop()
			delete(middlewareCache, name)
			_, _ = fmt.Fprintf(logWriter, "flecto: removed obsolete client set for %q (round %d < %d)\n", name, cm.round, currentRound)
		}
	}
}

// debugLogSnapshot logs the current cache state. Caller holds middlewareCacheMu.
func debugLogSnapshot(idle time.Duration) {
	msg := fmt.Sprintf("flecto[debug]: round=%d idle=%s entries=%d\n", currentRound, idle.Round(time.Second), len(middlewareCache))
	for name, cm := range middlewareCache {
		stateVersion := -1
		if cm.defaultClient != nil {
			stateVersion = cm.defaultClient.GetStateVersion()
		}
		msg += fmt.Sprintf("  - %q round=%d hosts=%d default=%t stateVersion=%d\n", name, cm.round, len(cm.hostClients), cm.defaultClient != nil, stateVersion)
	}
	_, _ = io.WriteString(logWriter, msg)
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
