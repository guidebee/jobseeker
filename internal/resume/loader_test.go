package resume

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A paragraph of runs, a tab, a table, a hyperlink field code and entities, as Word writes them.
const sample = `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="x"><w:body>` +
	`<w:p><w:pPr><w:pStyle w:val="Title"/></w:pPr><w:r><w:rPr><w:b/></w:rPr><w:t>Jane Doe</w:t></w:r></w:p>` +
	`<w:p><w:r><w:t xml:space="preserve">Engineer at </w:t></w:r><w:r><w:t>Acme &amp; Sons</w:t></w:r><w:r><w:tab/></w:r><w:r><w:t>2020</w:t></w:r></w:p>` +
	`<w:p><w:r><w:t>Line one</w:t></w:r><w:r><w:br/></w:r><w:r><w:t>line two</w:t></w:r></w:p>` +
	`<w:p><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> HYPERLINK "https://example.com/x" </w:instrText></w:r>` +
	`<w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>my site</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>` +
	`<w:tbl><w:tr><w:tc><w:p><w:r><w:t>Go</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>Python</w:t></w:r></w:p></w:tc></w:tr></w:tbl>` +
	`<w:p/><w:p/><w:p/><w:p><w:r><w:t>5 &lt; 7 &quot;ok&quot;</w:t></w:r></w:p>` +
	`</w:body></w:document>`

func TestDocxTextGivesPlainTextNotMarkup(t *testing.T) {
	text := docxText(sample)
	if strings.Contains(text, "<w:") || strings.Contains(text, "xmlns") || strings.Contains(text, "</") {
		t.Fatalf("markup survived: %q", text)
	}
	for _, want := range []string{"Jane Doe", "Engineer at Acme & Sons 2020", "Line one\nline two", "my site", "Go\nPython", `5 < 7 "ok"`} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
	if strings.Contains(text, "HYPERLINK") || strings.Contains(text, "example.com") {
		t.Errorf("a field code leaked into the text: %q", text)
	}
	if strings.Contains(text, "\n\n\n") {
		t.Errorf("more than one blank line in a row: %q", text)
	}
}

func TestDocxTextOfNothingIsEmpty(t *testing.T) {
	if got := docxText(""); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := docxText(`<w:document><w:body><w:p/></w:body></w:document>`); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestDocxTextIsMuchShorterThanItsMarkup(t *testing.T) {
	if len(docxText(sample))*4 > len(sample) {
		t.Fatalf("plain text %d chars from %d chars of markup", len(docxText(sample)), len(sample))
	}
}

// The whole path: a real .docx file on disk, read by LoadResumes, reaches the model as text.
func TestLoadResumesReadsARealDocxAsText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Jane_Doe_CV_Senior_Engineer.docx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(file)
	for name, body := range map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
		"word/document.xml":   sample,
		"word/_rels/document.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	resumes, err := LoadResumes(dir)
	if err != nil || len(resumes) != 1 {
		t.Fatalf("resumes %d, error %v", len(resumes), err)
	}
	content := resumes[0].GetResumeContent()
	if strings.Contains(content, "<w:") || !strings.Contains(content, "Acme & Sons") {
		t.Fatalf("content is not plain text: %q", content)
	}
}
