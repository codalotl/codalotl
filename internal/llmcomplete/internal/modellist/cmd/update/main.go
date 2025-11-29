package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/codalotl/codalotl/internal/llmcomplete/internal/modellist"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// This command updates the files in modellist/config to be up-to-date. It only updates providers in modellist.ProviderNames(). In theory it can do this via any mechanism.
//
// Currently, it does it like this:
//   - For each provider in modellist, get a JSON file from https://raw.githubusercontent.com/charmbracelet/catwalk/refs/heads/main/internal/providers/configs/PROVIDER_NAME.json
//   - Sanitize JSON. There's some things we need to add/rmeove.
//   - assert json fields are consistent (did they add/remove fields we need to be aware of?)
//   - copy file over to modellist/config/PROVIDER_NAME.json
//
// It also checks for added/removed providers.
//
// In the future, if this source goes stale, we have choices.
//   - We could keep the JSON format consistent and find another way to populate it.
//   - If we have another JSON source, we could change the JSON format and adapt at the modellist abstraction layer
//   - etc
func main() {
	fmt.Println("starting modellist/cmd/update")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	localProviders := modellist.GetProviderNames()
	if len(localProviders) == 0 {
		fmt.Println("no local providers found; nothing to do")
		return
	}

	// Discover remote provider configs via GitHub API
	remoteProviders, err := fetchRemoteProviderList(ctx)
	if err != nil {
		fmt.Printf("failed to fetch remote provider list: %v\n", err)
		os.Exit(1)
	}

	// Check for added/removed providers relative to local list
	added, removed := diffProviderSets(localProviders, keys(remoteProviders))
	if len(added) > 0 {
		fmt.Printf("remote has additional providers not in modellist.ProviderNames(): %v\n", added)
	}
	if len(removed) > 0 {
		fmt.Printf("modellist.ProviderNames() has providers missing remotely: %v\n", removed)
	}

	// Choose destination directory for config files
	destDir := resolveConfigDir()
	if destDir == "" {
		fmt.Println("could not locate destination config directory (expected codeai/llmcomplete/internal/modellist/config)")
		os.Exit(1)
	}

	// Update each local provider from remote source
	hadSchemaDiff := false
	hadModelDiff := false
	for _, provider := range localProviders {
		downloadURL, ok := remoteProviders[provider]
		if !ok {
			fmt.Printf("skipping %q: no remote config found\n", provider)
			continue
		}

		newBytes, err := fetchRemoteConfig(ctx, downloadURL)
		if err != nil {
			fmt.Printf("failed to download %s config: %v\n", provider, err)
			os.Exit(1)
		}

		// Sanitize the downloaded JSON before any comparisons or writes
		sanitizedBytes, err := ApplySanitizers(newBytes,
			RemoveDefaultHeaders,
			RenameAPIEndpoint,
			SetMissingAPIEndpoint,
			SetIsLegacy,
			FixProviderData,
			OrderKeys,
		)
		if err != nil {
			fmt.Printf("failed to sanitize %s config: %v\n", provider, err)
			os.Exit(1)
		}

		// Compare top-level JSON keys to detect schema drift, if existing local JSON is valid
		dstPath := filepath.Join(destDir, provider+".json")
		oldBytes, _ := os.ReadFile(dstPath)

		oldKeys, oldOK := parseTopLevelKeys(oldBytes)
		newKeys, newOK := parseTopLevelKeys(sanitizedBytes)

		if oldOK && newOK {
			addedKeys, removedKeys := diffStringSets(keysFromMap(oldKeys), keysFromMap(newKeys))
			if len(addedKeys) > 0 || len(removedKeys) > 0 {
				hadSchemaDiff = true
				fmt.Printf("schema change detected for %s: added=%v removed=%v\n", provider, addedKeys, removedKeys)
			}
		}

		// Per-model schema diffs and model set diffs
		oldModels := parseModels(oldBytes)
		newModels := parseModels(sanitizedBytes)
		addedModels, removedModels := diffModelSets(oldModels, newModels)
		if len(addedModels) > 0 || len(removedModels) > 0 {
			hadModelDiff = true
			fmt.Printf("model set change for %s: added=%v removed=%v\n", provider, addedModels, removedModels)
		}
		// For models present in both, compare their top-level keys
		for modelID := range intersectionKeys(oldModels, newModels) {
			oldM := oldModels[modelID]
			newM := newModels[modelID]
			addedK, removedK := diffStringSets(keysFromMap(oldM), keysFromMap(newM))
			if len(addedK) > 0 || len(removedK) > 0 {
				hadModelDiff = true
				fmt.Printf("model schema change for %s/%s: added=%v removed=%v\n", provider, modelID, addedK, removedK)
			}
		}

		if err := os.MkdirAll(destDir, 0o755); err != nil {
			fmt.Printf("failed to ensure destination dir %s: %v\n", destDir, err)
			os.Exit(1)
		}

		if err := os.WriteFile(dstPath, sanitizedBytes, 0o644); err != nil {
			fmt.Printf("failed to write %s: %v\n", dstPath, err)
			os.Exit(1)
		}
		fmt.Printf("updated %s\n", dstPath)
	}

	// If schema changed, model details changed, or provider set differs, exit non-zero to draw attention
	if hadSchemaDiff || hadModelDiff || len(added) > 0 || len(removed) > 0 {
		fmt.Println("completed with notices (schema/provider set changed)")
		os.Exit(1)
	}

	fmt.Println("update complete; no schema/provider-set changes detected")
}

// fetchRemoteProviderList returns provider name -> direct download URL for JSON
func fetchRemoteProviderList(ctx context.Context) (map[string]string, error) {
	const apiURL = "https://api.github.com/repos/charmbracelet/catwalk/contents/internal/providers/configs?ref=main"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "axi-modellist-update")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var contents []struct {
		Name        string `json:"name"`
		Type        string `json:"type"`
		DownloadURL string `json:"download_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&contents); err != nil {
		return nil, err
	}

	out := make(map[string]string)
	for _, c := range contents {
		if c.Type != "file" {
			continue
		}
		if !strings.HasSuffix(c.Name, ".json") {
			continue
		}
		name := strings.TrimSuffix(c.Name, ".json")
		out[name] = c.DownloadURL
	}
	return out, nil
}

func fetchRemoteConfig(ctx context.Context, downloadURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "axi-modellist-update")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}
	return io.ReadAll(resp.Body)
}

func resolveConfigDir() string {
	// Resolve relative to this source file location so execution from any CWD works.
	if _, thisFile, _, ok := runtime.Caller(0); ok {
		baseDir := filepath.Dir(thisFile) // .../codeai/llmcomplete/internal/modellist/cmd/update
		candidate := filepath.Clean(filepath.Join(baseDir, "..", "..", "config"))
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate
		}
	}

	// Fallback: repo-root relative path (useful when running via `go run` from repo root)
	preferred := filepath.Join("codeai", "llmcomplete", "internal", "modellist", "config")
	if st, err := os.Stat(preferred); err == nil && st.IsDir() {
		return preferred
	}
	return ""
}

func parseTopLevelKeys(b []byte) (map[string]struct{}, bool) {
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil, false
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, false
	}
	keys := make(map[string]struct{}, len(m))
	for k := range m {
		keys[k] = struct{}{}
	}
	return keys, true
}

// parseModels reads the provider JSON and returns a map of modelID -> set of top-level keys for that model object.
func parseModels(b []byte) map[string]map[string]struct{} {
	type rawProvider struct {
		Models []map[string]interface{} `json:"models"`
	}
	var rp rawProvider
	if err := json.Unmarshal(b, &rp); err != nil {
		return map[string]map[string]struct{}{}
	}
	out := make(map[string]map[string]struct{}, len(rp.Models))
	for _, m := range rp.Models {
		idVal, ok := m["id"].(string)
		if !ok || idVal == "" {
			continue
		}
		keySet := make(map[string]struct{}, len(m))
		for k := range m {
			keySet[k] = struct{}{}
		}
		out[idVal] = keySet
	}
	return out
}

func diffModelSets(oldModels, newModels map[string]map[string]struct{}) (added, removed []string) {
	return diffStringSets(keysFromMapOfSets(oldModels), keysFromMapOfSets(newModels))
}

func keysFromMapOfSets(m map[string]map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func intersectionKeys(a, b map[string]map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{})
	for k := range a {
		if _, ok := b[k]; ok {
			out[k] = struct{}{}
		}
	}
	return out
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func keysFromMap(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func diffStringSets(a, b []string) (added, removed []string) {
	setA := make(map[string]struct{}, len(a))
	setB := make(map[string]struct{}, len(b))
	for _, s := range a {
		setA[s] = struct{}{}
	}
	for _, s := range b {
		setB[s] = struct{}{}
	}
	for s := range setB {
		if _, ok := setA[s]; !ok {
			added = append(added, s)
		}
	}
	for s := range setA {
		if _, ok := setB[s]; !ok {
			removed = append(removed, s)
		}
	}
	return
}

func diffProviderSets(local []string, remote []string) (added, removed []string) {
	return diffStringSets(local, remote)
}
