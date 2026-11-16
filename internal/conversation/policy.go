package conversation

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed policies/*.yaml
var embeddedPolicies embed.FS

var defaultCatalog = func() *Catalog {
	catalog, err := Load(embeddedPolicies)
	if err != nil {
		panic(fmt.Sprintf("invalid embedded conversation policies: %v", err))
	}
	return catalog
}()

// Default returns the validated, immutable embedded policy catalog.
func Default() *Catalog { return defaultCatalog }

type General struct {
	Instructions string `yaml:"instructions"`
}

type Policy struct {
	ID           string   `yaml:"id"`
	Description  string   `yaml:"description"`
	Required     []string `yaml:"required"`
	Optional     []string `yaml:"optional"`
	Actions      []string `yaml:"actions"`
	Instructions string   `yaml:"instructions"`
}

type Catalog struct {
	general  General
	policies map[string]Policy
}

func decode(data []byte, target any) error {
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple YAML documents are not allowed")
		}
		return err
	}
	return nil
}

// Load validates every policy at startup rather than allowing a malformed
// configuration to silently weaken the tool/action contract.
func Load(fsys fs.FS) (*Catalog, error) {
	data, err := fs.ReadFile(fsys, "policies/general.yaml")
	if err != nil {
		return nil, err
	}
	book := &Catalog{policies: make(map[string]Policy)}
	if err := decode(data, &book.general); err != nil {
		return nil, fmt.Errorf("general.yaml: %w", err)
	}
	if strings.TrimSpace(book.general.Instructions) == "" {
		return nil, fmt.Errorf("general.yaml: missing instructions")
	}
	paths, err := fs.Glob(fsys, "policies/*.yaml")
	if err != nil {
		return nil, err
	}
	for _, file := range paths {
		if file == "policies/general.yaml" {
			continue
		}
		data, err := fs.ReadFile(fsys, file)
		if err != nil {
			return nil, err
		}
		var policy Policy
		if err := decode(data, &policy); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		if policy.ID == "" || policy.ID != strings.TrimSuffix(path.Base(file), ".yaml") ||
			strings.TrimSpace(policy.Description) == "" || strings.TrimSpace(policy.Instructions) == "" {
			return nil, fmt.Errorf("%s: invalid id, description or instructions", file)
		}
		if len(policy.Actions) == 0 {
			return nil, fmt.Errorf("%s: missing actions", file)
		}
		fields := make(map[string]bool)
		for _, name := range append(append([]string{}, policy.Required...), policy.Optional...) {
			if strings.TrimSpace(name) == "" || fields[name] {
				return nil, fmt.Errorf("%s: duplicate or empty field %q", file, name)
			}
			fields[name] = true
		}
		actions := make(map[string]bool)
		for _, action := range policy.Actions {
			if action != "request_handoff" && action != "record_message" || actions[action] {
				return nil, fmt.Errorf("%s: invalid or duplicate action %q", file, action)
			}
			actions[action] = true
		}
		if _, exists := book.policies[policy.ID]; exists {
			return nil, fmt.Errorf("%s: duplicate category", file)
		}
		book.policies[policy.ID] = policy
	}
	if _, ok := book.policies["unknown"]; !ok {
		return nil, fmt.Errorf("missing required unknown policy")
	}
	return book, nil
}

func (c *Catalog) Policy(id string) (Policy, bool) {
	policy, ok := c.policies[id]
	return policy, ok
}

func (c *Catalog) Categories() []string {
	names := make([]string, 0, len(c.policies))
	for name := range c.policies {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Prompt exposes only the short category descriptions. The selected policy's
// detailed instructions are sent as the set_call_context tool response.
func (c *Catalog) Prompt() string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(c.general.Instructions))
	b.WriteString("\nAvailable categories (use unknown if unclear):\n")
	for _, name := range c.Categories() {
		p := c.policies[name]
		fmt.Fprintf(&b, "- %s: %s\n", p.ID, strings.TrimSpace(p.Description))
	}
	b.WriteString("Once the call's subject is clear, call set_call_context to select the category and record only facts explicitly supplied by the caller. Call it again as new substantive answers arrive. Use the returned category instructions and missing_fields to guide one question at a time. Before requesting any action, update the context with the latest caller facts. Never treat filler, silence, or an unfinished response as a known or unavailable field.")
	return b.String()
}
