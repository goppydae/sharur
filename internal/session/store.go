package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	TypeSession             = "session"
	TypeMessage             = "message"
	TypeModelChange         = "model_change"
	TypeThinkingLevelChange = "thinking_level_change"
	TypeCompaction          = "compaction"
	TypeBranchSummary       = "branch_summary"
	TypeCustom              = "custom"
	TypeCustomMessage       = "custom_message"
	TypeLabel               = "label"
	TypeSessionInfo         = "session_info"
)

// record is a single JSONL line in a session file.
type record struct {
	Type      string   `json:"type"`
	ID        string   `json:"id,omitempty"`
	ParentID  *string  `json:"parentId,omitempty"`
	Timestamp string   `json:"timestamp"` // ISO 8601

	// For "session" (header)
	Version       int     `json:"version,omitempty"`
	CWD           string  `json:"cwd,omitempty"`
	ParentSession *string `json:"parentSession,omitempty"`

	// For "message" and "custom_message"
	Message *message `json:"message,omitempty"` // nested message object

	// For "model_change"
	Provider string `json:"provider,omitempty"`
	Model    string `json:"modelId,omitempty"`

	// For "thinking_level_change"
	ThinkingLevel string `json:"thinkingLevel,omitempty"`

	// For "compaction" and "branch_summary"
	Summary          string `json:"summary,omitempty"`
	FirstKeptEntryID string `json:"firstKeptEntryId,omitempty"`
	TokensBefore     int    `json:"tokensBefore,omitempty"`
	TokensAfter      int    `json:"tokensAfter,omitempty"`
	FromID           string `json:"fromId,omitempty"` // For branch_summary

	// For "label"
	TargetID string `json:"targetId,omitempty"`
	Label    string `json:"label,omitempty"`

	// For "session_info"
	Name string `json:"name,omitempty"`

	// For "custom" and "custom_message"
	CustomType string          `json:"customType,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
}


// store handles low-level JSONL file I/O for a session directory.
type store struct {
	baseDir string
	dir     string
}

func newStore(baseDir, dir string) *store {
	return &store{baseDir: baseDir, dir: dir}
}

// path returns the file path for a given session ID.
// It searches the directory for a file ending in _ID.jsonl to handle timestamped filenames.
func (s *store) path(id string) string {
	if filepath.IsAbs(id) {
		return id
	}

	suffix := "_" + id + ".jsonl"

	// 1. Check current project directory (s.dir)
	p := filepath.Join(s.dir, id+".jsonl")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	entries, err := os.ReadDir(s.dir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), suffix) {
				return filepath.Join(s.dir, e.Name())
			}
		}
	}

	// 2. Scan all project directories in baseDir if it's set
	if s.baseDir != "" {
		projects, err := os.ReadDir(s.baseDir)
		if err == nil {
			for _, pentry := range projects {
				if !pentry.IsDir() {
					continue
				}
				pdir := filepath.Join(s.baseDir, pentry.Name())
				if pdir == s.dir {
					continue
				}

				// Check exact match in this project
				tp := filepath.Join(pdir, id+".jsonl")
				if _, err := os.Stat(tp); err == nil {
					return tp
				}

				// Check suffix match in this project
				pentries, err := os.ReadDir(pdir)
				if err == nil {
					for _, pe := range pentries {
						if !pe.IsDir() && strings.HasSuffix(pe.Name(), suffix) {
							return filepath.Join(pdir, pe.Name())
						}
					}
				}
			}
		}
	}

	return p // Fallback to current project path (even if it doesn't exist)
}

// appendRecord appends a single record to the session file.
func (s *store) appendRecord(path string, r record) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if r.Timestamp == "" {
		r.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}

	enc := json.NewEncoder(f)
	if err := enc.Encode(r); err != nil {
		return fmt.Errorf("encode record: %w", err)
	}
	return nil
}

// writePath serialises all entries of a session to JSONL at the given path.
// This is typically used for initial creation or migration.
func (s *store) writePath(records []record, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer func() { _ = f.Close() }()

	enc := json.NewEncoder(f)
	for _, r := range records {
		if r.Timestamp == "" {
			r.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
		}
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return nil
}

// initFile ensures the session file exists, writing a header record if it doesn't.
// Returns the resolved path for subsequent appends. Called on the first Append* for new sessions.
func (s *store) initFile(id string, createdAt time.Time, version int) (string, error) {
	path := s.path(id)
	if _, err := os.Stat(path); err == nil {
		return path, nil // already exists
	}
	ts := createdAt.UTC().Format("2006-01-02T15-04-05-000Z")
	ts = strings.ReplaceAll(ts, ".", "-")
	path = filepath.Join(s.dir, ts+"_"+id+".jsonl")
	header := record{
		Type:      TypeSession,
		ID:        id,
		Version:   version,
		Timestamp: createdAt.UTC().Format(time.RFC3339Nano),
	}
	return path, s.writePath([]record{header}, path)
}

// write serialises a set of records to JSONL using a timestamped filename in s.dir.
func (s *store) write(id string, records []record) error {
	path := s.path(id)

	// If the resolved path doesn't exist yet, it's a new session.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		ts := time.Now().UTC().Format("2006-01-02T15-04-05-000Z")
		ts = strings.ReplaceAll(ts, ".", "-")
		path = filepath.Join(s.dir, ts+"_"+id+".jsonl")
	}

	return s.writePath(records, path)
}

// readPath deserialises all JSONL records from the given path.
func (s *store) readPath(path string) ([]record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var records []record
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var r record
		if err := json.Unmarshal(line, &r); err != nil {
			// Try to handle legacy records if possible, or skip
			continue
		}

		// Backward compatibility: map "kind" to "type" and "system" to "systemPrompt"
		// This is a rough heuristic for old header records.
		if r.Type == "" {
			var legacy map[string]interface{}
			_ = json.Unmarshal(line, &legacy)
			if kind, ok := legacy["kind"].(string); ok {
				r.Type = kind
				if kind == "header" {
					r.Type = TypeSession
				}
			}
		}

		records = append(records, r)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

// read deserialises all JSONL records back into a slice by ID.
func (s *store) read(id string) ([]record, error) {
	return s.readPath(s.path(id))
}

// readSummary extracts session metadata and the first message for listing purposes.
func (s *store) readSummary(id string) (*SessionSummary, error) {
	path := s.path(id)
	records, err := s.readPath(path)
	if err != nil {
		return nil, err
	}

	sum := &SessionSummary{ID: id}
	for _, r := range records {
		switch r.Type {
		case TypeSession:
			sum.ParentID = r.ParentID
			if t, err := time.Parse(time.RFC3339Nano, r.Timestamp); err == nil {
				sum.CreatedAt = t
				sum.UpdatedAt = t
			}
		case TypeSessionInfo:
			if r.Name != "" {
				sum.Name = r.Name
			}
		case TypeMessage:
			if sum.FirstMessage == "" && r.Message != nil {
				sum.FirstMessage = r.Message.Content
			}
			if t, err := time.Parse(time.RFC3339Nano, r.Timestamp); err == nil {
				sum.UpdatedAt = t
			}
		}
	}

	return sum, nil
}

// list returns all session IDs in the directory.
// It correctly handles both flat UUID filenames and timestamped {TS}_{UUID}.jsonl filenames.
func (s *store) list() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	type entryInfo struct {
		id    string
		mtime time.Time
	}
	var infos []entryInfo

	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".jsonl" {
			info, err := e.Info()
			if err != nil {
				continue
			}

			// Strip the timestamp prefix (ts_uuid → uuid) so callers always work
			// with plain UUIDs. path() resolves uuid→ts_uuid.jsonl via suffix search.
			name := strings.TrimSuffix(e.Name(), ".jsonl")
			if i := strings.Index(name, "_"); i != -1 {
				name = name[i+1:]
			}
			infos = append(infos, entryInfo{
				id:    name,
				mtime: info.ModTime(),
			})
		}
	}

	// Sort by modification time ascending so latest is last
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].mtime.Before(infos[j].mtime)
	})

	var ids []string
	for _, info := range infos {
		ids = append(ids, info.id)
	}
	return ids, nil
}
