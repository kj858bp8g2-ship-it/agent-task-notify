package strictjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestObjectRejectsMalformedAndDuplicateInput(t *testing.T) {
	tooDeep := strings.Repeat(`{"x":`, 65) + `0` + strings.Repeat(`}`, 65)
	cases := [][]byte{
		[]byte(`{"x":1,"x":2}`), []byte(`{"x":{"y":1,"y":2}}`), []byte(`{"x":[{"y":1,"y":2}]}`),
		[]byte(`[]`), []byte(`null`), []byte(`{"x":1} {}`), []byte(`{"x":`), []byte(tooDeep),
		append([]byte(`{"x":"`), append([]byte{0xff}, []byte(`"}`)...)...), bytes.Repeat([]byte{' '}, MaxBytes+1),
	}
	for _, data := range cases {
		if _, err := Object(data); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted or leaked parser error for %q: %v", data[:min(len(data), 32)], err)
		}
	}
	if got, err := Object([]byte(`{"x":[1,{"y":true}]}`)); err != nil || string(got["x"]) != `[1,{"y":true}]` {
		t.Fatalf("got=%s err=%v", got["x"], err)
	}
}

func TestScalarHelpersAreStrict(t *testing.T) {
	if got, err := String([]byte(`"ok"`)); err != nil || got != "ok" {
		t.Fatalf("string=%q err=%v", got, err)
	}
	if got, err := Integer([]byte(`-9223372036854775808`)); err != nil || got != -9223372036854775808 {
		t.Fatalf("integer=%d err=%v", got, err)
	}
	if got, err := Boolean([]byte(`true`)); err != nil || !got {
		t.Fatalf("boolean=%t err=%v", got, err)
	}
	for _, raw := range []string{`null`, `"1"`, `1.5`, `1e2`, `9223372036854775808`, `01`} {
		if _, err := Integer([]byte(raw)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("integer accepted %s: %v", raw, err)
		}
	}
	for _, raw := range []string{`null`, `1`, `true`} {
		if _, err := String([]byte(raw)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("string accepted %s: %v", raw, err)
		}
	}
	if _, err := String(json.RawMessage{0xff}); !errors.Is(err, ErrInvalid) {
		t.Fatal("string accepted invalid UTF-8")
	}
	for _, raw := range []string{`null`, `"true"`, `1`} {
		if _, err := Boolean([]byte(raw)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("boolean accepted %s: %v", raw, err)
		}
	}
}

func TestObjectRetainsUnknownNumericLexemes(t *testing.T) {
	for _, number := range []string{"1e400", "-1.23400e-999", "9007199254740993123456789"} {
		object, err := Object([]byte(`{"unknown":{"value":` + number + `}}`))
		if err != nil || string(object["unknown"]) != `{"value":`+number+`}` {
			t.Fatal("unknown JSON number rejected or rounded")
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
