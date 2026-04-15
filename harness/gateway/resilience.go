package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var ErrRetryBudgetExceeded = errors.New("retry budget exceeded")

type RetryBudget struct {
	mu    sync.Mutex
	max   int
	used  int
	usage map[string]int
}

func NewRetryBudget(max int) *RetryBudget {
	if max <= 0 {
		max = 1
	}
	return &RetryBudget{max: max, usage: make(map[string]int)}
}

func (b *RetryBudget) Consume(layer string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used >= b.max {
		return ErrRetryBudgetExceeded
	}
	b.used++
	b.usage[layer]++
	return nil
}

func (b *RetryBudget) Remaining() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.max - b.used
}

type ModelProfile struct {
	Name     string
	Provider string
	Model    string
	APIKey   string
	BaseURL  string
}

type ProfileRotator struct {
	mu        sync.Mutex
	profiles  []ModelProfile
	idx       int
	failCount int
}

func NewProfileRotator(profiles []ModelProfile) *ProfileRotator {
	filtered := make([]ModelProfile, 0, len(profiles))
	for _, p := range profiles {
		if strings.TrimSpace(p.Name) == "" {
			p.Name = fmt.Sprintf("profile-%d", len(filtered)+1)
		}
		if strings.TrimSpace(p.Provider) == "" && strings.TrimSpace(p.Model) == "" &&
			strings.TrimSpace(p.APIKey) == "" && strings.TrimSpace(p.BaseURL) == "" {
			continue
		}
		filtered = append(filtered, p)
	}
	if len(filtered) == 0 {
		filtered = append(filtered, ModelProfile{Name: "primary"})
	}
	return &ProfileRotator{profiles: filtered}
}

func (r *ProfileRotator) Current() ModelProfile {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.profiles[r.idx]
}

func (r *ProfileRotator) MarkFailureAndRotate() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failCount++
	if r.idx+1 < len(r.profiles) {
		r.idx++
		return true
	}
	return false
}

func (r *ProfileRotator) MarkSuccess() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failCount = 0
}

type AssetMode string

const (
	AssetModeStrict     AssetMode = "strict"
	AssetModeBestEffort AssetMode = "best-effort"
)

type AssetReport struct {
	Found   []string
	Missing []string
	Invalid []string
}

func ValidateRuntimeAssets(workspace string, mode AssetMode) (AssetReport, error) {
	report := AssetReport{}
	checkFile := func(name string, required bool) (string, error) {
		path := filepath.Join(workspace, name)
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				if required {
					return "", fmt.Errorf("required asset missing: %s", name)
				}
				report.Missing = append(report.Missing, name)
				return "", nil
			}
			return "", err
		}
		if info.IsDir() {
			return "", fmt.Errorf("asset %s is directory", name)
		}
		report.Found = append(report.Found, name)
		return path, nil
	}

	_, err := checkFile("HEARTBEAT.md", false)
	if err != nil && mode == AssetModeStrict {
		return report, err
	}
	cronPath, err := checkFile("CRON.json", false)
	if err != nil && mode == AssetModeStrict {
		return report, err
	}
	if cronPath != "" {
		data, readErr := os.ReadFile(cronPath)
		if readErr != nil {
			return report, readErr
		}
		var arr any
		if parseErr := json.Unmarshal(data, &arr); parseErr != nil {
			report.Invalid = append(report.Invalid, "CRON.json")
			if mode == AssetModeStrict {
				return report, fmt.Errorf("invalid CRON.json: %w", parseErr)
			}
		}
	}

	skillsDir := filepath.Join(workspace, "skills")
	if info, statErr := os.Stat(skillsDir); statErr == nil && info.IsDir() {
		report.Found = append(report.Found, "skills/")
		entries, rdErr := os.ReadDir(skillsDir)
		if rdErr == nil {
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				skillPath := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
				if _, err := os.Stat(skillPath); err != nil {
					report.Invalid = append(report.Invalid, filepath.ToSlash(filepath.Join("skills", entry.Name(), "SKILL.md")))
				}
			}
		}
	} else {
		report.Missing = append(report.Missing, "skills/")
	}

	if mode == AssetModeStrict && len(report.Invalid) > 0 {
		return report, fmt.Errorf("invalid assets: %s", strings.Join(report.Invalid, ", "))
	}
	return report, nil
}
