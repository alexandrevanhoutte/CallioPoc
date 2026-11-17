package logging

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func entries(t *testing.T, data string) []map[string]any {
	t.Helper()
	var records []map[string]any
	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("invalid JSON log %q: %v", scanner.Text(), err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return records
}

func TestJSONLevelAndFields(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(&output, "info")
	if err != nil {
		t.Fatal(err)
	}
	logger.Debug("hidden")
	logger.Info("server started", "addr", "127.0.0.1:8080")
	logger.Error("failed", "status", 503)

	actual := entries(t, output.String())
	if len(actual) != 2 {
		t.Fatalf("expected exactly two lines, got %d: %s", len(actual), output.String())
	}
	if actual[0]["time"] == nil || actual[0]["level"] != "INFO" ||
		actual[0]["msg"] != "server started" || actual[0]["addr"] != "127.0.0.1:8080" {
		t.Fatalf("unexpected first log entry: %#v", actual[0])
	}
	if actual[1]["level"] != "ERROR" || actual[1]["status"] != float64(503) {
		t.Fatalf("unexpected error log: %#v", actual[1])
	}
}

func TestComponentAndWithDoNotMutateParent(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(&output, "debug")
	if err != nil {
		t.Fatal(err)
	}
	child := logger.With("component", "gemini.live").With("session_id", "test")
	child.Debug("child")
	logger.Info("root")
	records := entries(t, output.String())
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[0]["component"] != "gemini.live" || records[0]["session_id"] != "test" {
		t.Fatalf("child missing attributes: %#v", records[0])
	}
	if _, ok := records[1]["component"]; ok {
		t.Fatalf("parent mutated: %#v", records[1])
	}
}

func TestSupportedLevelsAndInvalidConfig(t *testing.T) {
	for _, level := range []string{"", "info", "INFO", "debug", "warn", "error"} {
		if _, err := New(&bytes.Buffer{}, level); err != nil {
			t.Errorf("%q: %v", level, err)
		}
	}
	for _, level := range []string{"trace", "verbose", "off"} {
		if _, err := New(&bytes.Buffer{}, level); err == nil {
			t.Errorf("invalid level %q accepted", level)
		}
		if err := Init(level); err == nil {
			t.Errorf("invalid Init level %q accepted", level)
		}
	}
	if _, err := New(nil, "info"); err == nil {
		t.Fatal("nil output accepted")
	}
	if L() == nil || For("httpapi") == nil {
		t.Fatal("root/fallback logger missing")
	}
}

func TestConcurrentStructuredLogging(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(&output, "debug")
	if err != nil {
		t.Fatal(err)
	}
	const workers, messages = 8, 25
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			child := logger.With("worker", worker)
			for n := 0; n < messages; n++ {
				child.Info("message", "sequence", n)
			}
		}(i)
	}
	wg.Wait()
	records := entries(t, output.String())
	if len(records) != workers*messages {
		t.Fatalf("want %d records; got %d", workers*messages, len(records))
	}
	for i, record := range records {
		if record["worker"] == nil || record["sequence"] == nil || record["msg"] != "message" {
			t.Fatalf("invalid concurrent record %d: %#v", i, record)
		}
	}
}
