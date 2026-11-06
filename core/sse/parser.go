package sse

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

const maxLineBytes = 16 << 20

type Frame struct {
	Event string
	Data  string
}

// Parse follows the SSE field rules and supports large DeepSeek search/file
// events without Scanner's small default token limit.
func Parse(r io.Reader, yield func(Frame) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), maxLineBytes)
	var frame Frame
	hasFields := false
	flush := func() error {
		if hasFields {
			if err := yield(frame); err != nil {
				return err
			}
		}
		frame = Frame{}
		hasFields = false
		return nil
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			frame.Event = value
			hasFields = true
		case "data":
			if frame.Data != "" {
				frame.Data += "\n"
			}
			frame.Data += value
			hasFields = true
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read SSE: %w", err)
	}
	return flush()
}
