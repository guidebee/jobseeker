package jd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nguyenthenguyen/docx"
)

// JobDescription represents a parsed job description from a recruiter
type JobDescription struct {
	Filename string
	Content  string
	FilePath string
}

// LoadJobDescriptions loads all .docx files from the job descriptions directory
func LoadJobDescriptions(jdDir string) ([]*JobDescription, error) {
	var jds []*JobDescription

	// Check if directory exists
	if _, err := os.Stat(jdDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("job descriptions directory not found: %s", jdDir)
	}

	// Read all files in the directory
	files, err := os.ReadDir(jdDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read job descriptions directory: %w", err)
	}

	// Parse each .docx file
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		// Only process .docx files
		if !strings.HasSuffix(strings.ToLower(file.Name()), ".docx") {
			continue
		}

		// Skip temporary Word files (start with ~$)
		if strings.HasPrefix(file.Name(), "~$") {
			continue
		}

		filePath := filepath.Join(jdDir, file.Name())
		content, err := parseDocx(filePath)
		if err != nil {
			fmt.Printf("Warning: failed to parse %s: %v\n", file.Name(), err)
			continue
		}

		jds = append(jds, &JobDescription{
			Filename: file.Name(),
			Content:  content,
			FilePath: filePath,
		})
	}

	return jds, nil
}

// parseDocx extracts text content from a .docx file
func parseDocx(filePath string) (string, error) {
	r, err := docx.ReadDocxFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open docx file: %w", err)
	}
	defer r.Close()

	docx1 := r.Editable()
	return docx1.GetContent(), nil
}

// MoveToArchive moves a processed job description to the archive directory
func (jd *JobDescription) MoveToArchive(archiveDir string) error {
	// Create archive directory if it doesn't exist
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		return fmt.Errorf("failed to create archive directory: %w", err)
	}

	// Construct destination path
	destPath := filepath.Join(archiveDir, jd.Filename)

	// Move the file
	if err := os.Rename(jd.FilePath, destPath); err != nil {
		return fmt.Errorf("failed to move file to archive: %w", err)
	}

	return nil
}

// GetContent returns the full text content of the job description
func (jd *JobDescription) GetContent() string {
	return jd.Content
}
