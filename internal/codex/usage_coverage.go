package codex

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/pelletier/go-toml/v2"
)

const asyncCompactionCoverage = "background compaction is enabled; Codex does not publish all background response usage"

type localFeatureCheck struct {
	info    os.FileInfo
	enabled bool
	issue   string
}

// A positive local flag is enough to qualify file-only accounting. A negative
// file read cannot establish hidden per-process CLI overrides; authoritative
// loaded-thread feature state is preferred whenever the daemon supplies it.
func (r *LiveUsageReader) usageCoverage(ctx context.Context, sessions []LiveUsageSession, daemon map[string]string) string {
	if r.knownUsageCoverage == nil {
		r.knownUsageCoverage = make(map[string]string)
	}
	for id, issue := range daemon {
		r.knownUsageCoverage[id] = issue
	}
	issues := []string{}
	// Children are folded into their root's display row. Preserve independent
	// feature coverage for loaded/recent children before that grouping hides IDs.
	candidates := append([]LiveUsageSession(nil), sessions...)
	seen := make(map[string]bool)
	for _, session := range sessions {
		seen[session.ID] = true
	}
	for _, cursor := range r.files {
		if !seen[cursor.threadID] && cursor.threadID != "" && time.Since(cursor.lastModified) <= activeRolloutHorizon {
			candidates = append(candidates, LiveUsageSession{ID: cursor.threadID, WorkingDirectory: cursor.workingDirectory, Active: true})
			seen[cursor.threadID] = true
		}
	}
	for id := range r.knownUsageCoverage {
		if !seen[id] {
			delete(r.knownUsageCoverage, id)
		}
	}
	for _, session := range candidates {
		if !session.Active {
			continue
		}
		issue, verified := daemon[session.ID]
		if !verified {
			issue = r.knownUsageCoverage[session.ID]
		}
		if issue == "" && !verified {
			issue = r.localUsageCoverage(ctx, session.WorkingDirectory)
		}
		if issue != "" {
			issues = append(issues, "SESSION "+string([]rune(SanitizeSessionContext(session.ID))[:min(12, len([]rune(SanitizeSessionContext(session.ID))))])+": "+issue)
		}
	}
	if r.discoveryCoverageIssue != "" {
		issues = append(issues, r.discoveryCoverageIssue)
	}
	if len(issues) == 0 {
		return ""
	}
	sort.Strings(issues)
	return issues[0]
}

func (r *LiveUsageReader) localUsageCoverage(ctx context.Context, cwd string) string {
	paths := []string{filepath.Join(filepath.Dir(r.SessionsRoot), "config.toml")}
	if filepath.IsAbs(cwd) {
		for dir := filepath.Clean(cwd); ; dir = filepath.Dir(dir) {
			paths = append(paths, filepath.Join(dir, ".codex", "config.toml"))
			if filepath.Dir(dir) == dir {
				break
			}
		}
	}
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return "background usage configuration check interrupted"
		}
		enabled, issue := r.localAsyncCompaction(path)
		if issue != "" {
			return issue
		}
		if enabled {
			return "local configuration can enable background compaction; effective usage coverage is unverified"
		}
	}
	return ""
}

func (r *LiveUsageReader) localAsyncCompaction(path string) (bool, string) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, ""
	}
	if err != nil {
		return false, "background usage configuration could not be read"
	}
	if r.localFeatureChecks == nil {
		r.localFeatureChecks = make(map[string]localFeatureCheck)
	}
	if previous, ok := r.localFeatureChecks[path]; ok && os.SameFile(previous.info, info) && previous.info.ModTime() == info.ModTime() && previous.info.Size() == info.Size() {
		return previous.enabled, previous.issue
	}
	file, err := os.Open(path)
	if err != nil {
		return false, "background usage configuration could not be read"
	}
	data, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	file.Close()
	check := localFeatureCheck{info: info}
	var config map[string]any
	if err != nil || len(data) > 1<<20 || toml.Unmarshal(data, &config) != nil {
		check.issue = "background usage configuration could not be verified"
	} else {
		check.enabled = hasAsyncCompaction(config)
	}
	// Only active-session configuration is relevant; retain a bounded cache.
	if len(r.localFeatureChecks) >= 256 {
		clear(r.localFeatureChecks)
	}
	r.localFeatureChecks[path] = check
	return check.enabled, check.issue
}

func hasAsyncCompaction(config map[string]any) bool {
	if features, ok := config["features"].(map[string]any); ok {
		switch value := features["async_compaction"].(type) {
		case bool:
			if value {
				return true
			}
		case map[string]any:
			if enabled, ok := value["enabled"].(bool); ok && enabled {
				return true
			}
		}
	}
	if profiles, ok := config["profiles"].(map[string]any); ok {
		for _, profile := range profiles {
			if values, ok := profile.(map[string]any); ok && hasAsyncCompaction(values) {
				return true
			}
		}
	}
	return false
}
