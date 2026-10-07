package config

import (
	"encoding/json"
	"testing"
)

func TestDependsOnEntry_UnmarshalJSON_String(t *testing.T) {
	j := `{"depends_on": ["webview", "sdl2"]}`
	var c struct {
		DependsOn []DependsOnEntry `json:"depends_on"`
	}
	if err := json.Unmarshal([]byte(j), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(c.DependsOn) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(c.DependsOn))
	}
	if c.DependsOn[0].Name != "webview" || c.DependsOn[0].IsGit() {
		t.Errorf("entry[0] = %+v, want Name=webview IsGit=false", c.DependsOn[0])
	}
	if c.DependsOn[1].Name != "sdl2" || c.DependsOn[1].IsGit() {
		t.Errorf("entry[1] = %+v, want Name=sdl2 IsGit=false", c.DependsOn[1])
	}
	t.Logf("String format OK: %+v", c.DependsOn)
}

func TestDependsOnEntry_UnmarshalJSON_Mixed(t *testing.T) {
	j := `{"depends_on": ["webview", {"name":"sdl2","git":"https://github.com/user/sdl2.git","tag":"v2.0"}]}`
	var c struct {
		DependsOn []DependsOnEntry `json:"depends_on"`
	}
	if err := json.Unmarshal([]byte(j), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(c.DependsOn) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(c.DependsOn))
	}
	if c.DependsOn[0].Name != "webview" || c.DependsOn[0].IsGit() {
		t.Errorf("entry[0] = %+v", c.DependsOn[0])
	}
	if c.DependsOn[1].Name != "sdl2" || c.DependsOn[1].Git != "https://github.com/user/sdl2.git" || c.DependsOn[1].Tag != "v2.0" {
		t.Errorf("entry[1] = %+v", c.DependsOn[1])
	}
	t.Logf("Mixed format OK: %+v", c.DependsOn)
}

func TestDependsOnEntry_MarshalJSON_Roundtrip(t *testing.T) {
	original := []DependsOnEntry{
		{Name: "webview"},
		{Name: "custom", Git: "https://x.com/repo.git", Tag: "v1.0"},
	}
	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	t.Logf("Marshaled: %s", string(b))

	var decoded []DependsOnEntry
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(decoded))
	}
	if decoded[0].Name != "webview" || decoded[0].IsGit() {
		t.Errorf("entry[0] = %+v", decoded[0])
	}
	if decoded[1].Name != "custom" || decoded[1].Git != "https://x.com/repo.git" || decoded[1].Tag != "v1.0" {
		t.Errorf("entry[1] = %+v", decoded[1])
	}
	t.Logf("Round-trip OK: %+v", decoded)
}

func TestDependsOnEntry_MarshalJSON_Reversibility(t *testing.T) {
	j := `{"depends_on":["webview",{"name":"sdl2","git":"https://github.com/user/sdl2.git","tag":"v2.0"}]}`
	var c struct {
		DependsOn []DependsOnEntry `json:"depends_on"`
	}
	if err := json.Unmarshal([]byte(j), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	t.Logf("Re-marshaled:\n%s", string(b))
	// Re-parse to verify round-trip
	var c2 struct {
		DependsOn []DependsOnEntry `json:"depends_on"`
	}
	if err := json.Unmarshal(b, &c2); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if len(c2.DependsOn) != 2 {
		t.Fatalf("expected 2, got %d", len(c2.DependsOn))
	}
	if c2.DependsOn[1].Git != "https://github.com/user/sdl2.git" {
		t.Errorf("git lost: %+v", c2.DependsOn[1])
	}
}