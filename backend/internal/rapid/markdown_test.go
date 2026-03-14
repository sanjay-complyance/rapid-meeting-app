package rapid

import (
	"strings"
	"testing"
)

func TestMarkdownAddsFallbacks(t *testing.T) {
	markdown := Markdown(RapidReport{
		Summary:     "Ready",
		ActionItems: []string{"Owner to follow up"},
	})

	if markdown == "" {
		t.Fatal("expected markdown output")
	}
	if !strings.Contains(markdown, "Not established in discussion") {
		t.Fatal("expected fallback text in markdown")
	}
}
