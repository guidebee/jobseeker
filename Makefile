# Makefile for Jobseeker
# Usage: make <target>

.PHONY: help build run clean test deps

# Default target
help:
	@echo "Available commands:"
	@echo "  make build     - Build the application"
	@echo "  make run       - Run without building (development)"
	@echo "  make deps      - Download dependencies"
	@echo "  make clean     - Remove build artifacts"
	@echo "  make test      - Run tests"
	@echo "  make scan      - Run job scanner"
	@echo "  make analyze   - Analyze jobs with AI"
	@echo "  make list      - List recommended jobs"

# Build the application
build:
	go build -o jobseeker.exe ./cmd/jobseeker

# Download dependencies
deps:
	go mod download
	go mod tidy

# Clean build artifacts
clean:
	rm -f jobseeker.exe jobseeker

# Run tests
test:
	go test -v ./...

# Development: run without building
run:
	go run ./cmd/jobseeker

# Quick commands
scan: build
	./jobseeker.exe scan

analyze: build
	./jobseeker.exe analyze

list: build
	./jobseeker.exe list --recommended
