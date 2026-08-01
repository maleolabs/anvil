package server

import (
	"fmt"
)

// InitResult describes the outcome of InitializeServer.
type InitResult struct {
	// ConfigPath is the absolute path to the config file that was created
	// or already exists.
	ConfigPath string

	// AlreadyInitialized is true if the configuration already existed
	// and was not modified.
	AlreadyInitialized bool
}

// InitializeServer initializes the Server Runtime configuration store.
//
// It creates the config root directory (if needed) and writes the default
// ServerConfig to config.yaml if it does not already exist.
//
// If the configuration already exists, InitializeServer returns with
// AlreadyInitialized=true and does not modify any files (idempotent).
//
// The rootPath parameter specifies the config root directory. If empty,
// DefaultConfigRoot (/etc/anvil) is used.
//
// InitializeServer does NOT register projects, install artifacts, create
// releases, inspect a repository, or require anvil.yaml.
//
// Reference: ST-P5-07, ADR-013
func InitializeServer(rootPath string) (*InitResult, error) {
	if rootPath == "" {
		rootPath = DefaultConfigRoot
	}

	store := NewConfigStore(rootPath)
	alreadyInitialized := store.Exists()

	if err := store.Init(); err != nil {
		return nil, fmt.Errorf("initialize server runtime: %w", err)
	}

	return &InitResult{
		ConfigPath:         store.ConfigPath(),
		AlreadyInitialized: alreadyInitialized,
	}, nil
}
