package types

import (
	"fmt"
	"regexp"
)

var validAgentNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

type AgentType string

const (
	AgentTypeDefault AgentType = "default"
	AgentTypeTraefik AgentType = "traefik"
)

func (t AgentType) IsValid() bool {
	switch t {
	case AgentTypeDefault, AgentTypeTraefik:
		return true
	default:
		return false
	}
}

type AgentStatus string

const (
	AgentStatusSuccess AgentStatus = "success"
	AgentStatusError   AgentStatus = "error"
)

func (s AgentStatus) IsValid() bool {
	switch s {
	case AgentStatusSuccess, AgentStatusError:
		return true
	default:
		return false
	}
}

type Agent struct {
	Name         string      `json:"name" gorm:"size:100"`
	Status       AgentStatus `json:"status" gorm:"size:50"`
	Type         AgentType   `json:"type" gorm:"size:50"`
	Version      int         `json:"version"`
	LoadDuration Duration    `json:"load_duration"`
	Error        string      `json:"error" gorm:"size:500"`
}

func ValidateAgent(agent Agent) error {
	if !validAgentNameRegex.MatchString(agent.Name) {
		return fmt.Errorf("invalid agent name: only alphanumeric characters, underscores and hyphens are allowed")
	}

	if !agent.Type.IsValid() {
		return fmt.Errorf("invalid agent type: %s", agent.Type)
	}

	if agent.Status != "" && !agent.Status.IsValid() {
		return fmt.Errorf("invalid agent status: %s", agent.Status)
	}

	if agent.Version == 0 {
		return fmt.Errorf("agent version is required")
	}

	return nil
}
