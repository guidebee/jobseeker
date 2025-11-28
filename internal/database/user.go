package database

import (
	"errors"
	"fmt"
	"log"
	"os"

	"gorm.io/gorm"
)

// GetOrCreateUser retrieves a user by email or creates a new one
func GetOrCreateUser(email, name, location string) (*User, error) {
	if email == "" {
		return nil, errors.New("email is required")
	}

	db := GetDB()
	var user User

	// Try to find existing user
	result := db.Where("email = ?", email).First(&user)

	if result.Error == nil {
		// User found
		return &user, nil
	}

	if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		// Real database error
		return nil, fmt.Errorf("database error: %w", result.Error)
	}

	// User not found, create new one
	user = User{
		Email:           email,
		Name:            name,
		Location:        location,
		PlanType:        "free",
		JobScanLimit:    100,
		AIAnalysisLimit: 50,
	}

	if err := db.Create(&user).Error; err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	log.Printf("Created new user: %s (%s)", user.Name, user.Email)
	return &user, nil
}

// GetCurrentUser retrieves the current user from environment
// Falls back to config.yaml email if USER_EMAIL not set
func GetCurrentUser() (*User, error) {
	email := os.Getenv("USER_EMAIL")
	if email == "" {
		return nil, errors.New("USER_EMAIL not set in environment")
	}

	return GetUserByEmail(email)
}

// GetUserByEmail retrieves a user by email
func GetUserByEmail(email string) (*User, error) {
	if email == "" {
		return nil, errors.New("email is required")
	}

	db := GetDB()
	var user User

	result := db.Where("email = ?", email).First(&user)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("user not found: %s", email)
		}
		return nil, fmt.Errorf("database error: %w", result.Error)
	}

	return &user, nil
}

// CreateUser creates a new user
func CreateUser(email, name, phone, location string) (*User, error) {
	if email == "" {
		return nil, errors.New("email is required")
	}

	db := GetDB()

	// Check if user already exists
	var existing User
	result := db.Where("email = ?", email).First(&existing)
	if result.Error == nil {
		return nil, fmt.Errorf("user already exists: %s", email)
	}

	if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("database error: %w", result.Error)
	}

	// Create new user
	user := User{
		Email:           email,
		Name:            name,
		Phone:           phone,
		Location:        location,
		PlanType:        "free",
		JobScanLimit:    100,
		AIAnalysisLimit: 50,
	}

	if err := db.Create(&user).Error; err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	log.Printf("Created user: %s (%s)", user.Name, user.Email)
	return &user, nil
}

// UpdateUserLimits updates subscription limits for a user
func UpdateUserLimits(userID uint, planType string, jobLimit, analysisLimit int) error {
	db := GetDB()

	result := db.Model(&User{}).Where("id = ?", userID).Updates(map[string]interface{}{
		"plan_type":         planType,
		"job_scan_limit":    jobLimit,
		"ai_analysis_limit": analysisLimit,
	})

	if result.Error != nil {
		return fmt.Errorf("failed to update user limits: %w", result.Error)
	}

	return nil
}

// GetUserStats returns usage statistics for a user
func GetUserStats(userID uint) (map[string]int, error) {
	db := GetDB()
	stats := make(map[string]int)

	// Count jobs
	var jobCount int64
	if err := db.Model(&Job{}).Where("user_id = ?", userID).Count(&jobCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count jobs: %w", err)
	}
	stats["total_jobs"] = int(jobCount)

	// Count analyzed jobs
	var analyzedCount int64
	if err := db.Model(&Job{}).Where("user_id = ? AND is_analyzed = ?", userID, true).Count(&analyzedCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count analyzed jobs: %w", err)
	}
	stats["analyzed_jobs"] = int(analyzedCount)

	// Count recommended jobs
	var recommendedCount int64
	if err := db.Model(&Job{}).Where("user_id = ? AND status = ?", userID, "recommended").Count(&recommendedCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count recommended jobs: %w", err)
	}
	stats["recommended_jobs"] = int(recommendedCount)

	// Count applications
	var appCount int64
	if err := db.Model(&Application{}).Where("user_id = ?", userID).Count(&appCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count applications: %w", err)
	}
	stats["applications"] = int(appCount)

	return stats, nil
}
