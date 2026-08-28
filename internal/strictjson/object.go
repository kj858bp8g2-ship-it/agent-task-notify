// Package strictjson parses the small JSON objects accepted at native boundaries.
package strictjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"unicode/utf8"
)

const MaxBytes = 4 << 20

var ErrInvalid = errors.New("invalid JSON")

var integer = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)$`)

// Object accepts one UTF-8 JSON object, rejects duplicate keys at every depth,
// and never exposes parser details to boundary callers.
func Object(data []byte) (map[string]json.RawMessage, error) {
	if len(data) > MaxBytes || !utf8.Valid(data) {
		return nil, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return nil, ErrInvalid
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' || scanObject(decoder, 1) != nil {
		return nil, ErrInvalid
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, ErrInvalid
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return nil, ErrInvalid
	}
	return object, nil
}

func scanObject(decoder *json.Decoder, depth int) error {
	if depth > 64 {
		return ErrInvalid
	}
	keys := make(map[string]struct{})
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok {
			return ErrInvalid
		}
		if _, exists := keys[name]; exists {
			return ErrInvalid
		}
		keys[name] = struct{}{}
		if err := scanValue(decoder, depth+1); err != nil {
			return ErrInvalid
		}
	}
	_, err := decoder.Token()
	return err
}

func scanArray(decoder *json.Decoder, depth int) error {
	if depth > 64 {
		return ErrInvalid
	}
	for decoder.More() {
		if err := scanValue(decoder, depth+1); err != nil {
			return ErrInvalid
		}
	}
	_, err := decoder.Token()
	return err
}

func scanValue(decoder *json.Decoder, depth int) error {
	if depth > 64 {
		return ErrInvalid
	}
	token, err := decoder.Token()
	if err != nil {
		return ErrInvalid
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		return scanObject(decoder, depth)
	case '[':
		return scanArray(decoder, depth)
	default:
		return ErrInvalid
	}
}

func String(value json.RawMessage) (string, error) {
	var result string
	if len(value) == 0 || !utf8.Valid(value) || bytes.Equal(value, []byte("null")) || json.Unmarshal(value, &result) != nil {
		return "", ErrInvalid
	}
	return result, nil
}

func Integer(value json.RawMessage) (int64, error) {
	if !integer.Match(value) {
		return 0, ErrInvalid
	}
	result, err := strconv.ParseInt(string(value), 10, 64)
	if err != nil {
		return 0, ErrInvalid
	}
	return result, nil
}

func Boolean(value json.RawMessage) (bool, error) {
	switch string(value) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, ErrInvalid
	}
}
