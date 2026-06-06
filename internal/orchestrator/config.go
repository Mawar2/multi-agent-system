package orchestrator

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the supervisor's configuration.
type Config struct {
	// Projects to monitor
	Projects []ProjectConfig `yaml:"projects"`

	// Worker tier configuration
	WorkerTiers WorkerTierConfig `yaml:"worker_tiers"`

	// Supervisor settings
	PollIntervalSeconds int    `yaml:"poll_interval_seconds"` // How often to poll GitHub (default: 60)
	TaskTimeoutMinutes  int    `yaml:"task_timeout_minutes"`  // Max time for a task (default: 120)
	MaxRetryAttempts    int    `yaml:"max_retry_attempts"`    // Max retries per task (default: 3)
	TaskQueueDir        string `yaml:"task_queue_dir"`        // Directory for JSON queue (default: ./tasks)
}

// IssueFilterConfig declares which issues should be skipped before the supervisor auto-works them.
// All filters are opt-in; existing behaviour is preserved when the block is absent.
type IssueFilterConfig struct {
	// SkipIfHasPR skips issues that already have an open PR. Defaults to true when nil.
	SkipIfHasPR *bool `yaml:"skip_if_has_pr"`
	// SkipLabels skips issues carrying any of these labels (case-insensitive).
	SkipLabels []string `yaml:"skip_labels,omitempty"`
	// RequireAcceptanceCriteria skips issues that have no "- [ ]" checklist items.
	RequireAcceptanceCriteria bool `yaml:"require_acceptance_criteria"`
}

// ProjectConfig defines a project to monitor.
type ProjectConfig struct {
	Name            string            `yaml:"name"`             // Project name (e.g., "kaimi")
	RepoOwner       string            `yaml:"repo_owner"`       // GitHub owner (e.g., "Mawar2")
	RepoName        string            `yaml:"repo_name"`        // GitHub repo (e.g., "Kaimi")
	ConventionsPath string            `yaml:"conventions_path"` // Path to CLAUDE.md (e.g., "./CLAUDE.md")
	BranchPattern   string            `yaml:"branch_pattern"`   // e.g., "feature/KAI-{ticket}-{summary}"
	CommitPattern   string            `yaml:"commit_pattern"`   // e.g., "{ticket}_{description}"
	Labels          []string          `yaml:"labels,omitempty"` // Filter issues by labels (optional)
	IssueFilter     IssueFilterConfig `yaml:"issue_filter"`     // Smart issue filtering rules
}

// IssueFilterForRepo returns the IssueFilterConfig for the given repo with safe defaults applied.
// Unknown repos also receive safe defaults (SkipIfHasPR=true, no skip labels, AC not required).
func (c *Config) IssueFilterForRepo(owner, repo string) IssueFilterConfig {
	for _, p := range c.Projects {
		if strings.EqualFold(p.RepoOwner, owner) && strings.EqualFold(p.RepoName, repo) {
			f := p.IssueFilter
			if f.SkipIfHasPR == nil {
				t := true
				f.SkipIfHasPR = &t
			}
			return f
		}
	}
	t := true
	return IssueFilterConfig{SkipIfHasPR: &t}
}

// WorkerTierConfig defines worker pool settings.
type WorkerTierConfig struct {
	GeminiFlash GeminiFlashConfig `yaml:"gemini_flash"`
	GeminiPro   GeminiProConfig   `yaml:"gemini_pro"`
	Claude      ClaudeConfig      `yaml:"claude"`
}

// GeminiFlashConfig for Gemini Flash tier.
type GeminiFlashConfig struct {
	MaxWorkers int    `yaml:"max_workers"` // Max concurrent workers (default: 5)
	Model      string `yaml:"model"`       // Model name (e.g., "gemini-flash-3.5")
}

// GeminiProConfig for Gemini Pro tier.
type GeminiProConfig struct {
	MaxWorkers int    `yaml:"max_workers"` // Max concurrent workers (default: 3)
	Model      string `yaml:"model"`       // Model name (e.g., "gemini-pro-3.5")
}

// ClaudeConfig for Claude tier.
type ClaudeConfig struct {
	MaxWorkers int    `yaml:"max_workers"` // Max concurrent workers (default: 2)
	Model      string `yaml:"model"`       // Model name (e.g., "claude-sonnet-4.5")
}

// LoadConfig loads configuration from a YAML file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config YAML: %w", err)
	}

	// Apply defaults
	if config.PollIntervalSeconds == 0 {
		config.PollIntervalSeconds = 60
	}
	if config.TaskTimeoutMinutes == 0 {
		config.TaskTimeoutMinutes = 120
	}
	if config.MaxRetryAttempts == 0 {
		config.MaxRetryAttempts = 3
	}
	if config.TaskQueueDir == "" {
		config.TaskQueueDir = "./tasks"
	}

	// Validate
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &config, nil
}

// Validate checks that the configuration is valid.
func (c *Config) Validate() error {
	if len(c.Projects) == 0 {
		return fmt.Errorf("no projects configured")
	}

	for i, proj := range c.Projects {
		if proj.Name == "" {
			return fmt.Errorf("project %d: name is required", i)
		}
		if proj.RepoOwner == "" {
			return fmt.Errorf("project %s: repo_owner is required", proj.Name)
		}
		if proj.RepoName == "" {
			return fmt.Errorf("project %s: repo_name is required", proj.Name)
		}
	}

	return nil
}
