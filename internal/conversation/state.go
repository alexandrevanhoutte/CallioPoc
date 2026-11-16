package conversation

import (
	"fmt"
	"slices"
	"strings"
)

type FieldUpdate struct {
	Key   string
	Value string
}

// State is per-call and accessed by the provider's single receive goroutine.
// It is a tool-declared, best-effort state: not independent transcription proof.
type State struct {
	catalog     *Catalog
	category    string
	known       map[string]string
	unavailable map[string]bool
}

func NewState(catalog *Catalog) *State {
	return &State{catalog: catalog, known: make(map[string]string), unavailable: make(map[string]bool)}
}

func (s *State) Category() string { return s.category }

func (s *State) Value(key string) (string, bool) {
	value, ok := s.known[key]
	return value, ok
}

func (s *State) Update(category string, updates []FieldUpdate, unavailable []string) (Policy, error) {
	policy, ok := s.catalog.Policy(category)
	if !ok {
		return Policy{}, fmt.Errorf("unknown category %q", category)
	}
	if len(updates) > 12 || len(unavailable) > 12 {
		return Policy{}, fmt.Errorf("too many field updates")
	}
	valid := func(key string) bool {
		return slices.Contains(policy.Required, key) || slices.Contains(policy.Optional, key)
	}
	known := make(map[string]string)
	declined := make(map[string]bool)
	if s.category == category {
		for key, value := range s.known { known[key] = value }
		for key, value := range s.unavailable { declined[key] = value }
	} else if valid("caller_name") {
		// Keep the caller's previously provided name when intent changes.
		if name, ok := s.known["caller_name"]; ok { known["caller_name"] = name }
	}
	seen := make(map[string]bool)
	for _, field := range updates {
		value := strings.TrimSpace(field.Value)
		if !valid(field.Key) || value == "" || len(value) > 400 || seen[field.Key] {
			return Policy{}, fmt.Errorf("invalid field update %q", field.Key)
		}
		seen[field.Key] = true
		known[field.Key] = value
		delete(declined, field.Key)
	}
	for _, key := range unavailable {
		if !valid(key) || seen[key] {
			return Policy{}, fmt.Errorf("invalid unavailable field %q", key)
		}
		seen[key] = true
		if _, exists := known[key]; !exists {
			declined[key] = true
		}
	}
	s.category = category
	s.known = known
	s.unavailable = declined
	return policy, nil
}

func (s *State) Missing() []string {
	policy, ok := s.catalog.Policy(s.category)
	if !ok {
		return nil
	}
	var missing []string
	for _, key := range policy.Required {
		if _, known := s.known[key]; known { continue }
		if s.unavailable[key] { continue }
		missing = append(missing, key)
	}
	return missing
}

func (s *State) Allow(action string) error {
	policy, ok := s.catalog.Policy(s.category)
	if !ok {
		return fmt.Errorf("choose a category with set_call_context before requesting an action")
	}
	if !slices.Contains(policy.Actions, action) {
		return fmt.Errorf("action %s is not allowed for category %s", action, policy.ID)
	}
	if missing := s.Missing(); len(missing) != 0 {
		return fmt.Errorf("information still needed: %s; ask one question and wait for the caller's answer or an explicit refusal", strings.Join(missing, ", "))
	}
	return nil
}
