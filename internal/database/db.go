package database

import (
	"fmt"
	"log"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

// InitDB initializes the database connection
// This is a common pattern in Go - initialize once, use globally
func InitDB(dbPath string) error {
	var err error

	// Open SQLite database
	// gorm.Config lets you customize logging and behavior
	DB, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})

	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	// AutoMigrate creates tables based on your struct definitions
	// This is like running SQL CREATE TABLE statements
	// Order matters: User must be created before models with foreign keys
	err = DB.AutoMigrate(&User{}, &Job{}, &Application{}, &ProfileData{})
	if err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	log.Println("Database initialized successfully")
	return nil
}

// GetDB returns the database instance
// Useful for testing - you can mock this
func GetDB() *gorm.DB {
	return DB
}
