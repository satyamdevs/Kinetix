package provider

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// ForEachSSE reads an SSE (text/event-stream) body and invokes fn for every
// "data:" payload field in order. Returning io.EOF from fn signals normal
// completion (e.g. the OpenAI "[DONE]" sentinel) and stops scanning without error.
func ForEachSSE(r io.Reader, fn func(data string) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		err := fn(payload)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan sse stream: %w", err)
	}
	return nil
}