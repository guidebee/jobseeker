package resume

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nguyenthenguyen/docx"
)

// Resume represents a parsed resume
type Resume struct {
	Filename string
	Content  string
	FilePath string
}

// LoadResumes loads all .docx files from the resumes directory
func LoadResumes(resumesDir string) ([]*Resume, error) {
	var resumes []*Resume

	// Check if resumes directory exists
	if _, err := os.Stat(resumesDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("resumes directory not found: %s", resumesDir)
	}

	// Read all files in the directory
	files, err := os.ReadDir(resumesDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read resumes directory: %w", err)
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

		filePath := filepath.Join(resumesDir, file.Name())
		content, err := parseDocx(filePath)
		if err != nil {
			fmt.Printf("Warning: failed to parse %s: %v\n", file.Name(), err)
			continue
		}

		resumes = append(resumes, &Resume{
			Filename: file.Name(),
			Content:  content,
			FilePath: filePath,
		})
	}

	return resumes, nil
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

// SelectBestResume chooses the most appropriate resume for a job
// Currently returns the first resume, but can be enhanced to match by keywords
func SelectBestResume(resumes []*Resume, jobTitle string, jobType string) *Resume {
	if len(resumes) == 0 {
		return nil
	}

	// Future enhancement: match resume to job type
	// For now, use keyword matching
	jobTitleLower := strings.ToLower(jobTitle)

	// Look for resume with matching keywords in filename
	for _, resume := range resumes {
		filenameLower := strings.ToLower(resume.Filename)

		// Check for job type match
		if jobType == "contract" && strings.Contains(filenameLower, "contract") {
			return resume
		}
		if jobType == "permanent" && strings.Contains(filenameLower, "permanent") {
			return resume
		}

		// Check for role-specific keywords
		keywords := []string{"senior", "lead", "architect", "engineer", "developer", "backend", "frontend", "fullstack"}
		for _, keyword := range keywords {
			if strings.Contains(jobTitleLower, keyword) && strings.Contains(filenameLower, keyword) {
				return resume
			}
		}
	}

	// Default: return first resume
	return resumes[0]
}

// GetResumeContent returns the full text content of a resume
func (r *Resume) GetResumeContent() string {
	return r.Content
}

// GetSummary returns the first 500 characters as a summary
func (r *Resume) GetSummary() string {
	if len(r.Content) <= 500 {
		return r.Content
	}
	return r.Content[:500] + "..."
}
