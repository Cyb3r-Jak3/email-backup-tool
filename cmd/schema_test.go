package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestConfigSchemaUpToDate fails if config.schema.json doesn't match what
// GenerateConfigSchema produces from the current ConfigV1 struct tags. If
// this fails after a legitimate config change, run:
//
//	go generate ./cmd/...
//
// and commit the result.
func TestConfigSchemaUpToDate(t *testing.T) {
	generated, err := GenerateConfigSchema()
	if err != nil {
		t.Fatalf("GenerateConfigSchema: %v", err)
	}
	want, err := json.MarshalIndent(generated, "", "  ")
	if err != nil {
		t.Fatalf("marshalling generated schema: %v", err)
	}
	want = append(want, '\n')

	got, err := os.ReadFile(filepath.Join("..", "config.schema.json"))
	if err != nil {
		t.Fatalf("reading config.schema.json: %v", err)
	}

	if string(got) != string(want) {
		t.Errorf("config.schema.json is stale; run `go generate ./cmd/...` and commit the result")
	}
}
