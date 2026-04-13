package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	appconfig "github.com/mossagents/moss/config"
	"os"
	"path/filepath"
	stdruntime "runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type CapabilityStatus struct {
	Capability string    `json:"capability"`
	Kind       string    `json:"kind,omitempty"`
	Name       string    `json:"name,omitempty"`
	State      string    `json:"state"`
	Critical   bool      `json:"critical,omitempty"`
	Error      string    `json:"error,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type CapabilitySnapshot struct {
	UpdatedAt time.Time          `json:"updated_at"`
	Items     []CapabilityStatus `json:"items,omitempty"`
}

type fileCapabilityReporter struct {
	path string
	next CapabilityReporter
	mu   sync.Mutex
}

var capabilitySnapshotMu sync.Mutex

func CapabilityStatusPath() string {
	if base := capabilityStatusBaseDir(); base != "" {
		return filepath.Join(base, "state", "capabilities.json")
	}
	appDir := strings.TrimSpace(appconfig.AppDir())
	if appDir == "" {
		return ""
	}
	return filepath.Join(appDir, "state", "capabilities.json")
}

func capabilityStatusBaseDir() string {
	if dir := strings.TrimSpace(os.Getenv("MOSS_APP_DIR")); dir != "" {
		return dir
	}
	if stdruntime.GOOS == "windows" {
		for _, key := range []string{"LOCALAPPDATA", "APPDATA"} {
			if base := strings.TrimSpace(os.Getenv(key)); base != "" {
				return filepath.Join(base, appconfig.AppName())
			}
		}
	}
	return ""
}

func NewCapabilityReporter(path string, next CapabilityReporter) CapabilityReporter {
	path = strings.TrimSpace(path)
	if path == "" {
		return next
	}
	return &fileCapabilityReporter{path: path, next: next}
}

func (r *fileCapabilityReporter) Report(ctx context.Context, capability string, critical bool, state string, err error) {
	if r.next != nil {
		r.next.Report(ctx, capability, critical, state, err)
	}
	capability = strings.TrimSpace(capability)
	state = strings.TrimSpace(state)
	if capability == "" || state == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	capabilitySnapshotMu.Lock()
	defer capabilitySnapshotMu.Unlock()

	snapshot, loadErr := LoadCapabilitySnapshot(r.path)
	if loadErr != nil && !os.IsNotExist(loadErr) {
		return
	}
	indexed := make(map[string]CapabilityStatus, len(snapshot.Items))
	for _, item := range snapshot.Items {
		indexed[item.Capability] = item
	}
	item := indexed[capability]
	item.Capability = capability
	item.Kind, item.Name = splitCapabilityIdentity(capability)
	item.State = state
	item.Critical = critical
	item.UpdatedAt = time.Now().UTC()
	if err != nil {
		item.Error = err.Error()
	} else {
		item.Error = ""
	}
	indexed[capability] = item
	out := CapabilitySnapshot{
		UpdatedAt: item.UpdatedAt,
		Items:     make([]CapabilityStatus, 0, len(indexed)),
	}
	for _, value := range indexed {
		out.Items = append(out.Items, value)
	}
	sort.Slice(out.Items, func(i, j int) bool {
		if out.Items[i].Kind == out.Items[j].Kind {
			return out.Items[i].Capability < out.Items[j].Capability
		}
		return out.Items[i].Kind < out.Items[j].Kind
	})
	_ = saveCapabilitySnapshot(r.path, out)
}

func LoadCapabilitySnapshot(path string) (CapabilitySnapshot, error) {
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return CapabilitySnapshot{}, err
	}
	var snapshot CapabilitySnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return CapabilitySnapshot{}, fmt.Errorf("unmarshal capability snapshot: %w", err)
	}
	if len(snapshot.Items) == 0 {
		return snapshot, nil
	}
	for i := range snapshot.Items {
		if snapshot.Items[i].Kind == "" || snapshot.Items[i].Name == "" {
			snapshot.Items[i].Kind, snapshot.Items[i].Name = splitCapabilityIdentity(snapshot.Items[i].Capability)
		}
	}
	return snapshot, nil
}

func saveCapabilitySnapshot(path string, snapshot CapabilitySnapshot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func splitCapabilityIdentity(capability string) (kind, name string) {
	capability = strings.TrimSpace(capability)
	switch {
	case capability == "":
		return "", ""
	case strings.Contains(capability, ":"):
		parts := strings.SplitN(capability, ":", 2)
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	case strings.HasPrefix(capability, "runtime-"):
		return "runtime", strings.TrimPrefix(capability, "runtime-")
	case capability == "builtin-tools":
		return "builtin", capability
	default:
		return "runtime", capability
	}
}
