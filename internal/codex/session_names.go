package codex

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Codex's append-only name index stores renames as later entries. Metadata is
// display-only and optional: an unavailable index must not fail token polling.
// Called under the LiveUsageReader lock.
func (r *LiveUsageReader) refreshSessionNames() {
	if r.SessionsRoot == "" {
		return
	}
	file, err := os.Open(filepath.Join(filepath.Dir(r.SessionsRoot), "session_index.jsonl"))
	if err != nil {
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	if old := r.nameIndexInfo; old != nil && os.SameFile(old, info) && old.Size() == info.Size() && old.ModTime() == info.ModTime() {
		return
	}
	names := map[string]string{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		var entry struct {
			ID   string `json:"id"`
			Name string `json:"thread_name"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) != nil || entry.ID == "" {
			continue
		}
		names[entry.ID] = strings.Join(strings.Fields(SanitizeSessionContext(entry.Name)), " ")
	}
	if scanner.Err() != nil {
		return
	}
	r.sessionNames, r.nameIndexInfo = names, info
}
