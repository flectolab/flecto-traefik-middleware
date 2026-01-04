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

**Notes:**
- `project_code` is always required in each `host_configs` entry and is never inherited from the parent configuration.
- `agent_name` cannot be overridden in `host_configs` and is always inherited from the root configuration.

## How It Works

1. The middleware connects to the Flecto manager on startup
2. It periodically polls for redirect rule updates (configurable via `interval_check`)
3. For each incoming request, it checks if the hostname and URI match any redirect rule
4. If a match is found, the request is redirected with the appropriate HTTP status code (301, 302, 307, or 308)
5. If no match is found, the request is passed to the next handler

### Behavior with `host_configs`

When `host_configs` is defined:

- Each incoming request is matched against the configured hosts
- If a host matches, the corresponding project's client is used
- If no host matches and `project_code` is defined at the root level, the default client is used
- If no host matches and `project_code` is **not** defined at the root level, the middleware is skipped and the request is passed to the next handler
