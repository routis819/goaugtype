package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"go/version"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/routiz/goaugt/pkg/analyzer"
)

func getLocalGOROOT() (string, error) {
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return "", fmt.Errorf("failed to run 'go env GOROOT': %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Config represents the schema for upstream.json.
type Config struct {
	UpstreamRepo string            `json:"upstream_repo"`
	BaseRevision string            `json:"base_revision"`
	TrackedFiles []string          `json:"tracked_files"`
	FileHashes   map[string]string `json:"file_hashes"`
}

func getGitHubToken() string {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}
	return os.Getenv("GH_TOKEN")
}

func getLatestReleaseTag(repo string) (string, error) {
	if repo == "" {
		repo = "golang/go"
	}
	client := &http.Client{Timeout: 30 * time.Second}
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/git/matching-refs/tags/go1.", repo)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "goaugtype-upstreamchecker")
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if token := getGitHubToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET %s failed: %w", apiURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API returned status %d from %s: %s", resp.StatusCode, apiURL, string(body))
	}

	var refs []struct {
		Ref string `json:"ref"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&refs); err != nil {
		return "", fmt.Errorf("failed to decode GitHub refs from %s: %w", apiURL, err)
	}

	var highestTag string
	for _, r := range refs {
		name := strings.TrimPrefix(r.Ref, "refs/tags/")
		if !strings.HasPrefix(name, "go1.") {
			continue
		}
		if strings.Contains(name, "rc") || strings.Contains(name, "beta") {
			continue
		}
		if !version.IsValid(name) {
			continue
		}
		if highestTag == "" || version.Compare(name, highestTag) > 0 {
			highestTag = name
		}
	}

	if highestTag == "" {
		return "", fmt.Errorf("no valid go1.* release tags found in %s", repo)
	}
	return highestTag, nil
}

func getTrackedFileContent(repo, target, file string, isLocal bool, client *http.Client) ([]byte, error) {
	if isLocal {
		goroot, err := getLocalGOROOT()
		if err != nil {
			return nil, err
		}
		localPath := filepath.Join(goroot, file)
		data, err := os.ReadFile(localPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read local file %s: %w", localPath, err)
		}
		return data, nil
	}

	rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", repo, target, file)
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for %s: %w", rawURL, err)
	}
	req.Header.Set("User-Agent", "goaugtype-upstreamchecker")
	if token := getGitHubToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch %s (status %d): %s", rawURL, resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read body from %s: %w", rawURL, err)
	}
	return data, nil
}

func computeSHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config JSON in %s: %w", path, err)
	}
	if cfg.UpstreamRepo == "" {
		cfg.UpstreamRepo = "golang/go"
	}
	if cfg.FileHashes == nil {
		cfg.FileHashes = make(map[string]string)
	}
	return &cfg, nil
}

func saveConfig(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config to %s: %w", path, err)
	}
	return nil
}

func main() {
	configPath := flag.String("config", "upstream.json", "path to config file")
	targetFlag := flag.String("target", "", "target revision to compare against (empty for latest release tag, 'local' for GOROOT, or explicit tag/branch/commit)")
	updateFlag := flag.Bool("update", false, "update base_revision and file_hashes in config file with target values")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if len(cfg.TrackedFiles) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no tracked files specified in %s\n", *configPath)
		os.Exit(1)
	}

	if !*updateFlag {
		if cfg.BaseRevision != analyzer.UpstreamBaseRevision {
			fmt.Fprintf(os.Stderr, "implementation mismatch: upstream.json base_revision (%s) != analyzer.UpstreamBaseRevision (%s)\n", cfg.BaseRevision, analyzer.UpstreamBaseRevision)
			os.Exit(1)
		}
	}

	var targetRevision string
	var isLocal bool

	switch *targetFlag {
	case "local":
		isLocal = true
		targetRevision = strings.Fields(runtime.Version())[0]
	case "":
		isLocal = false
		tag, err := getLatestReleaseTag(cfg.UpstreamRepo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error resolving latest release tag: %v\n", err)
			os.Exit(1)
		}
		targetRevision = tag
	default:
		isLocal = false
		targetRevision = *targetFlag
	}

	client := &http.Client{Timeout: 30 * time.Second}
	targetHashes := make(map[string]string)

	for _, file := range cfg.TrackedFiles {
		data, err := getTrackedFileContent(cfg.UpstreamRepo, targetRevision, file, isLocal, client)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error processing tracked file %q: %v\n", file, err)
			os.Exit(1)
		}
		targetHashes[file] = computeSHA256Hex(data)
	}

	if *updateFlag {
		cfg.BaseRevision = targetRevision
		cfg.FileHashes = targetHashes
		if err := saveConfig(*configPath, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Updated %s: base_revision=%s with %d tracked file hashes.\n", *configPath, cfg.BaseRevision, len(targetHashes))
		os.Exit(0)
	}

	// Normal check mode
	type diffEntry struct {
		file     string
		expected string
		actual   string
	}
	var diffs []diffEntry

	for _, file := range cfg.TrackedFiles {
		expectedHash := cfg.FileHashes[file]
		actualHash := targetHashes[file]
		if expectedHash != actualHash {
			diffs = append(diffs, diffEntry{
				file:     file,
				expected: expectedHash,
				actual:   actualHash,
			})
		}
	}

	if len(diffs) > 0 {
		fmt.Fprintf(os.Stderr, "Upstream check failed: %d of %d tracked file(s) differ from base revision %q (target revision %q):\n",
			len(diffs), len(cfg.TrackedFiles), cfg.BaseRevision, targetRevision)
		for _, d := range diffs {
			fmt.Fprintf(os.Stderr, "  - %s:\n      base:   %s\n      target: %s\n", d.file, d.expected, d.actual)
		}
		os.Exit(1)
	}

	if *targetFlag == "" && cfg.BaseRevision != targetRevision {
		fmt.Fprintf(os.Stderr, "revision mismatch: base_revision (%s) is not the latest official Go release (%s)\n", cfg.BaseRevision, targetRevision)
		os.Exit(1)
	}

	fmt.Printf("All %d tracked files match base revision %q (target revision %q).\n", len(cfg.TrackedFiles), cfg.BaseRevision, targetRevision)
	os.Exit(0)
}
