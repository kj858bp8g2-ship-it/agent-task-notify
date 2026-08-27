package providers

import (
	"fmt"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/core"
)

type Message struct {
	AgentID         string `json:"agentId"`
	DurationSeconds int64  `json:"durationSeconds"`
	Reason          string `json:"reason"`
	Preview         bool   `json:"preview"`
}

type barkPayload struct {
	Title     string  `json:"title"`
	Body      string  `json:"body"`
	Group     string  `json:"group"`
	Level     string  `json:"level"`
	Volume    int     `json:"volume"`
	Sound     string  `json:"sound"`
	IsArchive int     `json:"isArchive"`
	Icon      *string `json:"icon,omitempty"`
	Call      *int    `json:"call,omitempty"`
}

type ntfyPayload struct {
	Topic    string  `json:"topic"`
	Title    string  `json:"title"`
	Message  string  `json:"message"`
	Priority int     `json:"priority"`
	Icon     *string `json:"icon,omitempty"`
}

func payloadFor(provider string, settings core.Settings, message Message, topic string) (any, error) {
	agent, err := core.AgentByID(message.AgentID)
	if err != nil {
		return nil, err
	}
	title := agent.DisplayName + " 长任务已停止"
	minutes := message.DurationSeconds / 60
	if message.DurationSeconds < 0 && message.DurationSeconds%60 != 0 {
		minutes--
	}
	body := fmt.Sprintf("任务已停止，请查看应用。耗时约 %d 分钟。", minutes)
	if message.Reason == "attention" {
		title = agent.DisplayName + " 任务需要关注"
	}
	if message.Preview {
		title, body = agent.DisplayName+" 通知预览", "这是一条通用测试通知。"
	}
	iconValue := core.Icon(message.AgentID, settings)
	var icon *string
	if iconValue != "" {
		icon = &iconValue
	}
	switch provider {
	case "bark":
		payload := barkPayload{Title: title, Body: body, Group: agent.DisplayName, Level: settings.Level, Volume: settings.Volume, Sound: settings.Sound, IsArchive: 0, Icon: icon}
		if settings.Continuous {
			call := 1
			payload.Call = &call
		}
		return payload, nil
	case "ntfy":
		return ntfyPayload{Topic: topic, Title: title, Message: body, Priority: settings.NtfyPriority, Icon: icon}, nil
	default:
		return nil, errCredential
	}
}
