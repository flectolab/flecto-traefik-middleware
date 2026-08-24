package flecto_traefik_middleware

import (
	"fmt"
	"time"

	"github.com/flectolab/flecto-manager/common/types"
	"github.com/flectolab/go-client"
)

// ClientSettings holds the common client configuration settings.
type ClientSettings struct {
	ManagerUrl    string `json:"manager_url" mapstructure:"manager_url"`
	NamespaceCode string `json:"namespace_code" mapstructure:"namespace_code"`
	ProjectCode   string `json:"project_code" mapstructure:"project_code"`

	HeaderAuthorizationName string `json:"header_authorization_name" mapstructure:"header_authorization_name"`
	TokenJWT                string `json:"token_jwt" mapstructure:"token_jwt"`

	IntervalCheck string `json:"interval_check" mapstructure:"interval_check"`
	AgentName     string `json:"agent_name" mapstructure:"agent_name"`

	// RedirectsLimit and PagesLimit are how many items the client asks for per
	// listing request when it reloads its state. Zero means unset: the client
	// falls back to its own default (client.DefaultRedirectsLimit /
	// client.DefaultPagesLimit).
	RedirectsLimit int `json:"redirects_limit" mapstructure:"redirects_limit"`
	PagesLimit     int `json:"pages_limit" mapstructure:"pages_limit"`
}

// HostConfig holds the configuration for specific hosts.
type HostConfig struct {
	Hosts          []string `json:"hosts" mapstructure:"hosts"` // required
	ClientSettings `mapstructure:",squash"`
}

// Config holds the plugin configuration.
type Config struct {
	ClientSettings `mapstructure:",squash"`
	Debug          bool         `json:"debug" mapstructure:"debug"`
	HostConfigs    []HostConfig `json:"host_configs" mapstructure:"host_configs"`
}

// CreateConfig creates the default plugin configuration.
func CreateConfig() *Config {
	return &Config{}
}

// mergeSettings merges parent settings with override settings.
// Override values take precedence when non-empty.
// Note: ProjectCode is NOT merged - it must be explicitly set in the override.
// Note: AgentName is always inherited from parent and cannot be overridden.
func mergeSettings(parent, override ClientSettings) ClientSettings {
	result := parent
	if override.ManagerUrl != "" {
		result.ManagerUrl = override.ManagerUrl
	}
	if override.NamespaceCode != "" {
		result.NamespaceCode = override.NamespaceCode
	}
	// ProjectCode is required in HostConfig and cannot be inherited
	result.ProjectCode = override.ProjectCode
	if override.HeaderAuthorizationName != "" {
		result.HeaderAuthorizationName = override.HeaderAuthorizationName
	}
	if override.TokenJWT != "" {
		result.TokenJWT = override.TokenJWT
	}
	if override.IntervalCheck != "" {
		result.IntervalCheck = override.IntervalCheck
	}
	// Zero means unset, so any non-zero value (including an invalid negative one,
	// rejected later by transformSettings) overrides the parent.
	if override.RedirectsLimit != 0 {
		result.RedirectsLimit = override.RedirectsLimit
	}
	if override.PagesLimit != 0 {
		result.PagesLimit = override.PagesLimit
	}
	// AgentName is always inherited from parent and cannot be overridden
	result.AgentName = parent.AgentName
	return result
}

func transformSettings(name string, settings ClientSettings) (*client.Config, error) {
	clientCfg := client.NewDefaultConfig()
	if settings.ManagerUrl == "" || settings.NamespaceCode == "" || settings.ProjectCode == "" || settings.TokenJWT == "" {
		return nil, fmt.Errorf("%s: missing configuration, manager_url, namespace_code, project_code or token_jwt is mandatory", name)
	}
	clientCfg.ManagerUrl = settings.ManagerUrl
	clientCfg.NamespaceCode = settings.NamespaceCode
	clientCfg.ProjectCode = settings.ProjectCode
	clientCfg.Http.TokenJWT = settings.TokenJWT

	clientCfg.AgentType = types.AgentTypeTraefik
	if settings.AgentName != "" {
		clientCfg.AgentName = settings.AgentName
	}

	if settings.HeaderAuthorizationName != "" {
		clientCfg.Http.HeaderAuthorizationName = settings.HeaderAuthorizationName
	}

	if settings.IntervalCheck != "" {
		intervalCheck, err := time.ParseDuration(settings.IntervalCheck)
		if err != nil {
			return nil, fmt.Errorf("%s: invalid interval check duration (%v)", name, err)
		}
		clientCfg.IntervalCheck = intervalCheck
	}

	if settings.RedirectsLimit < 0 {
		return nil, fmt.Errorf("%s: invalid redirects limit (%d), must be greater than 0", name, settings.RedirectsLimit)
	}
	if settings.RedirectsLimit > 0 {
		clientCfg.RedirectsLimit = settings.RedirectsLimit
	}

	if settings.PagesLimit < 0 {
		return nil, fmt.Errorf("%s: invalid pages limit (%d), must be greater than 0", name, settings.PagesLimit)
	}
	if settings.PagesLimit > 0 {
		clientCfg.PagesLimit = settings.PagesLimit
	}

	return clientCfg, nil
}

// validateConfig validates the plugin configuration.
func validateConfig(config *Config) error {
	// Must have either a default ProjectCode or at least one HostConfig
	if config.ProjectCode == "" && len(config.HostConfigs) == 0 {
		return fmt.Errorf("either project_code or host_configs must be configured")
	}

	for i, hc := range config.HostConfigs {
		if len(hc.Hosts) == 0 {
			return fmt.Errorf("host_configs[%d]: hosts is required and cannot be empty", i)
		}
		if hc.ProjectCode == "" {
			return fmt.Errorf("host_configs[%d]: project_code is required", i)
		}
	}
	return nil
}
