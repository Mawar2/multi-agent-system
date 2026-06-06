// Command hunter runs the Hunter agent, which polls SAM.gov for federal
// contract opportunities and files GitHub issues for relevant ones.
//
// Usage:
//
//	hunter --config hunter.yml
//
// Environment variables override config file values:
//
//	SAM_API_KEY    - SAM.gov API key
//	GITHUB_TOKEN   - GitHub personal access token (repo scope)
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Mawar2/multi-agent-system/internal/hunter"
	"gopkg.in/yaml.v3"
)

func main() {
	configPath := flag.String("config", "hunter.yml", "path to hunter config YAML")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// Environment variables take precedence over config file.
	if v := os.Getenv("SAM_API_KEY"); v != "" {
		cfg.SAMAPIKey = v
	}
	if v := os.Getenv("GITHUB_TOKEN"); v != "" {
		cfg.GitHubToken = v
	}

	if err := validate(cfg); err != nil {
		log.Fatalf("invalid config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	h := hunter.New(*cfg)
	h.Run(ctx)
}

func loadConfig(path string) (*hunter.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg hunter.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse YAML: %w", err)
	}
	return &cfg, nil
}

func validate(cfg *hunter.Config) error {
	if cfg.SAMAPIKey == "" {
		return fmt.Errorf("sam_api_key is required (or set SAM_API_KEY env var)")
	}
	if cfg.GitHubToken == "" {
		return fmt.Errorf("github_token is required (or set GITHUB_TOKEN env var)")
	}
	if cfg.TrackingRepoOwner == "" || cfg.TrackingRepoName == "" {
		return fmt.Errorf("tracking_repo_owner and tracking_repo_name are required")
	}
	return nil
}
