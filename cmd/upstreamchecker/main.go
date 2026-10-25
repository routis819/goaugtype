package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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

func updatePatches(snapshotDir, patchesDir, portedDir string, trackedFiles []string) error {
	if err := os.MkdirAll(patchesDir, 0755); err != nil {
		return fmt.Errorf("failed to create patches directory: %w", err)
	}

	for _, relPath := range trackedFiles {
		base := filepath.Base(relPath)
		stem := strings.TrimSuffix(base, ".go")
		pristineFile := filepath.Join(snapshotDir, base)
		portedFile := filepath.Join(portedDir, base)
		patchFile := filepath.Join(patchesDir, stem+".patch")

		cmd := exec.Command("git", "diff", "--no-index", "--src-prefix=a/", "--dst-prefix=b/", pristineFile, portedFile)
		out, err := cmd.Output()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
				return fmt.Errorf("failed to diff %s and %s: %w", pristineFile, portedFile, err)
			}
		}

		diffStr := string(out)
		diffStr = strings.ReplaceAll(diffStr, "a/"+pristineFile, "a/"+base)
		diffStr = strings.ReplaceAll(diffStr, "b/"+portedFile, "b/"+base)

		if err := os.WriteFile(patchFile, []byte(diffStr), 0644); err != nil {
			return fmt.Errorf("failed to write patch file %s: %w", patchFile, err)
		}
		fmt.Printf("Updated patch: %s\n", patchFile)
	}
	return nil
}

func applyPatches(snapshotDir, patchesDir, portedDir string, trackedFiles []string) error {
	rootCmd := exec.Command("git", "rev-parse", "--show-toplevel")
	rootOut, err := rootCmd.Output()
	var gitRoot string
	if err == nil {
		gitRoot = strings.TrimSpace(string(rootOut))
	}

	for _, relPath := range trackedFiles {
		base := filepath.Base(relPath)
		stem := strings.TrimSuffix(base, ".go")
		pristineFile := filepath.Join(snapshotDir, base)
		portedFile := filepath.Join(portedDir, base)
		patchFile := filepath.Join(patchesDir, stem+".patch")

		data, err := os.ReadFile(pristineFile)
		if err != nil {
			return fmt.Errorf("failed to read pristine snapshot %s: %w", pristineFile, err)
		}
		if err := os.WriteFile(portedFile, data, 0644); err != nil {
			return fmt.Errorf("failed to write ported file %s: %w", portedFile, err)
		}

		absPatchFile, err := filepath.Abs(patchFile)
		if err != nil {
			return err
		}

		absPortedDir, err := filepath.Abs(portedDir)
		if err != nil {
			return err
		}

		var cmd *exec.Cmd
		if gitRoot != "" {
			relPorted, err := filepath.Rel(gitRoot, absPortedDir)
			if err != nil {
				return err
			}
			cmd = exec.Command("git", "apply", "--3way", "--directory="+relPorted, absPatchFile)
			cmd.Dir = gitRoot
		} else {
			cmd = exec.Command("git", "apply", "--3way", absPatchFile)
			cmd.Dir = absPortedDir
		}

		if out, err := cmd.CombinedOutput(); err != nil {
			var cmd2 *exec.Cmd
			if gitRoot != "" {
				relPorted, _ := filepath.Rel(gitRoot, absPortedDir)
				cmd2 = exec.Command("git", "apply", "--directory="+relPorted, absPatchFile)
				cmd2.Dir = gitRoot
			} else {
				cmd2 = exec.Command("git", "apply", absPatchFile)
				cmd2.Dir = absPortedDir
			}
			if out2, err2 := cmd2.CombinedOutput(); err2 != nil {
				return fmt.Errorf("failed to apply patch %s to %s: %w\n%s\n%s", patchFile, portedFile, err2, string(out), string(out2))
			}
		}
		fmt.Printf("Applied patch %s -> %s\n", patchFile, portedFile)
	}
	return nil
}

func verifyPatches(snapshotDir, patchesDir, portedDir string, trackedFiles []string) error {
	tmpDir, err := os.MkdirTemp("", "goaugtype-patch-check-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	var drifts []string
	for _, relPath := range trackedFiles {
		base := filepath.Base(relPath)
		stem := strings.TrimSuffix(base, ".go")
		pristineFile := filepath.Join(snapshotDir, base)
		portedFile := filepath.Join(portedDir, base)
		patchFile := filepath.Join(patchesDir, stem+".patch")

		data, err := os.ReadFile(pristineFile)
		if err != nil {
			return fmt.Errorf("failed to read pristine snapshot %s: %w", pristineFile, err)
		}
		tmpFile := filepath.Join(tmpDir, base)
		if err := os.WriteFile(tmpFile, data, 0644); err != nil {
			return fmt.Errorf("failed to write temp file %s: %w", tmpFile, err)
		}

		absPatchFile, err := filepath.Abs(patchFile)
		if err != nil {
			return err
		}
		cmd := exec.Command("git", "apply", absPatchFile)
		cmd.Dir = tmpDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to apply patch %s in temp dir: %w\n%s", patchFile, err, string(out))
		}

		patchedData, err := os.ReadFile(tmpFile)
		if err != nil {
			return err
		}
		portedData, err := os.ReadFile(portedFile)
		if err != nil {
			return err
		}

		if computeSHA256Hex(patchedData) != computeSHA256Hex(portedData) {
			drifts = append(drifts, fmt.Sprintf("[DRIFT] %s and %s are out of sync (run 'make update-patches' or 'make generate')", portedFile, patchFile))
		}
	}

	if len(drifts) > 0 {
		return errors.New(strings.Join(drifts, "\n"))
	}
	return nil
}

func verifySnapshots(snapshotDir string, cfg *Config) error {
	for _, relPath := range cfg.TrackedFiles {
		base := filepath.Base(relPath)
		snapshotFile := filepath.Join(snapshotDir, base)
		data, err := os.ReadFile(snapshotFile)
		if err != nil {
			return fmt.Errorf("missing snapshot file %s: %w", snapshotFile, err)
		}
		hash := computeSHA256Hex(data)
		expected := cfg.FileHashes[relPath]
		if hash != expected {
			return fmt.Errorf("snapshot hash mismatch for %s: expected %s, got %s", base, expected, hash)
		}
	}
	return nil
}

func getAnalyzerUpstreamBaseRevision(rootDir string) (string, error) {
	adtFile := filepath.Join(rootDir, "pkg/analyzer/adt.go")
	data, err := os.ReadFile(adtFile)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "const UpstreamBaseRevision") {
			parts := strings.Split(line, "=")
			if len(parts) == 2 {
				return strings.Trim(strings.TrimSpace(parts[1]), `"`), nil
			}
		}
	}
	return "", fmt.Errorf("UpstreamBaseRevision not found in %s", adtFile)
}

func main() {
	configPath := flag.String("config", "upstream.json", "path to config file")
	targetFlag := flag.String("target", "", "target revision to compare against (empty for latest release tag, 'local' for GOROOT, or explicit tag/branch/commit)")
	updateFlag := flag.Bool("update", false, "update base_revision and file_hashes in config file with target values")
	applyPatchesFlag := flag.Bool("apply-patches", false, "apply upstream patches to pkg/analyzer")
	updatePatchesFlag := flag.Bool("update-patches", false, "generate/update upstream patches from pkg/analyzer")
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

	rootDir := filepath.Dir(*configPath)
	if rootDir == "" {
		rootDir = "."
	}
	snapshotDir := filepath.Join(rootDir, "internal/upstream/testdata/gotypes")
	patchesDir := filepath.Join(rootDir, "internal/upstream/patches")
	portedDir := filepath.Join(rootDir, "pkg/analyzer")

	if *updatePatchesFlag {
		if err := updatePatches(snapshotDir, patchesDir, portedDir, cfg.TrackedFiles); err != nil {
			fmt.Fprintf(os.Stderr, "Error updating patches: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Successfully updated %d patches in %s.\n", len(cfg.TrackedFiles), patchesDir)
		os.Exit(0)
	}

	if *applyPatchesFlag {
		if err := applyPatches(snapshotDir, patchesDir, portedDir, cfg.TrackedFiles); err != nil {
			fmt.Fprintf(os.Stderr, "Error applying patches: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Successfully applied %d patches to %s.\n", len(cfg.TrackedFiles), portedDir)
		os.Exit(0)
	}

	if !*updateFlag {
		adtBaseRevision, err := getAnalyzerUpstreamBaseRevision(rootDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error checking UpstreamBaseRevision: %v\n", err)
			os.Exit(1)
		}
		if cfg.BaseRevision != adtBaseRevision {
			fmt.Fprintf(os.Stderr, "implementation mismatch: upstream.json base_revision (%s) != analyzer.UpstreamBaseRevision (%s)\n", cfg.BaseRevision, adtBaseRevision)
			os.Exit(1)
		}

		if err := verifySnapshots(snapshotDir, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Snapshot check failed: %v\n", err)
			os.Exit(1)
		}

		if err := verifyPatches(snapshotDir, patchesDir, portedDir, cfg.TrackedFiles); err != nil {
			fmt.Fprintf(os.Stderr, "Patch check failed:\n%v\n", err)
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
	fetchedData := make(map[string][]byte)

	for _, file := range cfg.TrackedFiles {
		data, err := getTrackedFileContent(cfg.UpstreamRepo, targetRevision, file, isLocal, client)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error processing tracked file %q: %v\n", file, err)
			os.Exit(1)
		}
		targetHashes[file] = computeSHA256Hex(data)
		fetchedData[file] = data
	}

	if *updateFlag {
		for _, file := range cfg.TrackedFiles {
			base := filepath.Base(file)
			snapshotFile := filepath.Join(snapshotDir, base)
			data := fetchedData[file]
			if err := os.WriteFile(snapshotFile, data, 0644); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing snapshot file %s: %v\n", snapshotFile, err)
				os.Exit(1)
			}
		}

		cfg.BaseRevision = targetRevision
		cfg.FileHashes = targetHashes
		if err := saveConfig(*configPath, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Updated %s: base_revision=%s with %d tracked file hashes.\n", *configPath, cfg.BaseRevision, len(targetHashes))

		if err := applyPatches(snapshotDir, patchesDir, portedDir, cfg.TrackedFiles); err != nil {
			fmt.Fprintf(os.Stderr, "Error applying patches after update: %v\n", err)
			os.Exit(1)
		}
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
