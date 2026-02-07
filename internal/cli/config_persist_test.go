package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/llmmodel"
)

func isolateUserConfigWithHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LOCALAPPDATA", t.TempDir())
	return home
}

func readJSONObj(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var obj map[string]any
	if err := json.Unmarshal(b, &obj); err != nil {
		t.Fatalf("unmarshal %s: %v\ncontents=%q", path, err, string(b))
	}
	if obj == nil {
		obj = map[string]any{}
	}
	return obj
}

func TestPersistPreferredModel_UsesProvidenceFileWhenSet(t *testing.T) {
	home := isolateUserConfigWithHome(t)

	globalCfgPath := filepath.Join(home, ".codalotl", "config.json")
	if err := os.MkdirAll(filepath.Dir(globalCfgPath), 0755); err != nil {
		t.Fatalf("mkdir global cfg dir: %v", err)
	}
	if err := os.WriteFile(globalCfgPath, []byte(`{"preferredmodel":"gpt-old","reflowwidth":100}`+"\n"), 0644); err != nil {
		t.Fatalf("write global cfg: %v", err)
	}

	projectDir := t.TempDir()
	projectCfgPath := filepath.Join(projectDir, ".codalotl", "config.json")
	if err := os.MkdirAll(filepath.Dir(projectCfgPath), 0755); err != nil {
		t.Fatalf("mkdir project cfg dir: %v", err)
	}
	if err := os.WriteFile(projectCfgPath, []byte(`{"reflowwidth":80}`+"\n"), 0644); err != nil {
		t.Fatalf("write project cfg: %v", err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if got := cfg.PreferredModelProvidence.SourceIdentifier; got != globalCfgPath {
		t.Fatalf("expected preferredmodel to come from global config.\nwant=%q\ngot=%q", globalCfgPath, got)
	}

	if err := persistPreferredModelID(cfg, llmmodel.ModelID("gpt-new")); err != nil {
		t.Fatalf("persistPreferredModelID: %v", err)
	}

	globalObj := readJSONObj(t, globalCfgPath)
	if got := globalObj["preferredmodel"]; got != "gpt-new" {
		t.Fatalf("expected global preferredmodel to be updated, got %v", got)
	}
	if got := globalObj["reflowwidth"]; got != float64(100) {
		t.Fatalf("expected global reflowwidth to be preserved, got %v", got)
	}

	projectObj := readJSONObj(t, projectCfgPath)
	if _, ok := projectObj["preferredmodel"]; ok {
		t.Fatalf("expected project config to remain without preferredmodel, got %v", projectObj["preferredmodel"])
	}
	if got := projectObj["reflowwidth"]; got != float64(80) {
		t.Fatalf("expected project reflowwidth to be preserved, got %v", got)
	}
}

func TestPersistPreferredModel_UsesHighestPrecedenceFileWhenUnset(t *testing.T) {
	_ = isolateUserConfigWithHome(t)

	projectDir := t.TempDir()
	projectCfgPath := filepath.Join(projectDir, ".codalotl", "config.json")
	if err := os.MkdirAll(filepath.Dir(projectCfgPath), 0755); err != nil {
		t.Fatalf("mkdir project cfg dir: %v", err)
	}
	if err := os.WriteFile(projectCfgPath, []byte(`{"reflowwidth":80}`+"\n"), 0644); err != nil {
		t.Fatalf("write project cfg: %v", err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.PreferredModelProvidence.IsSet() {
		t.Fatalf("expected preferredmodel providence to be unset, got %+v", cfg.PreferredModelProvidence)
	}

	if err := persistPreferredModelID(cfg, llmmodel.ModelID("gpt-new")); err != nil {
		t.Fatalf("persistPreferredModelID: %v", err)
	}

	projectObj := readJSONObj(t, projectCfgPath)
	if got := projectObj["preferredmodel"]; got != "gpt-new" {
		t.Fatalf("expected project preferredmodel to be updated, got %v", got)
	}
	if got := projectObj["reflowwidth"]; got != float64(80) {
		t.Fatalf("expected project reflowwidth to be preserved, got %v", got)
	}
}

func TestPersistPreferredModel_CreatesGlobalConfigWhenNoFiles(t *testing.T) {
	home := isolateUserConfigWithHome(t)

	projectDir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if len(cfg.configLocations) != 0 {
		t.Fatalf("expected no config locations, got %v", cfg.configLocations)
	}

	if err := persistPreferredModelID(cfg, llmmodel.ModelID("gpt-new")); err != nil {
		t.Fatalf("persistPreferredModelID: %v", err)
	}

	globalCfgPath := filepath.Join(home, ".codalotl", "config.json")
	obj := readJSONObj(t, globalCfgPath)
	if got := obj["preferredmodel"]; got != "gpt-new" {
		t.Fatalf("expected global preferredmodel to be set, got %v", got)
	}

	// Clearing the model should remove the key.
	if err := persistPreferredModelID(cfg, llmmodel.ModelID("")); err != nil {
		t.Fatalf("persistPreferredModelID(clear): %v", err)
	}
	obj2 := readJSONObj(t, globalCfgPath)
	if _, ok := obj2["preferredmodel"]; ok {
		t.Fatalf("expected preferredmodel to be removed, got %v", obj2["preferredmodel"])
	}
}
