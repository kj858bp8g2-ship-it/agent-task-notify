package providers

import (
	"errors"
	"net"
	"net/url"
	"strings"
	"unicode"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/strictjson"
)

const maxCredentialBytes = 4096

var errCredential = errors.New("invalid provider credential")

type Credential struct {
	Endpoint             string `json:"endpoint"`
	Token                string `json:"token,omitempty"`
	AllowUnauthenticated bool   `json:"allowUnauthenticated,omitempty"`
}

// ParseCredential accepts only the provider-specific credential boundary fields.
func ParseCredential(provider string, data []byte) (Credential, error) {
	object, err := strictjson.Object(data)
	if err != nil {
		return Credential{}, errCredential
	}
	if provider != "bark" && provider != "ntfy" {
		return Credential{}, errCredential
	}
	endpointRaw, ok := object["endpoint"]
	if !ok {
		return Credential{}, errCredential
	}
	endpoint, err := strictjson.String(endpointRaw)
	if err != nil {
		return Credential{}, errCredential
	}
	credential := Credential{Endpoint: endpoint}
	for key, value := range object {
		switch key {
		case "endpoint":
		case "token":
			if provider != "ntfy" {
				return Credential{}, errCredential
			}
			credential.Token, err = strictjson.String(value)
			if err != nil {
				return Credential{}, errCredential
			}
		case "allowUnauthenticated":
			if provider != "ntfy" {
				return Credential{}, errCredential
			}
			credential.AllowUnauthenticated, err = strictjson.Boolean(value)
			if err != nil {
				return Credential{}, errCredential
			}
		default:
			return Credential{}, errCredential
		}
	}
	if err := ValidateCredential(provider, credential); err != nil {
		return Credential{}, errCredential
	}
	return credential, nil
}

func ValidateCredential(provider string, credential Credential) error {
	if provider != "bark" && provider != "ntfy" {
		return errCredential
	}
	if len(credential.Endpoint) == 0 || len(credential.Endpoint) > maxCredentialBytes || strings.IndexFunc(credential.Endpoint, unicode.IsSpace) >= 0 || strings.ContainsAny(credential.Endpoint, "\\%?#") {
		return errCredential
	}
	if provider == "bark" && (credential.Token != "" || credential.AllowUnauthenticated) {
		return errCredential
	}
	if provider == "ntfy" {
		if len(credential.Token) > maxCredentialBytes || strings.ContainsAny(credential.Token, "\r\n") || (strings.TrimSpace(credential.Token) == "" && !credential.AllowUnauthenticated) {
			return errCredential
		}
	}
	_, err := credentialURL(provider, credential.Endpoint)
	if err != nil {
		return errCredential
	}
	return nil
}

func credentialURL(provider, endpoint string) (*url.URL, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed == nil || parsed.Opaque != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Host == "" {
		return nil, errCredential
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopbackHost(parsed.Hostname())) {
		return nil, errCredential
	}
	path := strings.TrimPrefix(parsed.Path, "/")
	if path == "" || strings.HasSuffix(parsed.Path, "/") {
		return nil, errCredential
	}
	segments := strings.Split(path, "/")
	if provider == "ntfy" && len(segments) != 1 {
		return nil, errCredential
	}
	for _, segment := range segments {
		if segment == "" || !validSegment(segment) {
			return nil, errCredential
		}
	}
	return parsed, nil
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validSegment(segment string) bool {
	for _, char := range segment {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-') {
			return false
		}
	}
	return true
}
