package main

import (
	"fmt"
	"log"
	"os"

	"github.com/guidebee/jobseeker/internal/database"
	"github.com/guidebee/jobseeker/internal/profile"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

var (
	configPath string
	dbPath     string
)

// rootCmd is the base command
var rootCmd = &cobra.Command{
	Use:   "jobseeker",
	Short: "AI-powered job application assistant",
	Long: `Jobseeker automatically discovers jobs, analyzes matches using Claude AI,
and helps you apply to the best opportunities.`,
}

func main() {
	// Load environment variables from .env file
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found, using environment variables")
	}

	// Execute the root command
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	// Global flags available to all commands
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "configs/config.yaml", "Path to config file")
	rootCmd.PersistentFlags().StringVarP(&dbPath, "database", "d", getEnv("DB_PATH", "./jobseeker.db"), "Path to database file")

	// Add subcommands
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(scanCmd)
	rootCmd.AddCommand(analyzeCmd)
	rootCmd.AddCommand(listCmd)
}

// initApp initializes database and profile
// This is called by commands that need these dependencies
func initApp() (*profile.Profile, error) {
	// Initialize database
	err := database.InitDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	// Load profile
	prof, err := profile.LoadProfile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load profile: %w", err)
	}

	return prof, nil
}

// getEnv gets an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}
