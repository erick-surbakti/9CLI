package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// memoryFile is the user-level profile store, shared across sessions.
var memoryFile = func() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "ai-cli", "memory.json")
}()

// Profile is the structured "who you are" memory shown to the model.
type Profile struct {
	Name            string `json:"name"`
	Hobby           string `json:"hobby"`
	Personalization string `json:"personalization"`
}

// Fields returns the editable field keys in display order.
func Fields() []string {
	return []string{"name", "hobby", "personalization"}
}

// Get returns the value of a named field.
func (p Profile) Get(field string) string {
	switch field {
	case "name":
		return p.Name
	case "hobby":
		return p.Hobby
	case "personalization":
		return p.Personalization
	}
	return ""
}

// Set updates the value of a named field.
func (p *Profile) Set(field, value string) {
	value = strings.TrimSpace(value)
	switch field {
	case "name":
		p.Name = value
	case "hobby":
		p.Hobby = value
	case "personalization":
		p.Personalization = value
	}
}

// Prompt composes the profile into a system-prompt snippet. Empty fields are
// skipped; an empty profile yields an empty string.
func (p Profile) Prompt() string {
	var parts []string
	if p.Name != "" {
		parts = append(parts, "Name: "+p.Name)
	}
	if p.Hobby != "" {
		parts = append(parts, "Hobby: "+p.Hobby)
	}
	if p.Personalization != "" {
		parts = append(parts, "Personalization: "+p.Personalization)
	}
	return strings.Join(parts, "\n")
}

// IsEmpty reports whether the profile has no content.
func (p Profile) IsEmpty() bool {
	return p.Name == "" && p.Hobby == "" && p.Personalization == ""
}

// Load returns the saved user profile, or an empty profile if none exists.
func Load() (Profile, error) {
	data, err := os.ReadFile(memoryFile)
	if err != nil {
		return Profile{}, err
	}
	var p Profile
	if err := json.Unmarshal(data, &p); err == nil && !p.IsEmpty() {
		return p, nil
	}
	// legacy: old flat {"profile":"..."} files
	var legacy struct {
		Profile string `json:"profile"`
	}
	if err := json.Unmarshal(data, &legacy); err == nil && strings.TrimSpace(legacy.Profile) != "" {
		return Profile{Personalization: legacy.Profile}, nil
	}
	return Profile{}, nil
}

// Save persists the user profile. An empty profile clears the memory.
func Save(p Profile) error {
	if err := os.MkdirAll(filepath.Dir(memoryFile), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(memoryFile, data, 0o600)
}
