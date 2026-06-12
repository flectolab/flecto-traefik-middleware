package flecto_traefik_middleware

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flectolab/flecto-manager/common/types"
	"github.com/flectolab/go-client"
	"github.com/stretchr/testify/assert"
)

// resetMiddlewareCache clears the global per-name client cache (and stops its
// tickers) so tests that count clientFactory calls or assert client identity
// are not affected by clients cached by previous tests.
func resetMiddlewareCache() {
	middlewareCacheMu.Lock()
	defer middlewareCacheMu.Unlock()
	for name, cm := range middlewareCache {
		cm.stop()
		delete(middlewareCache, name)
	}
	currentRound = 0
	lastNewAt = time.Time{}
}

// mockClient implements client.Client interface for testing
type mockClient struct {
	initErr       error
	reloadErr     error
	reloadCalled  bool
	redirectMatch func(hostname, uri string) (*types.Redirect, string)
	pageMatch     func(hostname, uri string) *types.Page
}

func (m *mockClient) Init() error {
	return m.initErr
}

func (m *mockClient) Start(ctx context.Context) {}

func (m *mockClient) Reload() error {
	m.reloadCalled = true
	return m.reloadErr
}

func (m *mockClient) GetStateVersion() int {
	return 0
}

func (m *mockClient) RedirectMatch(hostname, uri string) (*types.Redirect, string) {
	if m.redirectMatch != nil {
		return m.redirectMatch(hostname, uri)
	}
	return nil, ""
}

func (m *mockClient) PageMatch(hostname, uri string) *types.Page {
	if m.pageMatch != nil {
		return m.pageMatch(hostname, uri)
	}
	return nil
}

func TestMiddleware_ServeHTTP(t *testing.T) {
	tests := []struct {
		name            string
		requestURL      string
		redirectMatch   func(hostname, uri string) (*types.Redirect, string)
		pageMatch       func(hostname, uri string) *types.Page
		wantStatusCode  int
		wantLocation    string
		wantContentType string
		wantBody        string
		wantNextCalled  bool
	}{
		{
			name:       "no redirect no page - passes to next handler",
			requestURL: "http://example.com/path",
			redirectMatch: func(hostname, uri string) (*types.Redirect, string) {
				return nil, ""
			},
			pageMatch: func(hostname, uri string) *types.Page {
				return nil
			},
			wantStatusCode: http.StatusOK,
			wantNextCalled: true,
		},
		{
			name:       "redirect 301 - moved permanently",
			requestURL: "http://example.com/old-path",
			redirectMatch: func(hostname, uri string) (*types.Redirect, string) {
				return &types.Redirect{
					Type:   types.RedirectTypeBasic,
					Source: "/old-path",
					Target: "/new-path",
					Status: types.RedirectStatusMovedPermanent,
				}, "/new-path"
			},
			wantStatusCode: http.StatusMovedPermanently,
			wantLocation:   "/new-path",
			wantNextCalled: false,
		},
		{
			name:       "redirect 302 - found",
			requestURL: "http://example.com/temp-path",
			redirectMatch: func(hostname, uri string) (*types.Redirect, string) {
				return &types.Redirect{
					Type:   types.RedirectTypeBasic,
					Source: "/temp-path",
					Target: "/destination",
					Status: types.RedirectStatusFound,
				}, "/destination"
			},
			wantStatusCode: http.StatusFound,
			wantLocation:   "/destination",
			wantNextCalled: false,
		},
		{
			name:       "redirect 307 - temporary redirect",
			requestURL: "http://example.com/api/resource",
			redirectMatch: func(hostname, uri string) (*types.Redirect, string) {
				return &types.Redirect{
					Type:   types.RedirectTypeBasic,
					Source: "/api/resource",
					Target: "/resource",
					Status: types.RedirectStatusTemporary,
				}, "/resource"
			},
			wantStatusCode: http.StatusTemporaryRedirect,
			wantLocation:   "/resource",
			wantNextCalled: false,
		},
		{
			name:       "redirect 308 - permanent redirect",
			requestURL: "http://example.com/permanent",
			redirectMatch: func(hostname, uri string) (*types.Redirect, string) {
				return &types.Redirect{
					Type:   types.RedirectTypeBasic,
					Source: "/permanent",
					Target: "/permanent",
					Status: types.RedirectStatusPermanent,
				}, "/permanent"
			},
			wantStatusCode: http.StatusPermanentRedirect,
			wantLocation:   "/permanent",
			wantNextCalled: false,
		},
		{
			name:       "page match - text/plain content",
			requestURL: "http://example.com/robots.txt",
			redirectMatch: func(hostname, uri string) (*types.Redirect, string) {
				return nil, ""
			},
			pageMatch: func(hostname, uri string) *types.Page {
				return &types.Page{
					Type:        types.PageTypeBasic,
					Path:        "/robots.txt",
					Content:     "User-agent: *\nDisallow: /admin",
					ContentType: types.PageContentTypeTextPlain,
				}
			},
			wantStatusCode:  http.StatusOK,
			wantContentType: "text/plain",
			wantBody:        "User-agent: *\nDisallow: /admin",
			wantNextCalled:  false,
		},
		{
			name:       "page match - XML content",
			requestURL: "http://example.com/sitemap.xml",
			redirectMatch: func(hostname, uri string) (*types.Redirect, string) {
				return nil, ""
			},
			pageMatch: func(hostname, uri string) *types.Page {
				return &types.Page{
					Type:        types.PageTypeBasic,
					Path:        "/sitemap.xml",
					Content:     "<?xml version=\"1.0\"?><urlset></urlset>",
					ContentType: types.PageContentTypeXML,
				}
			},
			wantStatusCode:  http.StatusOK,
			wantContentType: "application/xml",
			wantBody:        "<?xml version=\"1.0\"?><urlset></urlset>",
			wantNextCalled:  false,
		},
		{
			name:       "redirect takes priority over page",
			requestURL: "http://example.com/both",
			redirectMatch: func(hostname, uri string) (*types.Redirect, string) {
				return &types.Redirect{
					Type:   types.RedirectTypeBasic,
					Source: "/both",
					Target: "/redirected",
					Status: types.RedirectStatusFound,
				}, "/redirected"
			},
			pageMatch: func(hostname, uri string) *types.Page {
				return &types.Page{
					Type:        types.PageTypeBasic,
					Path:        "/both",
					Content:     "This should not be returned",
					ContentType: types.PageContentTypeTextPlain,
				}
			},
			wantStatusCode: http.StatusFound,
			wantLocation:   "/redirected",
			wantNextCalled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})

			mock := &mockClient{
				redirectMatch: tt.redirectMatch,
				pageMatch:     tt.pageMatch,
			}

			middleware := &Middleware{
				name:          "test",
				next:          next,
				debug:         true,
				defaultClient: mock,
				hostClients:   make(map[string]client.Client),
			}

			req := httptest.NewRequest(http.MethodGet, tt.requestURL, nil)
			rec := httptest.NewRecorder()

			middleware.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatusCode, rec.Code)
			assert.Equal(t, tt.wantNextCalled, nextCalled)

			if tt.wantLocation != "" {
				assert.Equal(t, tt.wantLocation, rec.Header().Get("Location"))
			}

			if tt.wantContentType != "" {
				assert.Equal(t, tt.wantContentType, rec.Header().Get("Content-Type"))
			}

			if tt.wantBody != "" {
				assert.Equal(t, tt.wantBody, rec.Body.String())
			}
		})
	}
}

func TestMiddleware_ServeHTTP_MultiHost(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	defaultMock := &mockClient{
		redirectMatch: func(hostname, uri string) (*types.Redirect, string) {
			return &types.Redirect{
				Type:   types.RedirectTypeBasic,
				Source: "/test",
				Target: "/default",
				Status: types.RedirectStatusFound,
			}, "/default"
		},
	}

	hostMock := &mockClient{
		redirectMatch: func(hostname, uri string) (*types.Redirect, string) {
			return &types.Redirect{
				Type:   types.RedirectTypeBasic,
				Source: "/test",
				Target: "/host-specific",
				Status: types.RedirectStatusFound,
			}, "/host-specific"
		},
	}

	middleware := &Middleware{
		name:          "test",
		next:          next,
		debug:         false,
		defaultClient: defaultMock,
		hostClients: map[string]client.Client{
			"example.com": hostMock,
			"example.fr":  hostMock,
		},
	}

	t.Run("uses host-specific client when host matches", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "http://example.com/test", nil)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusFound, rec.Code)
		assert.Equal(t, "/host-specific", rec.Header().Get("Location"))
	})

	t.Run("uses default client when host does not match", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "http://other.com/test", nil)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusFound, rec.Code)
		assert.Equal(t, "/default", rec.Header().Get("Location"))
	})

	t.Run("strips port from host", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "http://example.com:8080/test", nil)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusFound, rec.Code)
		assert.Equal(t, "/host-specific", rec.Header().Get("Location"))
	})

	_ = nextCalled
}

func TestNew(t *testing.T) {
	originalFactory := clientFactory
	defer func() { clientFactory = originalFactory }()

	tests := []struct {
		name        string
		config      *Config
		mockClient  *mockClient
		wantErr     bool
		errContains string
	}{
		{
			name: "returns error when no project_code and no host_configs",
			config: &Config{
				ClientSettings: ClientSettings{
					ManagerUrl:    "http://localhost:8080",
					NamespaceCode: "ns",
					ProjectCode:   "",
					TokenJWT:      "token",
				},
			},
			mockClient:  nil,
			wantErr:     true,
			errContains: "either project_code or host_configs must be configured",
		},
		{
			name: "succeeds even when client Init fails (non-blocking)",
			config: &Config{
				ClientSettings: ClientSettings{
					ManagerUrl:    "http://localhost:8080",
					NamespaceCode: "ns",
					ProjectCode:   "proj",
					TokenJWT:      "token",
				},
			},
			mockClient: &mockClient{
				initErr: errors.New("connection refused"),
			},
			wantErr:     false,
			errContains: "",
		},
		{
			name: "returns middleware when successful",
			config: &Config{
				ClientSettings: ClientSettings{
					ManagerUrl:    "http://localhost:8080",
					NamespaceCode: "ns",
					ProjectCode:   "proj",
					TokenJWT:      "token",
				},
			},
			mockClient:  &mockClient{},
			wantErr:     false,
			errContains: "",
		},
		{
			name: "returns error when host_configs has empty hosts",
			config: &Config{
				ClientSettings: ClientSettings{
					ManagerUrl:    "http://localhost:8080",
					NamespaceCode: "ns",
					ProjectCode:   "proj",
					TokenJWT:      "token",
				},
				HostConfigs: []HostConfig{
					{Hosts: []string{}, ClientSettings: ClientSettings{ProjectCode: "proj-x"}},
				},
			},
			mockClient:  &mockClient{},
			wantErr:     true,
			errContains: "hosts is required",
		},
		{
			name: "returns error when host_configs has no project_code",
			config: &Config{
				ClientSettings: ClientSettings{
					ManagerUrl:    "http://localhost:8080",
					NamespaceCode: "ns",
					ProjectCode:   "proj",
					TokenJWT:      "token",
				},
				HostConfigs: []HostConfig{
					{Hosts: []string{"example.com"}}, // missing project_code
				},
			},
			mockClient:  &mockClient{},
			wantErr:     true,
			errContains: "project_code is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetMiddlewareCache()
			if tt.mockClient != nil {
				clientFactory = func(cfg *client.Config) client.Client {
					return tt.mockClient
				}
			}

			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			ctx := context.Background()
			handler, err := New(ctx, next, tt.config, "test-middleware")

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, handler)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, handler)

				middleware, ok := handler.(*Middleware)
				assert.True(t, ok)
				assert.Equal(t, "test-middleware", middleware.name)
				assert.Equal(t, tt.mockClient, middleware.defaultClient)
			}
		})
	}
}

func TestNew_WithHostConfigs(t *testing.T) {
	originalFactory := clientFactory
	defer func() { clientFactory = originalFactory }()
	resetMiddlewareCache()

	createCount := 0
	clientFactory = func(cfg *client.Config) client.Client {
		createCount++
		return &mockClient{}
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	config := &Config{
		ClientSettings: ClientSettings{
			ManagerUrl:    "http://localhost:8080",
			NamespaceCode: "ns",
			ProjectCode:   "default-proj",
			TokenJWT:      "token",
		},
		HostConfigs: []HostConfig{
			{
				Hosts:          []string{"example.com", "example.fr"},
				ClientSettings: ClientSettings{ProjectCode: "proj-fr"},
			},
			{
				Hosts:          []string{"example.es"},
				ClientSettings: ClientSettings{ProjectCode: "proj-es"},
			},
		},
	}

	ctx := context.Background()
	handler, err := New(ctx, next, config, "test-middleware")

	assert.NoError(t, err)
	assert.NotNil(t, handler)
	// 3 clients: default + proj-fr + proj-es
	assert.Equal(t, 3, createCount)

	middleware := handler.(*Middleware)
	assert.NotNil(t, middleware.defaultClient)
	assert.Len(t, middleware.hostClients, 3) // example.com, example.fr, example.es
}

func TestNew_ReusesClientForSameSettings(t *testing.T) {
	originalFactory := clientFactory
	defer func() { clientFactory = originalFactory }()
	resetMiddlewareCache()

	createCount := 0
	clientFactory = func(cfg *client.Config) client.Client {
		createCount++
		return &mockClient{}
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Two host configs with same projectCode should share the same client
	config := &Config{
		ClientSettings: ClientSettings{
			ManagerUrl:    "http://localhost:8080",
			NamespaceCode: "ns",
			ProjectCode:   "default-proj",
			TokenJWT:      "token",
		},
		HostConfigs: []HostConfig{
			{
				Hosts:          []string{"example.com"},
				ClientSettings: ClientSettings{ProjectCode: "shared-proj"},
			},
			{
				Hosts:          []string{"example.fr"},
				ClientSettings: ClientSettings{ProjectCode: "shared-proj"}, // same as above
			},
		},
	}

	ctx := context.Background()
	handler, err := New(ctx, next, config, "test-middleware")

	assert.NoError(t, err)
	assert.NotNil(t, handler)
	// 2 clients: default + shared-proj (reused for both hosts)
	assert.Equal(t, 2, createCount)

	middleware := handler.(*Middleware)
	// Both hosts should share the same client
	assert.Same(t, middleware.hostClients["example.com"], middleware.hostClients["example.fr"])
}

func TestNew_HostConfigInitError_NonBlocking(t *testing.T) {
	originalFactory := clientFactory
	defer func() { clientFactory = originalFactory }()
	resetMiddlewareCache()

	callCount := 0
	clientFactory = func(cfg *client.Config) client.Client {
		callCount++
		if callCount == 2 {
			return &mockClient{initErr: errors.New("init failed for host config")}
		}
		return &mockClient{}
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	config := &Config{
		ClientSettings: ClientSettings{
			ManagerUrl:    "http://localhost:8080",
			NamespaceCode: "ns",
			ProjectCode:   "default-proj",
			TokenJWT:      "token",
		},
		HostConfigs: []HostConfig{
			{
				Hosts:          []string{"example.com"},
				ClientSettings: ClientSettings{ProjectCode: "proj-fail"},
			},
		},
	}

	ctx := context.Background()
	handler, err := New(ctx, next, config, "test-middleware")

	// Init errors are now ignored (non-blocking)
	assert.NoError(t, err)
	assert.NotNil(t, handler)
}
func TestNew_TransformSettingsError_DefaultClient(t *testing.T) {
	resetMiddlewareCache()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Missing required fields will cause transformSettings to fail
	config := &Config{
		ClientSettings: ClientSettings{
			ManagerUrl:    "http://localhost:8080",
			NamespaceCode: "ns",
			ProjectCode:   "proj",
			TokenJWT:      "", // missing token_jwt
		},
	}

	ctx := context.Background()
	handler, err := New(ctx, next, config, "test-middleware")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing configuration")
	assert.Nil(t, handler)
}

func TestNew_TransformSettingsError_HostConfig(t *testing.T) {
	originalFactory := clientFactory
	defer func() { clientFactory = originalFactory }()
	resetMiddlewareCache()

	clientFactory = func(cfg *client.Config) client.Client {
		return &mockClient{}
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Valid default config, but host config has missing token_jwt after merge
	config := &Config{
		ClientSettings: ClientSettings{
			ManagerUrl:    "http://localhost:8080",
			NamespaceCode: "ns",
			ProjectCode:   "default-proj",
			TokenJWT:      "token",
		},
		HostConfigs: []HostConfig{
			{
				Hosts: []string{"example.com"},
				ClientSettings: ClientSettings{
					ProjectCode:   "proj-x",
					ManagerUrl:    "http://other:8080",
					NamespaceCode: "other-ns",
					TokenJWT:      "", // This will override to empty after merge since it's non-empty check
				},
			},
		},
	}

	ctx := context.Background()
	handler, err := New(ctx, next, config, "test-middleware")

	// This should succeed because TokenJWT is inherited from parent (empty string doesn't override)
	assert.NoError(t, err)
	assert.NotNil(t, handler)
}

func TestNew_TransformSettingsError_HostConfigAfterMerge(t *testing.T) {
	resetMiddlewareCache()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Parent has no ProjectCode (so no default client is created)
	// Parent also has no ManagerUrl, which will be inherited by HostConfig
	// HostConfig has ProjectCode but ManagerUrl will be empty after merge
	config := &Config{
		ClientSettings: ClientSettings{
			ManagerUrl:    "", // empty - will cause host config to fail
			NamespaceCode: "ns",
			ProjectCode:   "", // no default client
			TokenJWT:      "token",
		},
		HostConfigs: []HostConfig{
			{
				Hosts:          []string{"example.com"},
				ClientSettings: ClientSettings{ProjectCode: "proj-x"}, // ManagerUrl inherited as empty
			},
		},
	}

	ctx := context.Background()
	handler, err := New(ctx, next, config, "test-middleware")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing configuration")
	assert.Nil(t, handler)
}

func TestCreateConfig(t *testing.T) {
	config := CreateConfig()

	assert.NotNil(t, config)
	assert.Equal(t, "", config.ManagerUrl)
	assert.Equal(t, "", config.NamespaceCode)
	assert.Equal(t, "", config.ProjectCode)
	assert.Equal(t, "", config.TokenJWT)
	assert.Equal(t, "", config.IntervalCheck)
	assert.Nil(t, config.HostConfigs)
}

func TestReloadClient(t *testing.T) {
	t.Run("calls reload on client", func(t *testing.T) {
		mock := &mockClient{}
		reloadFn := reloadClient("test-middleware", "http://localhost|ns|proj", mock)

		assert.False(t, mock.reloadCalled)
		reloadFn()
		assert.True(t, mock.reloadCalled)
	})

	t.Run("logs error to stderr on reload failure", func(t *testing.T) {
		mock := &mockClient{reloadErr: errors.New("connection refused")}
		reloadFn := reloadClient("test-middleware", "http://localhost|ns|proj", mock)

		// This should not panic, just log to stderr
		reloadFn()
		assert.True(t, mock.reloadCalled)
	})
}

func TestStartTicker(t *testing.T) {
	t.Run("calls work function on each tick", func(t *testing.T) {
		var callCount atomic.Int32
		work := func() {
			callCount.Add(1)
		}

		ctx, cancel := context.WithCancel(context.Background())
		startTicker(ctx, 10*time.Millisecond, work)

		// Wait for at least 2 ticks
		time.Sleep(25 * time.Millisecond)
		cancel()

		assert.GreaterOrEqual(t, callCount.Load(), int32(2))
	})

	t.Run("stops when context is canceled", func(t *testing.T) {
		var callCount atomic.Int32
		work := func() {
			callCount.Add(1)
		}

		ctx, cancel := context.WithCancel(context.Background())
		startTicker(ctx, 10*time.Millisecond, work)

		// Cancel immediately
		cancel()

		// Wait a bit to ensure no more calls happen
		time.Sleep(25 * time.Millisecond)

		assert.LessOrEqual(t, callCount.Load(), int32(1))
	})
}

func TestSettingsKey(t *testing.T) {
	settings := ClientSettings{
		ManagerUrl:    "http://localhost:8080",
		NamespaceCode: "ns",
		ProjectCode:   "proj",
	}

	key := settingsKey(settings)
	assert.Equal(t, "http://localhost:8080|ns|proj", key)
}

func TestClientForHost(t *testing.T) {
	defaultMock := &mockClient{}
	hostMock := &mockClient{}

	m := &Middleware{
		defaultClient: defaultMock,
		hostClients: map[string]client.Client{
			"example.com": hostMock,
		},
	}

	t.Run("returns host client when found", func(t *testing.T) {
		c := m.clientForHost("example.com")
		assert.Same(t, hostMock, c)
	})

	t.Run("returns default client when not found", func(t *testing.T) {
		c := m.clientForHost("other.com")
		assert.Same(t, defaultMock, c)
	})

	t.Run("strips port from host", func(t *testing.T) {
		c := m.clientForHost("example.com:443")
		assert.Same(t, hostMock, c)
	})

	t.Run("returns nil when no default and host not found", func(t *testing.T) {
		m := &Middleware{
			defaultClient: nil,
			hostClients: map[string]client.Client{
				"example.com": hostMock,
			},
		}
		c := m.clientForHost("other.com")
		assert.Nil(t, c)
	})
}

func TestNew_WithoutDefaultClient(t *testing.T) {
	originalFactory := clientFactory
	defer func() { clientFactory = originalFactory }()
	resetMiddlewareCache()

	createCount := 0
	clientFactory = func(cfg *client.Config) client.Client {
		createCount++
		return &mockClient{}
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// No ProjectCode on parent, only in HostConfigs
	config := &Config{
		ClientSettings: ClientSettings{
			ManagerUrl:    "http://localhost:8080",
			NamespaceCode: "ns",
			TokenJWT:      "token",
			// ProjectCode intentionally empty
		},
		HostConfigs: []HostConfig{
			{
				Hosts:          []string{"example.com"},
				ClientSettings: ClientSettings{ProjectCode: "proj-com"},
			},
		},
	}

	ctx := context.Background()
	handler, err := New(ctx, next, config, "test-middleware")

	assert.NoError(t, err)
	assert.NotNil(t, handler)
	// Only 1 client created (no default)
	assert.Equal(t, 1, createCount)

	middleware := handler.(*Middleware)
	assert.Nil(t, middleware.defaultClient)
	assert.Len(t, middleware.hostClients, 1)
}

func TestMiddleware_ServeHTTP_SkipsWhenNoClient(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	hostMock := &mockClient{
		redirectMatch: func(hostname, uri string) (*types.Redirect, string) {
			return &types.Redirect{
				Type:   types.RedirectTypeBasic,
				Source: "/test",
				Target: "/redirected",
				Status: types.RedirectStatusFound,
			}, "/redirected"
		},
	}

	middleware := &Middleware{
		name:          "test",
		next:          next,
		debug:         false,
		defaultClient: nil, // No default client
		hostClients: map[string]client.Client{
			"example.com": hostMock,
		},
	}

	t.Run("skips to next handler when no client for host", func(t *testing.T) {
		nextCalled = false
		req := httptest.NewRequest(http.MethodGet, "http://unknown.com/test", nil)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		assert.True(t, nextCalled)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("uses host client when available", func(t *testing.T) {
		nextCalled = false
		req := httptest.NewRequest(http.MethodGet, "http://example.com/test", nil)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		assert.False(t, nextCalled)
		assert.Equal(t, http.StatusFound, rec.Code)
		assert.Equal(t, "/redirected", rec.Header().Get("Location"))
	})
}

// TestNew_SharesClientAcrossRouters verifies the core fix: multiple New() calls
// for the same middleware name (one per router) share a single client instance.
func TestNew_SharesClientAcrossRouters(t *testing.T) {
	originalFactory := clientFactory
	defer func() { clientFactory = originalFactory }()
	resetMiddlewareCache()

	createCount := 0
	clientFactory = func(cfg *client.Config) client.Client {
		createCount++
		return &mockClient{}
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := &Config{ClientSettings: ClientSettings{
		ManagerUrl:    "http://localhost:8080",
		NamespaceCode: "ns",
		ProjectCode:   "proj",
		TokenJWT:      "token",
	}}

	h1, err := New(context.Background(), next, config, "shared")
	assert.NoError(t, err)
	h2, err := New(context.Background(), next, config, "shared")
	assert.NoError(t, err)

	// One client created, shared by both router instances.
	assert.Equal(t, 1, createCount)
	assert.Same(t, h1.(*Middleware).defaultClient, h2.(*Middleware).defaultClient)
}

// withFakeClock installs a controllable clock and a 5m idleDelay for round/GC
// tests, restoring the originals (and clientFactory) on cleanup. It returns a
// pointer to the current fake time that the test can advance.
func withFakeClock(t *testing.T) *time.Time {
	t.Helper()
	originalFactory := clientFactory
	originalNow := nowFunc
	originalIdle := idleDelay
	t.Cleanup(func() {
		clientFactory = originalFactory
		nowFunc = originalNow
		idleDelay = originalIdle
	})

	resetMiddlewareCache()
	clientFactory = func(cfg *client.Config) client.Client { return &mockClient{} }
	idleDelay = 5 * time.Minute

	fake := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFunc = func() time.Time { return fake }
	// Return a pointer into a heap variable the closure also reads.
	return &fake
}

func TestEnvDuration(t *testing.T) {
	const key = "FLECTO_REDIRECT_TEST_DURATION"
	tests := []struct {
		name string
		set  bool
		env  string
		want time.Duration
	}{
		{"unset uses default", false, "", 5 * time.Minute},
		{"empty uses default", true, "", 5 * time.Minute},
		{"valid seconds", true, "30s", 30 * time.Second},
		{"valid minutes", true, "10m", 10 * time.Minute},
		{"invalid uses default", true, "not-a-duration", 5 * time.Minute},
		{"zero uses default", true, "0s", 5 * time.Minute},
		{"negative uses default", true, "-1m", 5 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv(key, tt.env)
			}
			assert.Equal(t, tt.want, envDuration(key, 5*time.Minute))
		})
	}
}

func TestEnvBool(t *testing.T) {
	const key = "FLECTO_REDIRECT_TEST_BOOL"
	tests := []struct {
		name string
		set  bool
		env  string
		want bool
	}{
		{"unset", false, "", false},
		{"empty", true, "", false},
		{"one", true, "1", true},
		{"true lowercase", true, "true", true},
		{"true uppercase", true, "TRUE", true},
		{"true mixed case", true, "True", true},
		{"zero", true, "0", false},
		{"other", true, "yes", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv(key, tt.env)
			}
			assert.Equal(t, tt.want, envBool(key))
		})
	}
}

// TestDebugLogging verifies that with FLECTO_REDIRECT_DEBUG enabled and a custom
// idle delay, the round bumps, the cache snapshot and the orphan removal are all
// logged.
func TestDebugLogging(t *testing.T) {
	clock := withFakeClock(t)
	nowFunc = func() time.Time { return *clock }

	originalDebug := debugEnabled
	originalWriter := logWriter
	t.Cleanup(func() {
		debugEnabled = originalDebug
		logWriter = originalWriter
	})

	var buf bytes.Buffer
	debugEnabled = true
	logWriter = &buf
	idleDelay = 90 * time.Second // custom delay

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	cfgA := &Config{ClientSettings: ClientSettings{ManagerUrl: "http://m", NamespaceCode: "ns", ProjectCode: "a", TokenJWT: "t"}}
	cfgB := &Config{ClientSettings: ClientSettings{ManagerUrl: "http://m", NamespaceCode: "ns", ProjectCode: "b", TokenJWT: "t"}}

	// Round 1: both present.
	_, _ = New(context.Background(), next, cfgA, "mw-a")
	_, _ = New(context.Background(), next, cfgB, "mw-b")
	assert.Contains(t, buf.String(), "new round 1")

	// Round 2 after the custom idle delay, mw-b dropped.
	*clock = clock.Add(2 * time.Minute)
	_, _ = New(context.Background(), next, cfgA, "mw-a")
	assert.Contains(t, buf.String(), "new round 2")

	// Settle, then sweep: snapshot + removal logged.
	*clock = clock.Add(2 * time.Minute)
	buf.Reset()
	sweepOrphans()
	out := buf.String()
	assert.Contains(t, out, "flecto[debug]: round=2")
	assert.Contains(t, out, `removed obsolete client set for "mw-b"`)
	assert.NotContains(t, out, `removed obsolete client set for "mw-a"`)
}

func TestNew_RoundBumpsOnlyAfterIdle(t *testing.T) {
	clock := withFakeClock(t)
	nowFunc = func() time.Time { return *clock }
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	cfg := &Config{ClientSettings: ClientSettings{ManagerUrl: "http://m", NamespaceCode: "ns", ProjectCode: "p", TokenJWT: "t"}}

	// First New: round 1.
	_, err := New(context.Background(), next, cfg, "mw")
	assert.NoError(t, err)
	assert.Equal(t, 1, currentRound)

	// A second New shortly after (< idleDelay): same round (rapid reload / sibling).
	*clock = clock.Add(2 * time.Second)
	_, err = New(context.Background(), next, cfg, "mw")
	assert.NoError(t, err)
	assert.Equal(t, 1, currentRound)

	// A New after >= idleDelay of inactivity: new round.
	*clock = clock.Add(6 * time.Minute)
	_, err = New(context.Background(), next, cfg, "mw")
	assert.NoError(t, err)
	assert.Equal(t, 2, currentRound)
}

func TestSweepOrphans_RemovesDisappearedName(t *testing.T) {
	clock := withFakeClock(t)
	nowFunc = func() time.Time { return *clock }
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	cfgA := &Config{ClientSettings: ClientSettings{ManagerUrl: "http://m", NamespaceCode: "ns", ProjectCode: "a", TokenJWT: "t"}}
	cfgB := &Config{ClientSettings: ClientSettings{ManagerUrl: "http://m", NamespaceCode: "ns", ProjectCode: "b", TokenJWT: "t"}}

	// Round 1: both middlewares present.
	_, _ = New(context.Background(), next, cfgA, "mw-a")
	_, _ = New(context.Background(), next, cfgB, "mw-b")
	assert.Equal(t, 1, currentRound)
	assert.Len(t, middlewareCache, 2)

	// A later rebuild keeps only mw-a (mw-b removed from config). Advance past
	// idleDelay so this New starts a new round.
	*clock = clock.Add(6 * time.Minute)
	_, _ = New(context.Background(), next, cfgA, "mw-a")
	assert.Equal(t, 2, currentRound)
	assert.Len(t, middlewareCache, 2) // mw-b still cached for now

	// Not settled (a New just happened) -> sweep is a no-op.
	sweepOrphans()
	assert.Len(t, middlewareCache, 2)

	// After idleDelay with no New -> sweep removes the orphan (mw-b, round 1).
	*clock = clock.Add(6 * time.Minute)
	sweepOrphans()
	assert.Len(t, middlewareCache, 1)
	_, hasA := middlewareCache["mw-a"]
	_, hasB := middlewareCache["mw-b"]
	assert.True(t, hasA)
	assert.False(t, hasB)
}

func TestSweepOrphans_KeepsLiveEntries(t *testing.T) {
	clock := withFakeClock(t)
	nowFunc = func() time.Time { return *clock }
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	cfgA := &Config{ClientSettings: ClientSettings{ManagerUrl: "http://m", NamespaceCode: "ns", ProjectCode: "a", TokenJWT: "t"}}
	cfgB := &Config{ClientSettings: ClientSettings{ManagerUrl: "http://m", NamespaceCode: "ns", ProjectCode: "b", TokenJWT: "t"}}

	// Two rounds, both names present each time.
	_, _ = New(context.Background(), next, cfgA, "mw-a")
	_, _ = New(context.Background(), next, cfgB, "mw-b")
	*clock = clock.Add(6 * time.Minute)
	_, _ = New(context.Background(), next, cfgA, "mw-a")
	_, _ = New(context.Background(), next, cfgB, "mw-b")
	assert.Equal(t, 2, currentRound)

	// Long idle, then sweep: both were seen in the latest round -> nothing removed.
	*clock = clock.Add(30 * time.Minute)
	sweepOrphans()
	assert.Len(t, middlewareCache, 2)
}

// TestNew_ReuseOnSameConfig_RebuildOnChange verifies that an unchanged config
// (e.g. an ACME-triggered rebuild) reuses the cached client, while an actual
// config change (token rotation) rebuilds it.
func TestNew_ReuseOnSameConfig_RebuildOnChange(t *testing.T) {
	originalFactory := clientFactory
	defer func() { clientFactory = originalFactory }()
	resetMiddlewareCache()

	createCount := 0
	clientFactory = func(cfg *client.Config) client.Client {
		createCount++
		return &mockClient{}
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	base := ClientSettings{
		ManagerUrl:    "http://localhost:8080",
		NamespaceCode: "ns",
		ProjectCode:   "proj",
		TokenJWT:      "token",
	}

	_, err := New(context.Background(), next, &Config{ClientSettings: base}, "mw")
	assert.NoError(t, err)
	assert.Equal(t, 1, createCount)

	// Same config again -> reuse, no new client.
	_, err = New(context.Background(), next, &Config{ClientSettings: base}, "mw")
	assert.NoError(t, err)
	assert.Equal(t, 1, createCount)

	// Token rotation -> same name, different signature -> rebuild.
	changed := base
	changed.TokenJWT = "new-token"
	_, err = New(context.Background(), next, &Config{ClientSettings: changed}, "mw")
	assert.NoError(t, err)
	assert.Equal(t, 2, createCount)
}
