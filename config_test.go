package flecto_traefik_middleware

import (
	"testing"
	"time"

	"github.com/flectolab/flecto-manager/common/types"
	"github.com/stretchr/testify/assert"
)

func TestTransformSettings(t *testing.T) {
	tests := []struct {
		name        string
		middlewareN string
		settings    ClientSettings
		wantErr     bool
		errContains string
	}{
		{
			name:        "missing manager_url",
			middlewareN: "test-middleware",
			settings: ClientSettings{
				ManagerUrl:    "",
				NamespaceCode: "ns",
				ProjectCode:   "proj",
				TokenJWT:      "token",
			},
			wantErr:     true,
			errContains: "missing configuration",
		},
		{
			name:        "missing namespace_code",
			middlewareN: "test-middleware",
			settings: ClientSettings{
				ManagerUrl:    "http://localhost:8080",
				NamespaceCode: "",
				ProjectCode:   "proj",
				TokenJWT:      "token",
			},
			wantErr:     true,
			errContains: "missing configuration",
		},
		{
			name:        "missing project_code",
			middlewareN: "test-middleware",
			settings: ClientSettings{
				ManagerUrl:    "http://localhost:8080",
				NamespaceCode: "ns",
				ProjectCode:   "",
				TokenJWT:      "token",
			},
			wantErr:     true,
			errContains: "missing configuration",
		},
		{
			name:        "missing token_jwt",
			middlewareN: "test-middleware",
			settings: ClientSettings{
				ManagerUrl:    "http://localhost:8080",
				NamespaceCode: "ns",
				ProjectCode:   "proj",
				TokenJWT:      "",
			},
			wantErr:     true,
			errContains: "missing configuration",
		},
		{
			name:        "valid settings with required fields only",
			middlewareN: "test-middleware",
			settings: ClientSettings{
				ManagerUrl:    "http://localhost:8080",
				NamespaceCode: "ns",
				ProjectCode:   "proj",
				TokenJWT:      "token",
			},
			wantErr: false,
		},
		{
			name:        "valid settings with all fields",
			middlewareN: "test-middleware",
			settings: ClientSettings{
				ManagerUrl:              "http://localhost:8080",
				NamespaceCode:           "ns",
				ProjectCode:             "proj",
				TokenJWT:                "token",
				HeaderAuthorizationName: "X-Custom-Auth",
				IntervalCheck:           "5s",
			},
			wantErr: false,
		},
		{
			name:        "error invalid interval check duration",
			middlewareN: "test-middleware",
			settings: ClientSettings{
				ManagerUrl:              "http://localhost:8080",
				NamespaceCode:           "ns",
				ProjectCode:             "proj",
				TokenJWT:                "token",
				HeaderAuthorizationName: "X-Custom-Auth",
				IntervalCheck:           "wrong duration",
			},
			wantErr: true,
		},
		{
			name:        "error message contains middleware name",
			middlewareN: "my-custom-middleware",
			settings: ClientSettings{
				ManagerUrl:    "",
				NamespaceCode: "",
				ProjectCode:   "",
				TokenJWT:      "",
			},
			wantErr:     true,
			errContains: "my-custom-middleware",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := transformSettings(tt.middlewareN, tt.settings)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, got)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, got)
				assert.Equal(t, tt.settings.ManagerUrl, got.ManagerUrl)
				assert.Equal(t, tt.settings.NamespaceCode, got.NamespaceCode)
				assert.Equal(t, tt.settings.ProjectCode, got.ProjectCode)
				assert.Equal(t, tt.settings.TokenJWT, got.Http.TokenJWT)

				if tt.settings.HeaderAuthorizationName != "" {
					assert.Equal(t, tt.settings.HeaderAuthorizationName, got.Http.HeaderAuthorizationName)
				}
				if tt.settings.IntervalCheck != "" {
					duration, err := time.ParseDuration(tt.settings.IntervalCheck)
					assert.NoError(t, err)
					assert.Equal(t, got.IntervalCheck, duration)
				}
			}
		})
	}
}

func TestTransformSettings_AgentTypeAndAgentName(t *testing.T) {
	t.Run("AgentType is always AgentTypeTraefik", func(t *testing.T) {
		settings := ClientSettings{
			ManagerUrl:    "http://localhost:8080",
			NamespaceCode: "ns",
			ProjectCode:   "proj",
			TokenJWT:      "token",
		}
		got, err := transformSettings("test", settings)
		assert.NoError(t, err)
		assert.Equal(t, types.AgentTypeTraefik, got.AgentType)
	})

	t.Run("AgentName is set when provided", func(t *testing.T) {
		settings := ClientSettings{
			ManagerUrl:    "http://localhost:8080",
			NamespaceCode: "ns",
			ProjectCode:   "proj",
			TokenJWT:      "token",
			AgentName:     "my-traefik-node",
		}
		got, err := transformSettings("test", settings)
		assert.NoError(t, err)
		assert.Equal(t, "my-traefik-node", got.AgentName)
	})

	t.Run("AgentName uses default when empty", func(t *testing.T) {
		settings := ClientSettings{
			ManagerUrl:    "http://localhost:8080",
			NamespaceCode: "ns",
			ProjectCode:   "proj",
			TokenJWT:      "token",
			AgentName:     "",
		}
		got, err := transformSettings("test", settings)
		assert.NoError(t, err)
		// Default is the hostname from client.NewDefaultConfig()
		assert.NotEmpty(t, got.AgentName)
	})
}

func TestMergeSettings(t *testing.T) {
	parent := ClientSettings{
		ManagerUrl:              "http://parent.com",
		NamespaceCode:           "parent-ns",
		ProjectCode:             "parent-proj",
		TokenJWT:                "parent-token",
		HeaderAuthorizationName: "X-Parent-Auth",
		IntervalCheck:           "10s",
		AgentName:               "hostname",
	}

	t.Run("inherits parent except ProjectCode", func(t *testing.T) {
		override := ClientSettings{
			ProjectCode: "override-proj", // required
		}
		result := mergeSettings(parent, override)

		assert.Equal(t, parent.ManagerUrl, result.ManagerUrl)
		assert.Equal(t, parent.NamespaceCode, result.NamespaceCode)
		assert.Equal(t, "override-proj", result.ProjectCode) // from override
		assert.Equal(t, parent.TokenJWT, result.TokenJWT)
		assert.Equal(t, parent.HeaderAuthorizationName, result.HeaderAuthorizationName)
		assert.Equal(t, parent.IntervalCheck, result.IntervalCheck)
	})

	t.Run("ProjectCode is never inherited from parent", func(t *testing.T) {
		override := ClientSettings{
			ProjectCode: "", // empty - should NOT inherit from parent
		}
		result := mergeSettings(parent, override)

		assert.Equal(t, "", result.ProjectCode) // empty, not inherited
	})

	t.Run("overrides multiple fields", func(t *testing.T) {
		override := ClientSettings{
			ManagerUrl:    "http://override.com",
			NamespaceCode: "override-ns",
			ProjectCode:   "override-proj",
		}
		result := mergeSettings(parent, override)

		assert.Equal(t, "http://override.com", result.ManagerUrl)
		assert.Equal(t, "override-ns", result.NamespaceCode)
		assert.Equal(t, "override-proj", result.ProjectCode)
		assert.Equal(t, parent.TokenJWT, result.TokenJWT)
	})

	t.Run("overrides all fields", func(t *testing.T) {
		override := ClientSettings{
			ManagerUrl:              "http://override.com",
			NamespaceCode:           "override-ns",
			ProjectCode:             "override-proj",
			TokenJWT:                "override-token",
			HeaderAuthorizationName: "X-Override-Auth",
			IntervalCheck:           "30s",
		}
		result := mergeSettings(parent, override)

		assert.Equal(t, override.ManagerUrl, result.ManagerUrl)
		assert.Equal(t, override.NamespaceCode, result.NamespaceCode)
		assert.Equal(t, override.ProjectCode, result.ProjectCode)
		assert.Equal(t, override.TokenJWT, result.TokenJWT)
		assert.Equal(t, override.HeaderAuthorizationName, result.HeaderAuthorizationName)
		assert.Equal(t, override.IntervalCheck, result.IntervalCheck)
	})

	t.Run("AgentName is always inherited from parent and cannot be overridden", func(t *testing.T) {
		override := ClientSettings{
			ProjectCode: "override-proj",
			AgentName:   "override-hostname", // attempt to override
		}
		result := mergeSettings(parent, override)

		assert.Equal(t, "hostname", result.AgentName) // always from parent
	})
}

func TestValidateConfig(t *testing.T) {
	t.Run("error when no project_code and no host_configs", func(t *testing.T) {
		config := &Config{
			ClientSettings: ClientSettings{
				ManagerUrl:    "http://localhost:8080",
				NamespaceCode: "ns",
				ProjectCode:   "",
				TokenJWT:      "token",
			},
		}
		err := validateConfig(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "either project_code or host_configs must be configured")
	})

	t.Run("valid config without host_configs", func(t *testing.T) {
		config := &Config{
			ClientSettings: ClientSettings{
				ManagerUrl:    "http://localhost:8080",
				NamespaceCode: "ns",
				ProjectCode:   "proj",
				TokenJWT:      "token",
			},
		}
		err := validateConfig(config)
		assert.NoError(t, err)
	})

	t.Run("valid config with host_configs", func(t *testing.T) {
		config := &Config{
			ClientSettings: ClientSettings{
				ManagerUrl:    "http://localhost:8080",
				NamespaceCode: "ns",
				ProjectCode:   "proj",
				TokenJWT:      "token",
			},
			HostConfigs: []HostConfig{
				{Hosts: []string{"example.com"}, ClientSettings: ClientSettings{ProjectCode: "proj-com"}},
				{Hosts: []string{"example.fr", "example.es"}, ClientSettings: ClientSettings{ProjectCode: "proj-fr"}},
			},
		}
		err := validateConfig(config)
		assert.NoError(t, err)
	})

	t.Run("error when host_configs has empty hosts", func(t *testing.T) {
		config := &Config{
			ClientSettings: ClientSettings{
				ManagerUrl:    "http://localhost:8080",
				NamespaceCode: "ns",
				ProjectCode:   "proj",
				TokenJWT:      "token",
			},
			HostConfigs: []HostConfig{
				{Hosts: []string{"example.com"}, ClientSettings: ClientSettings{ProjectCode: "proj-com"}},
				{Hosts: []string{}, ClientSettings: ClientSettings{ProjectCode: "proj-empty"}}, // invalid hosts
			},
		}
		err := validateConfig(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "host_configs[1]")
		assert.Contains(t, err.Error(), "hosts is required")
	})

	t.Run("error when first host_config has empty hosts", func(t *testing.T) {
		config := &Config{
			ClientSettings: ClientSettings{
				ManagerUrl:    "http://localhost:8080",
				NamespaceCode: "ns",
				ProjectCode:   "proj",
				TokenJWT:      "token",
			},
			HostConfigs: []HostConfig{
				{Hosts: []string{}, ClientSettings: ClientSettings{ProjectCode: "proj"}}, // invalid hosts
			},
		}
		err := validateConfig(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "host_configs[0]")
	})

	t.Run("error when host_config has no project_code", func(t *testing.T) {
		config := &Config{
			ClientSettings: ClientSettings{
				ManagerUrl:    "http://localhost:8080",
				NamespaceCode: "ns",
				ProjectCode:   "proj",
				TokenJWT:      "token",
			},
			HostConfigs: []HostConfig{
				{Hosts: []string{"example.com"}, ClientSettings: ClientSettings{ProjectCode: ""}}, // missing project_code
			},
		}
		err := validateConfig(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "host_configs[0]")
		assert.Contains(t, err.Error(), "project_code is required")
	})
}
