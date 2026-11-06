package sse

import (
	"strings"
	"testing"
)

func TestParseMultiLineFrame(t *testing.T) {
	var got []Frame
	err := Parse(strings.NewReader(": ping\nevent: ready\ndata: {\ndata: \"id\":1\n\nevent: close\ndata: done\n"), func(f Frame) error {
		got = append(got, f)
		return nil
	})
	if err != nil || len(got) != 2 || got[0].Data != "{\n\"id\":1" || got[1].Event != "close" {
		t.Fatalf("Parse() = %#v, %v", got, err)
	}
}
