package provider

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestForEachSSEExtractsDataPayloads(t *testing.T) {
	body := "event: message_start\ndata: {\"a\":1}\n\n" +
		"data: {\"b\":2}\n\n" +
		"data: [DONE]\n\n"

	var got []string
	err := ForEachSSE(strings.NewReader(body), func(data string) error {
		if data == "[DONE]" {
			return io.EOF
		}
		got = append(got, data)
		return nil
	})
	if err != nil {
		t.Fatalf("ForEachSSE() = %v", err)
	}
	want := []string{`{"a":1}`, `{"b":2}`}
	if len(got) != len(want) {
		t.Fatalf("payloads = %v, want %v", got, want)
	}
	var parsed map[string]int
	if err := json.Unmarshal([]byte(got[0]), &parsed); err != nil || parsed["a"] != 1 {
		t.Fatalf("payload[0] = %q, err = %v", got[0], err)
	}
}

func TestForEachSSECleanEndsWithoutSentinel(t *testing.T) {
	err := ForEachSSE(strings.NewReader("data: x\n\n"), func(data string) error {
		if data == "x" {
			return io.EOF
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ForEachSSE() = %v, want nil", err)
	}
}

func TestForEachSSEPropagatesCallbackError(t *testing.T) {
	boom := &errSentinel{}
	err := ForEachSSE(strings.NewReader("data: x\n\n"), func(data string) error {
		return boom
	})
	if err != boom {
		t.Fatalf("ForEachSSE() = %v, want callback error", err)
	}
}

type errSentinel struct{}

func (*errSentinel) Error() string { return "sentinel" }