package llm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Stream errors never expose a partial model result or raw reasoning.
type streamError struct{ reason string }

func (e *streamError) Error() string { return e.reason }

func readChatStream(reader io.Reader) ([]byte, error) {
	scanner := bufio.NewScanner(io.LimitReader(reader, 64*1024*1024+1))
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	var lines []string
	var content bytes.Buffer
	finish := ""
	wireBytes := 0
	for scanner.Scan() {
		line := scanner.Text()
		wireBytes += len(line) + 1
		if wireBytes > 64*1024*1024 {
			return nil, &streamError{"stream exceeds safety size limit"}
		}
		if strings.HasPrefix(line, "data:") {
			lines = append(lines, strings.TrimLeft(line[5:], " "))
			continue
		}
		if line != "" || len(lines) == 0 {
			continue
		}
		data := strings.Join(lines, "\n")
		lines = nil
		if data == "[DONE]" {
			if finish != "stop" {
				return nil, &streamError{"incomplete model output: finish_reason is not stop"}
			}
			return json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content.String()}}}})
		}
		var event struct {
			Error   json.RawMessage `json:"error"`
			Choices []struct {
				Index int `json:"index"`
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return nil, &streamError{"invalid stream event"}
		}
		if len(event.Error) > 0 && string(event.Error) != "null" {
			return nil, &streamError{"relay stream error event"}
		}
		for _, choice := range event.Choices {
			if choice.Index != 0 {
				continue
			}
			if content.Len()+len(choice.Delta.Content) > 8*1024*1024 {
				return nil, &streamError{"model content exceeds safety size limit"}
			}
			content.WriteString(choice.Delta.Content)
			if choice.FinishReason != "" {
				finish = choice.FinishReason
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("stream transport interrupted: %w", err)
	}
	return nil, &streamError{"incomplete stream: missing [DONE]"}
}
