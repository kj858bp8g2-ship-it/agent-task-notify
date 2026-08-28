package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// Catches accepting credential fields through a looser parser, accidental
// prompt/echo in stdin mode, or accepting unacknowledged public ntfy topics.
func TestCredentialInputBoundary(t *testing.T) {
	for _, tc := range []struct {
		provider, input string
		valid           bool
	}{{"bark", `{"endpoint":"http://127.0.0.1/synthetic-key"}`, true}, {"ntfy", `{"endpoint":"http://127.0.0.1/synthetic-topic","token":"synthetic-token"}`, true}, {"ntfy", `{"endpoint":"http://127.0.0.1/synthetic-topic","allowUnauthenticated":true}`, true}, {"ntfy", `{"endpoint":"http://127.0.0.1/synthetic-topic"}`, false}, {"bark", `{"endpoint":"http://127.0.0.1/synthetic-key","token":"synthetic-token"}`, false}, {"bark", `{"endpoint":"http://127.0.0.1/synthetic-key","endpoint":"other"}`, false}, {"bark", `{"endpoint":"http://127.0.0.1/synthetic-key","extra":true}`, false}, {"bark", strings.Repeat("x", (4<<20)+1), false}} {
		var out bytes.Buffer
		_, err := readCredential(tc.provider, strings.NewReader(tc.input), &out, true, nil)
		if (err == nil) != tc.valid || strings.Contains(out.String(), "synthetic-") {
			t.Fatal("unsafe stdin credential handling")
		}
	}
}

func TestHiddenCredentialPromptsDoNotEcho(t *testing.T) {
	for _, ack := range []string{"true", "false", "yes"} {
		values := []string{"http://127.0.0.1/synthetic-topic", "", ack}
		i := 0
		reader := func() ([]byte, error) {
			if i >= len(values) {
				t.Fatal("unexpected prompt")
			}
			b := []byte(values[i])
			i++
			return b, nil
		}
		var out bytes.Buffer
		c, err := readCredential("ntfy", forbiddenInput{}, &out, false, reader)
		if (err == nil) != (ack == "true") || strings.Contains(out.String(), "synthetic-topic") || !strings.Contains(out.String(), "not private") {
			t.Fatal("hidden input/privacy boundary")
		}
		if err == nil && !c.AllowUnauthenticated {
			t.Fatal("explicit true not retained")
		}
	}
	var out bytes.Buffer
	_, err := readCredential("bark", forbiddenInput{}, &out, false, func() ([]byte, error) { return nil, errors.New("planted-secret") })
	if err == nil || strings.Contains(out.String()+err.Error(), "planted-secret") {
		t.Fatal("terminal error leaked")
	}
	_, err = readCredential("bark", forbiddenInput{}, &out, false, nil)
	if err == nil {
		t.Fatal("nonterminal accepted")
	}
}
