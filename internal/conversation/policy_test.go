package conversation

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestEmbeddedCatalog(t *testing.T) {
	c := Default()
	if len(c.Categories()) != 4 {
		t.Fatalf("expected four categories, got %v", c.Categories())
	}
	prompt := c.Prompt()
	for _, required := range []string{"one question at a time", "switch with them immediately", "set_call_context", "recruiting", "sales", "medical", "unknown"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("missing %q in global prompt", required)
		}
	}
	sales, _ := c.Policy("sales")
	if len(sales.Actions) != 1 || sales.Actions[0] != "record_message" {
		t.Fatal("commercial callers must never be offered a live handoff")
	}
}

func TestLoadValidation(t *testing.T) {
	validGeneral := []byte("instructions: global rules\n")
	for _, tc := range []struct{ name, policy string }{
		{"unknown key", "id: unknown\ndescription: Other\ninstructions: Be brief\nactions: [record_message]\nextra: invalid\n"},
		{"invalid action", "id: unknown\ndescription: Other\ninstructions: Be brief\nactions: [do_anything]\n"},
		{"duplicate field", "id: unknown\ndescription: Other\ninstructions: Be brief\nrequired: [name, name]\nactions: [record_message]\n"},
		{"invalid file id", "id: sales\ndescription: Other\ninstructions: Be brief\nactions: [record_message]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys := fstest.MapFS{"policies/general.yaml": {Data: validGeneral}, "policies/unknown.yaml": {Data: []byte(tc.policy)}}
			if _, err := Load(fsys); err == nil {
				t.Fatal("expected malformed policy to be rejected")
			}
		})
	}
}

func TestInterviewRequiresPositionBeforeHandoff(t *testing.T) {
	s := NewState(Default())
	_, err := s.Update("recruiting", []FieldUpdate{{Key: "caller_name", Value: "Thomas"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Allow("request_handoff"); err == nil || !strings.Contains(err.Error(), "position") {
		t.Fatalf("handoff should be blocked on missing position: %v", err)
	}
	if _, err := s.Update("recruiting", []FieldUpdate{{Key: "position", Value: "Backend Engineer"}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Allow("request_handoff"); err != nil {
		t.Fatal(err)
	}
}

func TestRefusalAndCategorySwitch(t *testing.T) {
	s := NewState(Default())
	_, err := s.Update("recruiting", nil, []string{"caller_name", "position"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Allow("request_handoff"); err != nil {
		t.Fatal(err)
	}
	_, err = s.Update("sales", []FieldUpdate{{Key: "caller_name", Value: "Thomas"}, {Key: "offer_description", Value: "Shoes"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Allow("request_handoff"); err == nil {
		t.Fatal("sales must never initiate a live handoff")
	}
	if err := s.Allow("record_message"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update("medical", []FieldUpdate{{Key: "not_a_field", Value: "bad"}}, nil); err == nil {
		t.Fatal("unknown fields must be rejected")
	}
	if s.Category() != "sales" {
		t.Fatal("failed update must not change category")
	}
}

func TestPartialUpdatesDoNotClobberEarlierAnswers(t *testing.T) {
	s := NewState(Default())
	_, err := s.Update("recruiting", []FieldUpdate{{Key: "caller_name", Value: "Thomas"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Update("recruiting", nil, []string{"position"})
	if err != nil {
		t.Fatal(err)
	}
	if name, _ := s.Value("caller_name"); name != "Thomas" {
		t.Fatalf("lost prior name %q", name)
	}
	if err := s.Allow("request_handoff"); err != nil {
		t.Fatal(err)
	}
	_, err = s.Update("unknown", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if name, _ := s.Value("caller_name"); name != "Thomas" {
		t.Fatal("name should survive a category change")
	}
	if err := s.Allow("request_handoff"); err == nil {
		t.Fatal("unknown purpose must still be requested")
	}
}

func TestPoliciesAvoidDemoDisclaimers(t *testing.T) {
	catalog := Default()
	for _, id := range catalog.Categories() {
		p, ok := catalog.Policy(id)
		if !ok {
			t.Fatalf("missing policy %s", id)
		}
		for _, unwanted := range []string{"local demo", "technical demo", "emergency services", "simulated takeover", "not connected to a phone"} {
			if strings.Contains(strings.ToLower(p.Instructions), unwanted) {
				t.Fatalf("%s contains an unsolicited demo disclaimer: %q", id, unwanted)
			}
		}
	}
	for _, unwanted := range []string{"local demo", "technical demo", "emergency services"} {
		if strings.Contains(strings.ToLower(catalog.Prompt()), unwanted) {
			t.Fatalf("global prompt contains unnecessary wording: %q", unwanted)
		}
	}
}
