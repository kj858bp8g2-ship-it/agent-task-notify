package core

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/assets"
)

type Agent struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	IconURL     string `json:"iconUrl"`
}

func Agents() []Agent {
	var agents []Agent
	if json.Unmarshal(assets.AgentIconsJSON(), &agents) != nil {
		return nil
	}
	return append([]Agent(nil), agents...)
}

func AgentByID(id string) (Agent, error) {
	for _, agent := range Agents() {
		if agent.ID == id {
			return agent, nil
		}
	}
	return Agent{}, errors.New("unknown agent source")
}

func Icon(agentID string, settings Settings) string {
	agent, err := AgentByID(agentID)
	if err != nil {
		return ""
	}
	if override, configured := settings.Icons[agentID]; configured {
		if validHTTPSURL(override) {
			return override
		}
		return ""
	}
	if validHTTPSURL(agent.IconURL) {
		return agent.IconURL
	}
	return ""
}

func validHTTPSURL(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	parsed, err := url.ParseRequestURI(value)
	return err == nil && parsed.IsAbs() && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil
}
