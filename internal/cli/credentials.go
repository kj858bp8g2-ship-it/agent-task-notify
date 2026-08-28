package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/providers"
	"golang.org/x/term"
)

type hiddenReader func() ([]byte, error)

// The only terminal boundary. Tests inject a reader into readCredential, not
// a secret production argument or a substitute crypto/configuration backend.
func terminalReader(input io.Reader) hiddenReader {
	file, ok := input.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return nil
	}
	return func() ([]byte, error) { return term.ReadPassword(int(file.Fd())) }
}

func readCredential(provider string, input io.Reader, prompt io.Writer, explicit bool, hidden hiddenReader) (providers.Credential, error) {
	if explicit {
		data, err := readBounded(input)
		if err != nil {
			return providers.Credential{}, errInput
		}
		defer clear(data)
		credential, err := providers.ParseCredential(provider, data)
		if err != nil {
			return providers.Credential{}, errInput
		}
		if credential.AllowUnauthenticated {
			fmt.Fprintln(prompt, "Warning: unauthenticated ntfy topics are not private; an obscure topic is not access control.")
		}
		return credential, nil
	}
	if hidden == nil {
		return providers.Credential{}, errInput
	}
	read := func(label string) (string, error) {
		fmt.Fprint(prompt, label)
		data, err := hidden()
		defer clear(data)
		fmt.Fprintln(prompt)
		if err != nil || !utf8.Valid(data) || len(data) > 4096 {
			return "", errInput
		}
		return string(data), nil
	}
	endpoint, err := read("Endpoint (hidden, local only): ")
	if err != nil {
		return providers.Credential{}, errInput
	}
	credential := providers.Credential{Endpoint: endpoint}
	if provider == "ntfy" {
		credential.Token, err = read("Token (hidden; empty requires explicit public-topic consent): ")
		if err != nil {
			return providers.Credential{}, errInput
		}
		if strings.TrimSpace(credential.Token) == "" {
			ack, err := read("Warning: unauthenticated topics are not private. Type true to allow: ")
			if err != nil || ack != "true" {
				return providers.Credential{}, errInput
			}
			credential.AllowUnauthenticated = true
		}
	}
	if providers.ValidateCredential(provider, credential) != nil {
		return providers.Credential{}, errInput
	}
	return credential, nil
}
