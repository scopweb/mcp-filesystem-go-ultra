package core

import "time"

// Streaming and performance thresholds for file operations
// These constants define when to switch between different processing strategies

const (
	// SmallFileThreshold defines files read entirely into memory
	// Files <= 100KB are read completely for better performance
	SmallFileThreshold = 100 * 1024

	// MediumFileThreshold defines when streaming should start
	// Files > 100KB and <= 500KB use streaming with moderate buffering
	MediumFileThreshold = 500 * 1024

	// LargeFileThreshold defines when chunking/segmentation should be used
	// Files > 500KB and <= 5MB use adaptive chunking
	LargeFileThreshold = 5 * 1024 * 1024

	// VeryLargeFileThreshold for specialized handling
	// Files > 5MB require special handling (lazy loading, pagination, etc)
	VeryLargeFileThreshold = 50 * 1024 * 1024

	// Default buffer sizes for I/O operations
	DefaultBufferSize = 64 * 1024 // 64KB - optimal for most disk I/O

	// Context timeout for file operations
	DefaultOperationTimeout = 30 * time.Second

	// Cache expiration times
	FileCacheExpiration = 3 * time.Minute   // How long to cache file contents
	DirCacheExpiration  = 2 * time.Minute   // How long to cache directory listings
	MetaCacheExpiration = 10 * time.Minute  // How long to cache file metadata

	// Maximum entries to search before returning results
	MaxSearchResults = 1000

	// Maximum items to return in directory listings
	MaxListItems = 10000

	// Regex cache limits
	MaxRegexPatterns = 100 // Maximum compiled patterns to keep in cache

	// Pipeline execution limits
	MaxPipelineSteps = 20  // Maximum number of steps per pipeline
	MaxPipelineFiles = 100 // Maximum number of files affected by a pipeline

	// Pipeline risk assessment thresholds (based on number of files)
	PipelineRiskMedium   = 30  // 30+ files = MEDIUM risk
	PipelineRiskHigh     = 50  // 50+ files = HIGH risk
	PipelineRiskCritical = 80  // 80+ files = CRITICAL risk

	// Pipeline edit thresholds (based on number of edits)
	PipelineEditsMedium   = 100  // 100+ edits = MEDIUM risk
	PipelineEditsHigh     = 500  // 500+ edits = HIGH risk
	PipelineEditsCritical = 1000 // 1000+ edits = CRITICAL risk
)
