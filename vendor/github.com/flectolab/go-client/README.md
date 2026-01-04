# Flecto Go Client

Go client library for [Flecto Manager](https://github.com/flectolab/flecto-manager). Fetches and caches redirect rules and pages from the Flecto Manager API, with automatic periodic refresh based on project version changes.

## Installation

```bash
go get github.com/flectolab/go-client
```

## Configuration

```go
import (
    client "github.com/flectolab/go-client"
    "github.com/flectolab/flecto-manager/common/types"
)

cfg := client.NewDefaultConfig()
cfg.ManagerUrl = "http://localhost:8080"
cfg.NamespaceCode = "my-namespace"
cfg.ProjectCode = "my-project"
cfg.AgentType = types.AgentTypeDefault // Required
cfg.Http.TokenJWT = "your-jwt-token"

// Optional settings
cfg.AgentName = "my-agent"             // Default: hostname
cfg.IntervalCheck = 5 * time.Minute    // Default: 5 minutes
```

### Configuration Options

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `ManagerUrl` | `string` | Yes | `""` | Flecto Manager API URL |
| `NamespaceCode` | `string` | Yes | `""` | Namespace identifier |
| `ProjectCode` | `string` | Yes | `""` | Project identifier |
| `AgentType` | `types.AgentType` | Yes | `""` | Agent type (e.g. `types.AgentTypeDefault`) |
| `AgentName` | `string` | No | hostname | Agent name for status reporting |
| `IntervalCheck` | `time.Duration` | No | `5m` | Interval between version checks |
| `Http.TokenJWT` | `string` | Yes | `""` | JWT token for authentication |
| `Http.HeaderAuthorizationName` | `string` | No | `"Authorization"` | Authorization header name |

## Usage

### Create the client

```go
c := client.New(cfg)
```

### Initialize and load state

```go
err := c.Init()
if err != nil {
    log.Fatal(err)
}
```

### Match redirects and pages

```go
// Check for redirect match
redirect, target := c.RedirectMatch("example.com", "/old-path")
if redirect != nil {
    // Handle redirect to target
}

// Check for page match
page := c.PageMatch("example.com", "/robots.txt")
if page != nil {
    // Serve page content
}
```

## Refresh Modes

### Manual refresh with Reload

Use `Reload()` when you want to control when the client checks for updates:

```go
c := client.New(cfg)
err := c.Init()
if err != nil {
    log.Fatal(err)
}

// Later, manually trigger a reload
err = c.Reload()
if err != nil {
    log.Printf("reload failed: %v", err)
}
```

`Reload()` checks the project version and only fetches new data if the version has changed.

### Automatic refresh with Start

Use `Start()` for automatic background refresh at the configured interval:

```go
c := client.New(cfg)
err := c.Init()
if err != nil {
    log.Fatal(err)
}

ctx, cancel := context.WithCancel(context.Background())
defer cancel()

// Start background refresh loop
go c.Start(ctx)

// Your application logic...
```

`Start()` runs a loop that calls `Reload()` at every `IntervalCheck` interval. Cancel the context to stop the loop.

## Complete Example

```go
package main

import (
    "context"
    "log"
    "net/http"
    "time"

    client "github.com/flectolab/go-client"
    "github.com/flectolab/flecto-manager/common/types"
)

func main() {
    cfg := client.NewDefaultConfig()
    cfg.ManagerUrl = "http://localhost:8080"
    cfg.NamespaceCode = "production"
    cfg.ProjectCode = "website"
    cfg.AgentType = types.AgentTypeDefault
    cfg.Http.TokenJWT = "your-jwt-token"
    cfg.IntervalCheck = 1 * time.Minute

    c := client.New(cfg)

    if err := c.Init(); err != nil {
        log.Fatalf("failed to initialize client: %v", err)
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    go c.Start(ctx)

    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        // Check for page
        if page := c.PageMatch(r.Host, r.URL.RequestURI()); page != nil {
            w.Header().Set("Content-Type", string(page.ContentType))
            w.Write([]byte(page.Content))
            return
        }

        // Check for redirect
        if redirect, target := c.RedirectMatch(r.Host, r.URL.RequestURI()); redirect != nil {
            http.Redirect(w, r, target, int(redirect.Status))
            return
        }

        // Continue with normal handling...
        w.Write([]byte("Hello World"))
    })

    log.Fatal(http.ListenAndServe(":8080", nil))
}
```

## API Reference

### Client Interface

```go
type Client interface {
    Init() error
    Reload() error
    Start(ctx context.Context)
    GetStateVersion() int
    RedirectMatch(host, uri string) (*types.Redirect, string)
    PageMatch(host, uri string) *types.Page
}
```

| Method | Description |
|--------|-------------|
| `Init()` | Initialize the client and load initial state |
| `Reload()` | Check version and reload state if changed |
| `Start(ctx)` | Start background refresh loop |
| `GetStateVersion()` | Get current project version |
| `RedirectMatch(host, uri)` | Find matching redirect rule |
| `PageMatch(host, uri)` | Find matching page |
