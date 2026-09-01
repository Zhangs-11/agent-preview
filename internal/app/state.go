package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/Zhangs-11/agent-preview/internal/preview"
)

var previewIDPattern = regexp.MustCompile(`^[a-f0-9]{16}$`)

type workerRequest struct {
	ID         string       `json:"id"`
	Kind       preview.Kind `json:"kind"`
	Target     string       `json:"target"`
	Token      string       `json:"token"`
	TTLSeconds int64        `json:"ttl_seconds"`
	LocalOnly  bool         `json:"local_only"`
	CreatedAt  time.Time    `json:"created_at"`
}

type State struct {
	ID         string       `json:"id"`
	Status     string       `json:"status"`
	Kind       preview.Kind `json:"kind"`
	Target     string       `json:"target"`
	URL        string       `json:"url,omitempty"`
	LocalURL   string       `json:"local_url,omitempty"`
	Visibility string       `json:"visibility"`
	Provider   string       `json:"provider,omitempty"`
	CreatedAt  time.Time    `json:"created_at"`
	ExpiresAt  *time.Time   `json:"expires_at,omitempty"`
	Error      string       `json:"error,omitempty"`
}

func stateRoot() (string, error) {
	if configured := os.Getenv("AGENT_PREVIEW_STATE_DIR"); configured != "" {
		return filepath.Abs(configured)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate user cache directory: %w", err)
	}
	return filepath.Join(cache, "agent-preview", "previews"), nil
}

func previewDirectory(id string) (string, error) {
	if !previewIDPattern.MatchString(id) {
		return "", fmt.Errorf("invalid preview ID %q", id)
	}
	root, err := stateRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, id), nil
}

func requestPath(id string) (string, error) {
	directory, err := previewDirectory(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "request.json"), nil
}

func statePath(id string) (string, error) {
	directory, err := previewDirectory(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "state.json"), nil
}

func stopPath(id string) (string, error) {
	directory, err := previewDirectory(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "stop"), nil
}

func requestStop(id string) error {
	state, err := readState(id)
	if err != nil {
		return fmt.Errorf("read preview state: %w", err)
	}
	if state.Status != "starting" && state.Status != "ready" {
		return fmt.Errorf("preview %s is already %s", id, state.Status)
	}
	path, err := stopPath(id)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte("stop\n"), 0o600)
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func readJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

func readState(id string) (State, error) {
	path, err := statePath(id)
	if err != nil {
		return State{}, err
	}
	var state State
	if err := readJSON(path, &state); err != nil {
		return State{}, err
	}
	return state, nil
}

func writeState(state State) error {
	path, err := statePath(state.ID)
	if err != nil {
		return err
	}
	return writeJSON(path, state)
}

func listStates() ([]State, error) {
	root, err := stateRoot()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return []State{}, nil
	}
	if err != nil {
		return nil, err
	}
	states := make([]State, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		state, err := readState(entry.Name())
		if err == nil {
			states = append(states, state)
		}
	}
	sort.Slice(states, func(left, right int) bool {
		return states[left].CreatedAt.After(states[right].CreatedAt)
	})
	return states, nil
}
