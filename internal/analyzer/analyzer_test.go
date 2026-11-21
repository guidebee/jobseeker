package analyzer

import (
	"strings"
	"testing"
)

const body = `{"match_score": 72, "reasoning": "fits", "pros": ["a", "b"], "cons": ["c"]}`

func TestParseAnalysisResponseAcceptsEveryShapeTheModelUses(t *testing.T) {
	cases := map[string]string{
		"plain":                      body,
		"fenced":                     "```json\n" + body + "\n```",
		"fenced without a language":  "```\n" + body + "\n```",
		"fence then a note":          "```json\n" + body + "\n```\n\nNote: the salary was not stated.",
		"a sentence then the fence":  "Here is the analysis:\n```json\n" + body + "\n```",
		"a reasoning block first":    "<think>weighing the skills</think>\n" + body,
		"reasoning block and fence":  "<think>x</think>\n```json\n" + body + "\n```\nHope this helps.",
		"trailing text after plain":  body + "\n\nLet me know if you need more.",
		"leading and trailing space": "  \n" + body + "  \n",
	}
	a := &Analyzer{}
	for name, response := range cases {
		got, err := a.parseAnalysisResponse(response)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got.MatchScore != 72 || len(got.Pros) != 2 || len(got.Cons) != 1 || got.Reasoning != "fits" {
			t.Errorf("%s: parsed %+v", name, got)
		}
	}
}

func TestParseAnalysisResponseRejectsAnswersWithoutAUsableObject(t *testing.T) {
	a := &Analyzer{}
	for name, response := range map[string]string{
		"empty":         "",
		"prose only":    "I could not score this listing.",
		"broken object": `{"match_score": 72, "reasoning": `,
		"wrong types":   `{"match_score": "high"}`,
	} {
		if _, err := a.parseAnalysisResponse(response); err == nil || !strings.Contains(err.Error(), "failed to parse JSON") {
			t.Errorf("%s: error = %v", name, err)
		}
	}
}
