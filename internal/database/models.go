package database

import (
	"time"

	"gorm.io/gorm"
)

// User represents a user of the application
type User struct {
	ID        uint           `gorm:"primarykey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`

	// User identification
	Email    string `gorm:"uniqueIndex;not null"` // Primary identifier
	Name     string
	Phone    string
	Location string

	// Authentication (for future web interface)
	PasswordHash string // bcrypt hash, empty for CLI-only usage

	// Subscription/limits (for future SaaS model)
	PlanType         string    `gorm:"default:'free'"` // "free", "premium", "enterprise"
	JobScanLimit     int       `gorm:"default:100"`    // Jobs per month
	AIAnalysisLimit  int       `gorm:"default:50"`     // AI analyses per month
	SubscriptionEnds *time.Time

	// Relationships
	Jobs         []Job         `gorm:"foreignKey:UserID"`
	Applications []Application `gorm:"foreignKey:UserID"`
	ProfileData  *ProfileData  `gorm:"foreignKey:UserID"`
}

// Job represents a job posting discovered by the scanner
type Job struct {
	ID          uint           `gorm:"primarykey"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   gorm.DeletedAt `gorm:"index"`

	// User ownership
	UserID uint  `gorm:"uniqueIndex:idx_user_external;not null"` // Composite unique index with ExternalID
	User   User  `gorm:"foreignKey:UserID"`

	// Job details from scraper
	ExternalID  string `gorm:"uniqueIndex:idx_user_external"` // Job board's unique ID (unique per user)
	Source      string `gorm:"index"`        // e.g., "seek", "linkedin"
	URL         string
	Title       string
	Company     string
	Location    string
	Salary      string
	JobType     string `gorm:"index"`        // "contract", "permanent", "unknown"
	Description string `gorm:"type:text"`
	Requirements string `gorm:"type:text"`

	// Analysis results from Claude
	MatchScore       int        // 0-100
	Analysis         string     `gorm:"type:text"` // Full formatted analysis
	AnalysisReasoning string    `gorm:"type:text"` // Claude's reasoning (structured)
	AnalysisPros     string     `gorm:"type:text"` // Pros as JSON array
	AnalysisCons     string     `gorm:"type:text"` // Cons as JSON array
	ResumeUsed       string     // Which resume was used for analysis
	IsAnalyzed       bool       `gorm:"index"`
	AnalyzedAt       *time.Time

	// Application status
	Status        string `gorm:"index"` // "discovered", "recommended", "approved", "applied", "rejected"
	AppliedAt     *time.Time
	CoverLetter   string `gorm:"type:text"`
}

// Application tracks submitted job applications
type Application struct {
	ID          uint           `gorm:"primarykey"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   gorm.DeletedAt `gorm:"index"`

	// User ownership
	UserID uint `gorm:"index;not null"`
	User   User `gorm:"foreignKey:UserID"`

	JobID       uint   `gorm:"index"`
	Job         Job    `gorm:"foreignKey:JobID"`

	// Application details
	CoverLetter string `gorm:"type:text"`
	Resume      string // Path or version identifier
	Status      string // "pending", "viewed", "interview", "rejected", "offer"
	Notes       string `gorm:"type:text"`

	// Response tracking
	ResponseAt  *time.Time
	InterviewAt *time.Time
}

// ProfileData stores cached profile information (resumes, GitHub, LinkedIn)
// One row per user, updated during 'jobseeker init'
type ProfileData struct {
	ID        uint      `gorm:"primarykey"`
	CreatedAt time.Time
	UpdatedAt time.Time

	// User ownership
	UserID uint `gorm:"uniqueIndex;not null"` // One ProfileData per user
	User   User `gorm:"foreignKey:UserID"`

	// Cached resumes (JSON array of {filename, content, metadata})
	ResumesJSON string `gorm:"type:text"`

	// GitHub data (JSON array of repos)
	GitHubRepos string `gorm:"type:text"`
	GitHubUser  string // GitHub username from config

	// LinkedIn profile data
	LinkedInProfile string `gorm:"type:text"`
	LinkedInURL     string

	// Search keywords extracted from resumes (JSON array)
	SearchKeywords string `gorm:"type:text"`

	// Metadata
	ResumesCount   int
	LastInitAt     time.Time
	InitVersion    string // Track init schema version
}
