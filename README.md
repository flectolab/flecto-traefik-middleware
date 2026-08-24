# Flecto Traefik Middleware

A Traefik middleware plugin for dynamic URL redirection powered by Flecto.

This middleware intercepts HTTP requests and checks against a [Flecto Manager](https://github.com/flectolab/flecto-manager) to determine if requests should be redirected based on hostname and URI patterns.

## Configuration

### Static Configuration

Add the plugin to your Traefik static configuration:

```yaml
experimental:
  plugins:
    flecto:
      moduleName: github.com/flectolab/flecto-traefik-middleware
      version: vX.X.X
```

Or with TOML:

```toml
[experimental.plugins.flecto]
  moduleName = "github.com/flectolab/flecto-traefik-middleware"
  version = "vX.X.X"
```

### Dynamic Configuration

#### Basic Example (Single Project)

Use this configuration when all hosts use the same Flecto project:

```yaml
http:
  middlewares:
    my-flecto-redirect:
      plugin:
        flecto:
          manager_url: "https://flecto-manager.example.com"
          namespace_code: "my-namespace"
          project_code: "my-project"
          token_jwt: "your-jwt-token"
          header_authorization_name: "Authorization"  # optional, default: Authorization
          interval_check: "5m"                        # optional, default: 5m
          agent_name: "my-traefik-agent"                # optional, default: hostname
          redirects_limit: 500                        # optional, default: 500
          pages_limit: 500                            # optional, default: 500
          debug: false                                # optional, default: false

  routers:
    my-router:
      rule: "Host(`example.com`)"
      middlewares:
        - my-flecto-redirect
      service: my-service
```

#### Multi-Host Example (Multiple Projects)

Use `host_configs` when you need different Flecto projects for different hosts:

```yaml
http:
  middlewares:
    my-flecto-redirect:
      plugin:
        flecto:
          # Parent configuration (shared settings)
          manager_url: "https://flecto-manager.example.com"
          namespace_code: "my-namespace"
          token_jwt: "your-jwt-token"
          interval_check: "5m"
          redirects_limit: 500
          pages_limit: 500
          debug: false
          # project_code can be empty if host_configs is defined
          # In this case, unmatched hosts will skip the middleware

          host_configs:
            # Minimal override: only project_code (required)
            - hosts:
                - "example.com"
                - "example.fr"
              project_code: "project-fr"

            # Full override: all settings can be overridden
            - hosts:
                - "example.es"
              manager_url: "https://other-manager.example.com"
              namespace_code: "other-namespace"
              project_code: "project-es"
              token_jwt: "other-jwt-token"
              header_authorization_name: "X-Custom-Auth"
              interval_check: "10m"
              redirects_limit: 1000
              pages_limit: 100

  routers:
    my-router:
      rule: "Host(`example.com`) || Host(`example.fr`) || Host(`example.es`)"
      middlewares:
        - my-flecto-redirect
      service: my-service
```

## Configuration Options

### Root Configuration

| Option                      | Required | Default         | Description                                                       |
|-----------------------------|----------|-----------------|-------------------------------------------------------------------|
| `manager_url`               | Yes      | -               | URL of the Flecto manager API                                     |
| `namespace_code`            | Yes      | -               | Namespace code in Flecto                                          |
| `project_code`              | Cond.    | -               | Project code in Flecto. Required if `host_configs` is not defined |
| `token_jwt`                 | Yes      | -               | JWT token for authentication with Flecto manager                  |
| `header_authorization_name` | No       | `Authorization` | HTTP header name for the JWT token                                |
| `interval_check`            | No       | `5m`            | Interval to check for redirect rule updates                       |
| `agent_name`                 | No       | `hostname`      | Name of this Traefik agent (for agent identification)             |
| `redirects_limit`           | No       | `500`           | Number of redirects fetched per listing request when reloading    |
| `pages_limit`               | No       | `500`           | Number of pages fetched per listing request when reloading        |
| `debug`                     | No       | `false`         | Add some headers (project version, url used and redirect matched) |
| `host_configs`              | No       | -               | List of host-specific configurations (see below)                  |

### Host Configuration (`host_configs[]`)

| Option                      | Required | Inherited | Description                                        |
|-----------------------------|----------|-----------|----------------------------------------------------|
| `hosts`                     | Yes      | No        | List of hostnames for this configuration           |
| `project_code`              | Yes      | No        | Project code in Flecto (cannot be inherited)       |
| `manager_url`               | No       | Yes       | Override the manager URL                           |
| `namespace_code`            | No       | Yes       | Override the namespace code                        |
| `token_jwt`                 | No       | Yes       | Override the JWT token                             |
| `header_authorization_name` | No       | Yes       | Override the authorization header name             |
| `interval_check`            | No       | Yes       | Override the interval check duration               |
| `redirects_limit`           | No       | Yes       | Override the redirects listing page size           |
| `pages_limit`               | No       | Yes       | Override the pages listing page size               |

**Notes:**
- `project_code` is always required in each `host_configs` entry and is never inherited from the parent configuration.
- `agent_name` cannot be overridden in `host_configs` and is always inherited from the root configuration.

### Environment Variables

These tune the plugin globally (set them on the Traefik process), independently of the per-middleware configuration above.

| Variable                     | Default | Description                                                                                                   |
|------------------------------|---------|---------------------------------------------------------------------------------------------------------------|
| `FLECTO_REDIRECT_IDLE_DELAY` | `5m`    | Go duration (e.g. `30s`, `10m`). Inactivity window used to detect a settled rebuild before cleaning up obsolete clients (see *Client sharing and cleanup*). Invalid or `<= 0` values fall back to the default. |
| `FLECTO_REDIRECT_DEBUG`      | `false` | `1` or `true` enables verbose stderr logging: the configuration of each client as it is created, the state version transition on every client reload, plus the internal client cache state (rounds, per-name client state, and removals). Useful to observe the effective configuration and obsolete clients being cleaned up. |

When `FLECTO_REDIRECT_DEBUG` is enabled, the plugin logs, prefixed with `flecto[debug]:`:

- **Client creation** — the *effective* client configuration, after inheritance from the root configuration and after defaults are applied (so `redirects_limit` / `pages_limit` show the real values sent to the manager). One line per client, logged only when a client is actually built: a rebuild that reuses the cached clients (unchanged configuration) logs nothing:

  ```
  flecto[debug]: flecto@file: created client for https://flecto-manager.example.com|my-namespace|my-project config{manager_url=https://flecto-manager.example.com namespace_code=my-namespace project_code=my-project agent_name=traefik-1 agent_type=traefik header_authorization_name=Authorization interval_check=5m0s redirects_limit=500 pages_limit=500}
  ```

- **Client reload** — on each polling tick, with the project version before and after and whether the reload succeeded. The configuration is deliberately not repeated here, it is only logged at creation:

  ```
  flecto[debug]: flecto@file: reload ok for https://flecto-manager.example.com|my-namespace|my-project (stateVersion 41 -> 42)
  ```

> **Note:** secrets are excluded from every log. JWT tokens are never written out, not even masked: `token_jwt` is absent from the client configuration line.

## How It Works

1. The middleware connects to the Flecto manager on startup
2. It periodically polls for redirect rule updates (configurable via `interval_check`), fetching redirects and pages in paginated batches (configurable via `redirects_limit` and `pages_limit`)
3. For each incoming request, it checks if the hostname and URI match any redirect rule
4. If a match is found, the request is redirected with the appropriate HTTP status code (301, 302, 307, or 308)
5. If no match is found, the request is passed to the next handler

### Behavior with `host_configs`

When `host_configs` is defined:

- Each incoming request is matched against the configured hosts
- If a host matches, the corresponding project's client is used
- If no host matches and `project_code` is defined at the root level, the default client is used
- If no host matches and `project_code` is **not** defined at the root level, the middleware is skipped and the request is passed to the next handler

### Client sharing and cleanup

Traefik calls the plugin once per router that references a middleware, and rebuilds the whole router tree on any configuration change (and on its own, e.g. on ACME events). To avoid spawning a separate Flecto client and polling loop per router, clients are shared in a process-wide cache **keyed by middleware name**:

- All routers referencing the same middleware share a single client (and a single reload ticker) per project. This guarantees the polled state stays consistent and fresh for every router.
- Any configuration change for an existing middleware name (token rotation, project, interval, host configs...) replaces its client set in place, and the previous tickers are stopped immediately — no leak.
- The only client that cannot be cleaned up immediately is one whose middleware **name disappears** (the middleware is removed or renamed), because Traefik provides no teardown signal. A background sweeper removes such orphaned clients once the configuration has been idle for `FLECTO_REDIRECT_IDLE_DELAY` (default 5 minutes), stopping their polling and agent heartbeat. Set `FLECTO_REDIRECT_DEBUG=1` to watch this happen in the logs.

> **Known limitation — removing the last Flecto middleware.** The sweeper detects an orphan by noticing that a *surviving* middleware was rebuilt while the removed one was not. If you remove the **only/last** middleware that references this plugin, the plugin stops receiving any calls from Traefik (no surviving middleware to rebuild), so the orphan cannot be detected and its client keeps polling until Traefik is restarted. This is irreducible: with no teardown signal and no surviving middleware, the plugin's internal state for "a still-present middleware that simply wasn't rebuilt" is indistinguishable from "a removed middleware". Removing one middleware among several is fine — survivors trigger the cleanup on the next reload (e.g. an ACME event or another config change). A restart clears any such leftover client.
