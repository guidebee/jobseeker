package analyzer

import (
	"strings"
	"testing"

	"github.com/guidebee/jobseeker/internal/database"
	"github.com/guidebee/jobseeker/internal/profile"
	"github.com/guidebee/jobseeker/internal/resume"
)

func TestHasUsableDescriptionNeedsRealAdvertText(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"empty", "", false},
		{"spaces only", strings.Repeat(" ", 500), false},
		{"a title-length line", "Senior Engineer at Acme", false},
		{"one short of the minimum", strings.Repeat("a", MinDescriptionChars-1), false},
		{"exactly the minimum", strings.Repeat("a", MinDescriptionChars), true},
		{"a normal advert", strings.Repeat("Build and run services. ", 60), true},
	}
	for _, c := range cases {
		if got := HasUsableDescription(&database.Job{Description: c.text}); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func analyzerWith(content string) *Analyzer {
	p := &profile.Profile{}
	return &Analyzer{profile: p, resumes: []*resume.Resume{{Filename: "cv.docx", Content: content}}, useResumes: true}
}

func TestPromptCarriesTheResumeAsTextUpToTheLimit(t *testing.T) {
	// A resume longer than the old 2,000-character cut: its experience (the end) must still reach the model.
	body := strings.Repeat("Led platform teams and shipped services. ", 100) // about 4,000 characters
	prompt := analyzerWith(body + "FINAL-ROLE-MARKER").buildResumeBasedPrompt(&database.Job{Title: "Engineer"}, "any pay")
	if !strings.Contains(prompt, "FINAL-ROLE-MARKER") {
		t.Fatal("the end of a 4,000-character resume was cut off")
	}
	if len(body) <= 2000 {
		t.Fatal("the test resume must be longer than the old limit")
	}
}

func TestPromptStillCutsAVeryLongResumeAndDescription(t *testing.T) {
	long := strings.Repeat("§", MaxResumeChars+500) // a character the template never uses
	prompt := analyzerWith(long).buildResumeBasedPrompt(&database.Job{Title: "Engineer", Description: strings.Repeat("¶", MaxDescriptionChars+500)}, "pay")
	if strings.Count(prompt, "§") != MaxResumeChars {
		t.Errorf("the resume was not limited to %d characters: %d", MaxResumeChars, strings.Count(prompt, "§"))
	}
	if strings.Count(prompt, "¶") != MaxDescriptionChars {
		t.Errorf("the description was not limited to %d characters: %d", MaxDescriptionChars, strings.Count(prompt, "¶"))
	}
}

func TestPromptHoldsNoDocumentMarkup(t *testing.T) {
	// What the loader now produces: text. The prompt must not add markup of its own around the resume.
	prompt := analyzerWith("Jane Doe\nSenior engineer").buildResumeBasedPrompt(&database.Job{Title: "Engineer"}, "pay")
	if strings.Contains(prompt, "<w:") || !strings.Contains(prompt, "Jane Doe\nSenior engineer") {
		t.Fatalf("prompt: %q", prompt)
	}
}

func TestTruncateCountsCharactersAndNeverSplitsOne(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Fatalf("got %q", got)
	}
	if got := truncate("abcdef", 3); got != "abc..." {
		t.Fatalf("got %q", got)
	}
	text := strings.Repeat("–é", 10) // multi-byte characters
	got := truncate(text, 5)
	if got != "–é–é–..." {
		t.Fatalf("got %q", got)
	}
	for _, r := range got {
		if r == '�' {
			t.Fatalf("a character was split: %q", got)
		}
	}
}
