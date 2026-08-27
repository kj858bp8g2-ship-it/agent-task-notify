package providers_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/core"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/providers"
)

func defaults(t *testing.T, p string) core.Settings {
	t.Helper()
	s, e := core.Defaults()
	if e != nil {
		t.Fatal(e)
	}
	s.Provider = p
	return s
}

func TestNtfyCannotRequestPhoneCall(t *testing.T) {
	var body map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Error("wrong publish path")
		}
		if r.Header.Get("Call") != "" || r.Header.Get("X-Call") != "" {
			t.Error("phone header")
		}
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		_, _ = io.WriteString(w, `{"id":"synthetic","event":"message","topic":"synthetic"}`)
	}))
	defer server.Close()
	r := providers.Send(context.Background(), defaults(t, "ntfy"), providers.Credential{Endpoint: server.URL + "/synthetic", AllowUnauthenticated: true}, providers.Message{AgentID: "codex", DurationSeconds: 3600})
	if !r.Accepted {
		t.Fatal(r.Diagnostic)
	}
	for k := range body {
		if !map[string]bool{"topic": true, "title": true, "message": true, "priority": true, "icon": true}[k] {
			t.Fatal("unexpected field", k)
		}
	}
}

func TestPayloadWhitelistsAllAgentsAndVariants(t *testing.T) {
	for _, p := range []string{"bark", "ntfy"} {
		p := p
		t.Run(p, func(t *testing.T) {
			var bodies []map[string]json.RawMessage
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var b map[string]json.RawMessage
				if e := json.NewDecoder(r.Body).Decode(&b); e != nil {
					t.Error(e)
				}
				bodies = append(bodies, b)
				if p == "bark" {
					_, _ = io.WriteString(w, `{"code":200}`)
				} else {
					_, _ = io.WriteString(w, `{"id":"id","event":"message","topic":"topic"}`)
				}
			}))
			defer server.Close()
			s := defaults(t, p)
			s.Icons["codex"] = "https://example.test/codex.png"
			ids := []string{"codex", "claude-code", "cursor", "gemini-cli", "opencode", "workbuddy"}
			for _, id := range ids {
				c := providers.Credential{Endpoint: server.URL + "/topic", AllowUnauthenticated: true}
				if p == "bark" {
					c.Endpoint = server.URL + "/base/key"
					c.AllowUnauthenticated = false
				}
				if r := providers.Send(context.Background(), s, c, providers.Message{AgentID: id, DurationSeconds: 125}); !r.Accepted {
					t.Fatal(id, r.Diagnostic)
				}
			}
			allowed := map[string]bool{"topic": true, "title": true, "message": true, "priority": true, "icon": true}
			if p == "bark" {
				allowed = map[string]bool{"title": true, "body": true, "group": true, "level": true, "volume": true, "sound": true, "isArchive": true, "icon": true, "call": true}
			}
			for index, b := range bodies {
				for k := range b {
					if !allowed[k] {
						t.Fatal("unexpected", p, k)
					}
				}
				agent, err := core.AgentByID(ids[index])
				if err != nil {
					t.Fatal(err)
				}
				if string(b["title"]) != `"`+agent.DisplayName+` 长任务已停止"` {
					t.Fatalf("wrong display name for %s: %s", ids[index], b["title"])
				}
				if _, present := b["icon"]; !present {
					t.Fatalf("missing icon for %s", ids[index])
				}
				if ids[index] == "codex" && string(b["icon"]) != `"https://example.test/codex.png"` {
					t.Fatalf("icon override was not preserved: %s", b["icon"])
				}
			}
		})
	}
	var bodies []map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&b)
		bodies = append(bodies, b)
		_, _ = io.WriteString(w, `{"code":200}`)
	}))
	defer server.Close()
	s, c := defaults(t, "bark"), providers.Credential{Endpoint: server.URL + "/key"}
	for _, m := range []providers.Message{{AgentID: "codex", DurationSeconds: 125}, {AgentID: "codex", DurationSeconds: 125, Reason: "attention"}, {AgentID: "codex", Preview: true}} {
		if r := providers.Send(context.Background(), s, c, m); !r.Accepted {
			t.Fatal(r.Diagnostic)
		}
	}
	const terminalBody = `"任务已停止，请查看应用。耗时约 2 分钟。"`
	if string(bodies[0]["call"]) != "1" || string(bodies[0]["title"]) != `"Codex 长任务已停止"` || string(bodies[0]["body"]) != terminalBody || string(bodies[1]["title"]) != `"Codex 任务需要关注"` || string(bodies[1]["body"]) != terminalBody || string(bodies[2]["title"]) != `"Codex 通知预览"` || string(bodies[2]["body"]) != `"这是一条通用测试通知。"` {
		t.Fatal("copy/continuous mismatch")
	}
	s.Continuous = false
	if r := providers.Send(context.Background(), s, c, providers.Message{AgentID: "codex"}); !r.Accepted {
		t.Fatal(r.Diagnostic)
	}
	if _, ok := bodies[3]["call"]; ok {
		t.Fatal("single bark call")
	}
	s.Icons["codex"] = ""
	if r := providers.Send(context.Background(), s, c, providers.Message{AgentID: "codex"}); !r.Accepted {
		t.Fatal(r.Diagnostic)
	}
	if _, ok := bodies[4]["icon"]; ok {
		t.Fatal("disabled icon")
	}
}

func TestCredentialBoundaryAndNoLeakedMarkers(t *testing.T) {
	const endpoint, token = "secret-endpoint-marker", "secret-token-marker"
	for _, raw := range []string{`{"endpoint":"https://` + endpoint + `.test/topic","token":"` + token + `","allowUnauthenticated":false,"extra":true}`, `{"endpoint":"https://` + endpoint + `.test/topic","endpoint":"https://duplicate.test/topic","token":"` + token + `","allowUnauthenticated":false}`, `{"endpoint":"https://` + endpoint + `.test/topic","token":true,"allowUnauthenticated":false}`, `{"endpoint":"https://` + endpoint + `.test/topic","token":"` + token + `","allowUnauthenticated":"false"}`, `{"endpoint":"https://` + endpoint + `.test/topic","token":"","allowUnauthenticated":false}`} {
		_, e := providers.ParseCredential("ntfy", []byte(raw))
		if e == nil || strings.Contains(e.Error(), endpoint) || strings.Contains(e.Error(), token) {
			t.Fatal("credential leak", e)
		}
	}
	for _, tc := range []struct {
		raw  string
		want providers.Credential
	}{
		{`{"endpoint":"https://example.test/topic","token":"synthetic"}`, providers.Credential{Endpoint: "https://example.test/topic", Token: "synthetic"}},
		{`{"endpoint":"https://example.test/topic","allowUnauthenticated":true}`, providers.Credential{Endpoint: "https://example.test/topic", AllowUnauthenticated: true}},
	} {
		credential, e := providers.ParseCredential("ntfy", []byte(tc.raw))
		if e != nil || credential != tc.want {
			t.Fatalf("valid optional ntfy credential %s: %#v %v", tc.raw, credential, e)
		}
	}
	for _, v := range []string{"http://example.test/topic", "https://user:pass@example.test/topic", "https://example.test/topic?q=1", "https://example.test/topic#f", "https://example.test/topic?", "https://example.test/topic#", "https://example.test/a%2Fb", "https://example.test/a\\b", "https://example.test/a b", "https://example.test/a/../topic", "https://example.test/./topic", "https://example.test/topic/", "https://example.test/a/b"} {
		if e := providers.ValidateCredential("ntfy", providers.Credential{Endpoint: v, Token: "token"}); e == nil {
			t.Fatal("unsafe", v)
		}
	}
	for _, v := range []string{"https://example.test/", "https://example.test/a/../b"} {
		if e := providers.ValidateCredential("bark", providers.Credential{Endpoint: v}); e == nil {
			t.Fatal("unsafe bark", v)
		}
	}
	if e := providers.ValidateCredential("ntfy", providers.Credential{Endpoint: "http://localhost:8080/topic", AllowUnauthenticated: true}); e != nil {
		t.Fatal(e)
	}
	if e := providers.ValidateCredential("ntfy", providers.Credential{Endpoint: "http://127.0.0.1:8080/topic", Token: "bad\r\ntoken"}); e == nil {
		t.Fatal("CRLF token")
	}
	if e := providers.ValidateCredential("ntfy", providers.Credential{Endpoint: "https://example.test/topic", Token: strings.Repeat("a", 4097)}); e == nil {
		t.Fatal("oversized token")
	}
	if e := providers.ValidateCredential("ntfy", providers.Credential{Endpoint: "https://example.test/" + strings.Repeat("a", 4097), Token: "token"}); e == nil {
		t.Fatal("oversized endpoint")
	}
	if e := providers.ValidateCredential("ntfy", providers.Credential{Endpoint: "https://example.test/topic", Token: " \t "}); e == nil {
		t.Fatal("whitespace token without consent")
	}
}

func TestResponsesRedirectOversizeTransportAndCancellation(t *testing.T) {
	const secret = "secret-response-marker"
	redirects := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects++ }))
	defer target.Close()
	cases := []struct {
		name, p, body string
		status        int
		retry         bool
	}{{"bark ok", "bark", `{"code":200}`, 200, false}, {"bark 400", "bark", `{"code":400}`, 200, false}, {"bark 408", "bark", `{"code":408}`, 200, true}, {"bark 503", "bark", `{"code":503}`, 200, true}, {"bark type", "bark", `{"code":"200"}`, 200, true}, {"ntfy ok", "ntfy", `{"id":"id","event":"message","topic":"topic"}`, 200, false}, {"missing", "ntfy", `{"event":"message","topic":"topic"}`, 200, true}, {"wrong", "ntfy", `{"id":"id","event":"message","topic":"other"}`, 200, true}, {"wrong types", "ntfy", `{"id":3,"event":true,"topic":[]}`, 200, true}, {"duplicate", "ntfy", `{"id":"id","id":"two","event":"message","topic":"topic"}`, 200, true}, {"malformed", "ntfy", secret, 200, true}, {"400", "ntfy", secret, 400, false}, {"408", "ntfy", secret, 408, true}, {"425", "ntfy", secret, 425, true}, {"429", "ntfy", secret, 429, true}, {"500", "ntfy", secret, 500, true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			c := providers.Credential{Endpoint: srv.URL + "/topic", AllowUnauthenticated: true}
			if tc.p == "bark" {
				c.Endpoint = srv.URL + "/key"
				c.AllowUnauthenticated = false
			}
			r := providers.Send(context.Background(), defaults(t, tc.p), c, providers.Message{AgentID: "codex"})
			if strings.HasSuffix(tc.name, "ok") {
				if !r.Accepted {
					t.Fatal(r.Diagnostic)
				}
			} else if r.Accepted || r.Retryable != tc.retry || strings.Contains(r.Diagnostic, secret) {
				t.Fatalf("%+v", r)
			}
		})
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(302)
	}))
	defer redirect.Close()
	r := providers.Send(context.Background(), defaults(t, "ntfy"), providers.Credential{Endpoint: redirect.URL + "/topic", AllowUnauthenticated: true}, providers.Message{AgentID: "codex"})
	if r.Accepted || r.Retryable || redirects != 0 {
		t.Fatalf("redirect %+v %d", r, redirects)
	}
	over := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"code":200,"padding":"`+strings.Repeat("x", 65537)+`"}`)
	}))
	defer over.Close()
	r = providers.Send(context.Background(), defaults(t, "bark"), providers.Credential{Endpoint: over.URL + "/key"}, providers.Message{AgentID: "codex"})
	if r.Accepted || !r.Retryable || r.Diagnostic != "malformed-response" {
		t.Fatalf("oversize %+v", r)
	}
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _, e := w.(http.Hijacker).Hijack()
		if e == nil {
			_ = c.Close()
		}
	}))
	defer broken.Close()
	r = providers.Send(context.Background(), defaults(t, "bark"), providers.Credential{Endpoint: broken.URL + "/key"}, providers.Message{AgentID: "codex"})
	if r.Accepted || !r.Retryable || r.Diagnostic != "transport" {
		t.Fatalf("transport %+v", r)
	}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	addr := l.Addr().String()
	_ = l.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r = providers.Send(ctx, defaults(t, "bark"), providers.Credential{Endpoint: "http://" + addr + "/key"}, providers.Message{AgentID: "codex"})
	if r.Accepted || r.Retryable || r.Diagnostic != "transport" {
		t.Fatalf("canceled %+v", r)
	}
}
