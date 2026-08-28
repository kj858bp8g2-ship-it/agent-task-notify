package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/core"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/strictjson"
)

const maxResponseBytes = 64*1024 + 1

type Result struct {
	Accepted   bool
	Retryable  bool
	Diagnostic string
}

func Send(ctx context.Context, settings core.Settings, credential Credential, message Message) Result {
	if err := core.ValidateSettings(settings); err != nil || ValidateCredential(settings.Provider, credential) != nil {
		return Result{Diagnostic: "credential"}
	}
	endpoint, err := credentialURL(settings.Provider, credential.Endpoint)
	if err != nil {
		return Result{Diagnostic: "credential"}
	}
	topic := strings.TrimPrefix(endpoint.Path, "/")
	payload, err := payloadFor(settings.Provider, settings, message, topic)
	if err != nil {
		return Result{Diagnostic: "credential"}
	}
	target := *endpoint
	if settings.Provider == "ntfy" {
		target.Path, target.RawPath = "/", ""
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Result{Diagnostic: "transport", Retryable: true}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), strings.NewReader(string(encoded)))
	if err != nil {
		return Result{Diagnostic: "transport", Retryable: ctx.Err() == nil}
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	if settings.Provider == "ntfy" && credential.Token != "" {
		req.Header.Set("Authorization", "Bearer "+credential.Token)
	}
	client := &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return Result{Diagnostic: "transport", Retryable: ctx.Err() == nil}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return Result{Retryable: retryableStatus(int64(response.StatusCode)), Diagnostic: statusDiagnostic(response.StatusCode)}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil && ctx.Err() != nil {
		return Result{Diagnostic: "transport"}
	}
	if err != nil || len(body) > maxResponseBytes-1 {
		return Result{Retryable: true, Diagnostic: "malformed-response"}
	}
	object, err := strictjson.Object(body)
	if err != nil {
		return Result{Retryable: true, Diagnostic: "malformed-response"}
	}
	if settings.Provider == "bark" {
		return barkResult(object)
	}
	return ntfyResult(object, topic)
}

func retryableStatus(code int64) bool {
	return code == 408 || code == 425 || code == 429 || code >= 500 && code <= 599
}

func statusDiagnostic(code int) string {
	if code >= 500 && code <= 599 {
		return "http-server"
	}
	return "http-client"
}

func barkResult(object map[string]json.RawMessage) Result {
	codeRaw, ok := object["code"]
	if !ok {
		return Result{Retryable: true, Diagnostic: "malformed-response"}
	}
	code, err := strictjson.Integer(codeRaw)
	if err != nil {
		return Result{Retryable: true, Diagnostic: "malformed-response"}
	}
	if code == 200 {
		return Result{Accepted: true}
	}
	diagnostic := "business-client"
	if code >= 500 && code <= 599 {
		diagnostic = "business-server"
	}
	return Result{Retryable: retryableStatus(code), Diagnostic: diagnostic}
}

func ntfyResult(object map[string]json.RawMessage, topic string) Result {
	id, idOK := object["id"]
	event, eventOK := object["event"]
	responseTopic, topicOK := object["topic"]
	if !idOK || !eventOK || !topicOK {
		return Result{Retryable: true, Diagnostic: "malformed-response"}
	}
	idValue, idErr := strictjson.String(id)
	eventValue, eventErr := strictjson.String(event)
	topicValue, topicErr := strictjson.String(responseTopic)
	if idErr != nil || eventErr != nil || topicErr != nil || strings.TrimSpace(idValue) == "" || eventValue != "message" || topicValue != topic {
		return Result{Retryable: true, Diagnostic: "malformed-response"}
	}
	return Result{Accepted: true}
}
