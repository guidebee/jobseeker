package export

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/guidebee/jobseeker/internal/database"
	"github.com/xuri/excelize/v2"
)

// GenerateExcelLocally creates an Excel file using excelize library (no API calls)
func GenerateExcelLocally(jobs []database.Job, outputPath string) error {
	f := excelize.NewFile()
	defer f.Close()

	// Create Sheet 1: Jobs Summary
	err := createJobsSummarySheet(f, jobs)
	if err != nil {
		return fmt.Errorf("failed to create jobs summary sheet: %w", err)
	}

	// Create Sheet 2: Detailed Analysis
	err = createAnalysisSheet(f, jobs)
	if err != nil {
		return fmt.Errorf("failed to create analysis sheet: %w", err)
	}

	// Create Sheet 3: Statistics
	err = createStatisticsSheet(f, jobs)
	if err != nil {
		return fmt.Errorf("failed to create statistics sheet: %w", err)
	}

	// Delete default Sheet1 if it exists
	f.DeleteSheet("Sheet1")

	// Save the file
	if err := f.SaveAs(outputPath); err != nil {
		return fmt.Errorf("failed to save Excel file: %w", err)
	}

	return nil
}

// createJobsSummarySheet creates the main jobs listing sheet
func createJobsSummarySheet(f *excelize.File, jobs []database.Job) error {
	sheetName := "Jobs Summary"
	index, err := f.NewSheet(sheetName)
	if err != nil {
		return err
	}
	f.SetActiveSheet(index)

	// Define headers
	headers := []string{"ID", "Title", "Company", "Location", "Salary", "Type", "Source", "Score", "Status", "Pros", "Cons", "Date Found", "URL"}

	// Create header style
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Size: 11, Color: "#FFFFFF"},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#4472C4"}, Pattern: 1},
		Border: []excelize.Border{
			{Type: "top", Color: "#000000", Style: 1},
			{Type: "bottom", Color: "#000000", Style: 1},
			{Type: "left", Color: "#000000", Style: 1},
			{Type: "right", Color: "#000000", Style: 1},
		},
	})

	// Write headers
	for i, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheetName, cell, header)
		f.SetCellStyle(sheetName, cell, cell, headerStyle)
	}

	// Color styles for match scores
	redStyle, _ := f.NewStyle(&excelize.Style{Fill: excelize.Fill{Type: "pattern", Color: []string{"#FF6B6B"}, Pattern: 1}})
	yellowStyle, _ := f.NewStyle(&excelize.Style{Fill: excelize.Fill{Type: "pattern", Color: []string{"#FFE66D"}, Pattern: 1}})
	greenStyle, _ := f.NewStyle(&excelize.Style{Fill: excelize.Fill{Type: "pattern", Color: []string{"#95E1D3"}, Pattern: 1}})

	// Text wrap style for pros/cons
	wrapStyle, _ := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{
			WrapText: true,
			Vertical: "top",
		},
	})

	// Write data rows
	for i, job := range jobs {
		row := i + 2
		f.SetCellValue(sheetName, fmt.Sprintf("A%d", row), job.ID)
		f.SetCellValue(sheetName, fmt.Sprintf("B%d", row), job.Title)
		f.SetCellValue(sheetName, fmt.Sprintf("C%d", row), job.Company)
		f.SetCellValue(sheetName, fmt.Sprintf("D%d", row), job.Location)
		f.SetCellValue(sheetName, fmt.Sprintf("E%d", row), job.Salary)
		f.SetCellValue(sheetName, fmt.Sprintf("F%d", row), job.JobType)
		f.SetCellValue(sheetName, fmt.Sprintf("G%d", row), job.Source)
		f.SetCellValue(sheetName, fmt.Sprintf("H%d", row), job.MatchScore)
		f.SetCellValue(sheetName, fmt.Sprintf("I%d", row), job.Status)

		// Parse and add pros/cons
		var pros, cons []string
		if job.AnalysisPros != "" {
			json.Unmarshal([]byte(job.AnalysisPros), &pros)
		}
		if job.AnalysisCons != "" {
			json.Unmarshal([]byte(job.AnalysisCons), &cons)
		}

		// Format pros/cons with bullet points
		prosText := ""
		if len(pros) > 0 {
			for _, pro := range pros {
				prosText += "✓ " + pro + "\n"
			}
		}
		consText := ""
		if len(cons) > 0 {
			for _, con := range cons {
				consText += "✗ " + con + "\n"
			}
		}

		f.SetCellValue(sheetName, fmt.Sprintf("J%d", row), prosText)
		f.SetCellStyle(sheetName, fmt.Sprintf("J%d", row), fmt.Sprintf("J%d", row), wrapStyle)

		f.SetCellValue(sheetName, fmt.Sprintf("K%d", row), consText)
		f.SetCellStyle(sheetName, fmt.Sprintf("K%d", row), fmt.Sprintf("K%d", row), wrapStyle)

		f.SetCellValue(sheetName, fmt.Sprintf("L%d", row), job.CreatedAt.Format("02/01/2006"))

		// Set URL as hyperlink
		f.SetCellHyperLink(sheetName, fmt.Sprintf("M%d", row), job.URL, "External")
		f.SetCellValue(sheetName, fmt.Sprintf("M%d", row), "View Job")

		// Color code match score
		scoreCell := fmt.Sprintf("H%d", row)
		if job.MatchScore < 50 {
			f.SetCellStyle(sheetName, scoreCell, scoreCell, redStyle)
		} else if job.MatchScore < 70 {
			f.SetCellStyle(sheetName, scoreCell, scoreCell, yellowStyle)
		} else {
			f.SetCellStyle(sheetName, scoreCell, scoreCell, greenStyle)
		}
	}

	// Auto-fit columns
	for i := range headers {
		col, _ := excelize.ColumnNumberToName(i + 1)
		f.SetColWidth(sheetName, col, col, 15)
	}
	f.SetColWidth(sheetName, "B", "B", 30) // Title column wider
	f.SetColWidth(sheetName, "J", "K", 35) // Pros/Cons columns wider
	f.SetColWidth(sheetName, "M", "M", 12) // URL column

	// Enable auto-filter
	lastCol, _ := excelize.ColumnNumberToName(len(headers))
	f.AutoFilter(sheetName, fmt.Sprintf("A1:%s1", lastCol), []excelize.AutoFilterOptions{})

	// Freeze top row
	f.SetPanes(sheetName, &excelize.Panes{
		Freeze: true,
		YSplit: 1,
	})

	return nil
}

// createAnalysisSheet creates the detailed analysis sheet
func createAnalysisSheet(f *excelize.File, jobs []database.Job) error {
	sheetName := "Detailed Analysis"
	_, err := f.NewSheet(sheetName)
	if err != nil {
		return err
	}

	headers := []string{"ID", "Title", "Company", "Score", "Reasoning", "Pros", "Cons", "Resume Used", "Analyzed Date"}

	// Header style
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Size: 11, Color: "#FFFFFF"},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#70AD47"}, Pattern: 1},
	})

	// Write headers
	for i, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheetName, cell, header)
		f.SetCellStyle(sheetName, cell, cell, headerStyle)
	}

	// Write data
	for i, job := range jobs {
		if !job.IsAnalyzed {
			continue
		}

		row := i + 2
		f.SetCellValue(sheetName, fmt.Sprintf("A%d", row), job.ID)
		f.SetCellValue(sheetName, fmt.Sprintf("B%d", row), job.Title)
		f.SetCellValue(sheetName, fmt.Sprintf("C%d", row), job.Company)
		f.SetCellValue(sheetName, fmt.Sprintf("D%d", row), job.MatchScore)
		f.SetCellValue(sheetName, fmt.Sprintf("E%d", row), truncateText(job.AnalysisReasoning, 500))

		// Parse pros/cons from JSON
		var pros, cons []string
		json.Unmarshal([]byte(job.AnalysisPros), &pros)
		json.Unmarshal([]byte(job.AnalysisCons), &cons)

		f.SetCellValue(sheetName, fmt.Sprintf("F%d", row), strings.Join(pros, "; "))
		f.SetCellValue(sheetName, fmt.Sprintf("G%d", row), strings.Join(cons, "; "))
		f.SetCellValue(sheetName, fmt.Sprintf("H%d", row), job.ResumeUsed)

		if job.AnalyzedAt != nil {
			f.SetCellValue(sheetName, fmt.Sprintf("I%d", row), job.AnalyzedAt.Format("02/01/2006"))
		}
	}

	// Set column widths
	f.SetColWidth(sheetName, "B", "B", 25)
	f.SetColWidth(sheetName, "E", "E", 40)
	f.SetColWidth(sheetName, "F", "G", 30)

	// Freeze top row
	f.SetPanes(sheetName, &excelize.Panes{Freeze: true, YSplit: 1})

	return nil
}

// createStatisticsSheet creates the statistics dashboard sheet
func createStatisticsSheet(f *excelize.File, jobs []database.Job) error {
	sheetName := "Statistics"
	_, err := f.NewSheet(sheetName)
	if err != nil {
		return err
	}

	// Calculate statistics
	stats := calculateStatistics(jobs)

	// Title style
	titleStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Size: 14},
	})

	// Header style
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#D9E1F2"}, Pattern: 1},
	})

	row := 1

	// Total jobs
	f.SetCellValue(sheetName, "A"+fmt.Sprint(row), "Job Search Statistics")
	f.SetCellStyle(sheetName, "A"+fmt.Sprint(row), "A"+fmt.Sprint(row), titleStyle)
	row += 2

	f.SetCellValue(sheetName, "A"+fmt.Sprint(row), "Total Jobs Found")
	f.SetCellValue(sheetName, "B"+fmt.Sprint(row), len(jobs))
	row++

	f.SetCellValue(sheetName, "A"+fmt.Sprint(row), "Average Match Score")
	f.SetCellValue(sheetName, "B"+fmt.Sprint(row), stats.AvgMatchScore)
	row += 2

	// Jobs by status
	f.SetCellValue(sheetName, "A"+fmt.Sprint(row), "Jobs by Status")
	f.SetCellStyle(sheetName, "A"+fmt.Sprint(row), "B"+fmt.Sprint(row), headerStyle)
	row++

	for status, count := range stats.ByStatus {
		f.SetCellValue(sheetName, "A"+fmt.Sprint(row), status)
		f.SetCellValue(sheetName, "B"+fmt.Sprint(row), count)
		row++
	}
	row++

	// Jobs by source
	f.SetCellValue(sheetName, "A"+fmt.Sprint(row), "Jobs by Source")
	f.SetCellStyle(sheetName, "A"+fmt.Sprint(row), "B"+fmt.Sprint(row), headerStyle)
	row++

	for source, count := range stats.BySource {
		f.SetCellValue(sheetName, "A"+fmt.Sprint(row), source)
		f.SetCellValue(sheetName, "B"+fmt.Sprint(row), count)
		row++
	}
	row++

	// Jobs by type
	f.SetCellValue(sheetName, "A"+fmt.Sprint(row), "Jobs by Type")
	f.SetCellStyle(sheetName, "A"+fmt.Sprint(row), "B"+fmt.Sprint(row), headerStyle)
	row++

	for jobType, count := range stats.ByType {
		f.SetCellValue(sheetName, "A"+fmt.Sprint(row), jobType)
		f.SetCellValue(sheetName, "B"+fmt.Sprint(row), count)
		row++
	}
	row++

	// Top companies
	f.SetCellValue(sheetName, "A"+fmt.Sprint(row), "Top 10 Companies")
	f.SetCellStyle(sheetName, "A"+fmt.Sprint(row), "B"+fmt.Sprint(row), headerStyle)
	row++

	for i, item := range stats.TopCompanies {
		if i >= 10 {
			break
		}
		f.SetCellValue(sheetName, "A"+fmt.Sprint(row), item.Name)
		f.SetCellValue(sheetName, "B"+fmt.Sprint(row), item.Count)
		row++
	}

	// Set column widths
	f.SetColWidth(sheetName, "A", "A", 25)
	f.SetColWidth(sheetName, "B", "B", 12)

	return nil
}

type Statistics struct {
	AvgMatchScore float64
	ByStatus      map[string]int
	BySource      map[string]int
	ByType        map[string]int
	TopCompanies  []NameCount
	TopLocations  []NameCount
}

type NameCount struct {
	Name  string
	Count int
}

func calculateStatistics(jobs []database.Job) Statistics {
	stats := Statistics{
		ByStatus: make(map[string]int),
		BySource: make(map[string]int),
		ByType:   make(map[string]int),
	}

	totalScore := 0
	analyzedCount := 0
	companyMap := make(map[string]int)
	locationMap := make(map[string]int)

	for _, job := range jobs {
		// Status counts
		stats.ByStatus[job.Status]++

		// Source counts
		stats.BySource[job.Source]++

		// Type counts
		stats.ByType[job.JobType]++

		// Average score
		if job.IsAnalyzed {
			totalScore += job.MatchScore
			analyzedCount++
		}

		// Company counts
		if job.Company != "" {
			companyMap[job.Company]++
		}

		// Location counts
		if job.Location != "" {
			locationMap[job.Location]++
		}
	}

	if analyzedCount > 0 {
		stats.AvgMatchScore = float64(totalScore) / float64(analyzedCount)
	}

	// Convert maps to sorted slices
	for name, count := range companyMap {
		stats.TopCompanies = append(stats.TopCompanies, NameCount{name, count})
	}
	for name, count := range locationMap {
		stats.TopLocations = append(stats.TopLocations, NameCount{name, count})
	}

	return stats
}
