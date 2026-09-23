package evals_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type evalCase struct {
	ID    string `json:"id"`
	Input struct {
		Question  string  `json:"question"`
		BirthTime string  `json:"birth_time"`
		Longitude float64 `json:"longitude"`
		Gender    string  `json:"gender"`
		Locale    string  `json:"locale"`
	} `json:"input"`
	Expect struct {
		RequiredTools  []string `json:"required_tools"`
		MustNotContain []string `json:"must_not_contain"`
		OutputSchema   string   `json:"output_schema"`
	} `json:"expect"`
}

func TestEvaluationCasesAreValid(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("cases", "*.json"))
	if err != nil {
		t.Fatalf("glob cases: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no evaluation cases found")
	}
	seen := make(map[string]string)
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			var testCase evalCase
			decoder := json.NewDecoder(strings.NewReader(string(raw)))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&testCase); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if testCase.ID == "" {
				t.Fatal("id is required")
			}
			if previous, exists := seen[testCase.ID]; exists {
				t.Fatalf("duplicate id %q in %q and %q", testCase.ID, previous, path)
			}
			seen[testCase.ID] = path
			if testCase.Input.Question == "" || testCase.Input.BirthTime == "" || testCase.Input.Gender == "" || testCase.Input.Locale == "" {
				t.Fatal("question, birth_time, gender, and locale are required")
			}
			if testCase.Input.Longitude < -180 || testCase.Input.Longitude > 180 {
				t.Fatalf("longitude %v is out of range", testCase.Input.Longitude)
			}
			if len(testCase.Expect.RequiredTools) == 0 {
				t.Fatal("at least one required tool is required")
			}
			if testCase.Expect.OutputSchema != "expert-opinion-v1" {
				t.Fatalf("output schema = %q", testCase.Expect.OutputSchema)
			}
		})
	}
}

func TestExpertOpinionContractIsJSON(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "contracts", "expert-opinion.schema.json"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if document["title"] != "ExpertOpinion" {
		t.Fatalf("schema title = %v", document["title"])
	}
}
