package rapid

import (
	"encoding/json"
	"fmt"
	"strings"
)

type RapidReport struct {
	Summary              string   `json:"summary"`
	Decision             string   `json:"decision"`
	Recommend            string   `json:"recommend"`
	Agree                string   `json:"agree"`
	Perform              string   `json:"perform"`
	Input                string   `json:"input"`
	Decide               string   `json:"decide"`
	Rationale            string   `json:"rationale"`
	RisksOrOpenQuestions string   `json:"risks_or_open_questions"`
	ActionItems          []string `json:"action_items"`
	EvidenceSegmentIDs   []string `json:"evidence_segment_ids"`
}

func MarkdownFromJSON(payload string) (string, error) {
	var report RapidReport
	if err := json.Unmarshal([]byte(payload), &report); err != nil {
		return "", fmt.Errorf("decode rapid report: %w", err)
	}
	return Markdown(report), nil
}

func Markdown(report RapidReport) string {
	sections := []string{
		"# RAPID Report",
		fmt.Sprintf("## Summary\n\n%s", fallback(report.Summary)),
		fmt.Sprintf("## Decision\n\n%s", fallback(report.Decision)),
		fmt.Sprintf("## Recommend\n\n%s", fallback(report.Recommend)),
		fmt.Sprintf("## Agree\n\n%s", fallback(report.Agree)),
		fmt.Sprintf("## Perform\n\n%s", fallback(report.Perform)),
		fmt.Sprintf("## Input\n\n%s", fallback(report.Input)),
		fmt.Sprintf("## Decide\n\n%s", fallback(report.Decide)),
		fmt.Sprintf("## Rationale\n\n%s", fallback(report.Rationale)),
		fmt.Sprintf("## Risks Or Open Questions\n\n%s", fallback(report.RisksOrOpenQuestions)),
	}

	if len(report.ActionItems) == 0 {
		sections = append(sections, "## Action Items\n\n- Not established in discussion")
	} else {
		lines := make([]string, 0, len(report.ActionItems))
		for _, item := range report.ActionItems {
			lines = append(lines, "- "+fallback(item))
		}
		sections = append(sections, "## Action Items\n\n"+strings.Join(lines, "\n"))
	}

	if len(report.EvidenceSegmentIDs) > 0 {
		sections = append(sections, "## Evidence Segment IDs\n\n"+strings.Join(report.EvidenceSegmentIDs, ", "))
	}

	return strings.Join(sections, "\n\n")
}

func fallback(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "Not established in discussion"
	}
	return trimmed
}
