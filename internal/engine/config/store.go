package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Store owns config.yaml: loading, validating, and saving it atomically.
//
// It keeps the exact bytes it loaded. Saving a configuration nobody changed
// writes those bytes back unaltered, so an operator's comments and formatting
// survive PortCloak having been opened (UC-O7).
type Store struct {
	mu sync.RWMutex

	home Home
	cfg  Config

	raw      []byte
	baseline []byte
	// locate maps a document path to the line it was read from, so a
	// validation problem can name a line in the operator's own file.
	locate func(string) int
}

// NewStore creates a store rooted at home. It does not read anything yet.
func NewStore(home Home) *Store {
	return &Store{home: home, cfg: Config{Version: 1}}
}

// Home is where this store's files live.
func (s *Store) Home() Home { return s.home }

// Load reads and validates config.yaml.
//
// A malformed file is an error rather than a partial parse: silently dropping
// the entries it could not read would lose an operator's environments without
// telling them (UC-O7 E1).
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.home.ConfigFile()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.cfg = Config{Version: 1}
			s.raw, s.baseline, s.locate = nil, nil, nil
			return nil
		}
		return fmt.Errorf("reading %s: %w", path, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return &ValidationError{File: path, Problems: []Problem{{
			Line:    yamlErrorLine(err),
			Message: "PortCloak could not read this file as YAML: " + trimYAMLPrefix(err.Error()),
		}}}
	}

	var cfg Config
	if len(doc.Content) > 0 {
		if err := doc.Content[0].Decode(&cfg); err != nil {
			return &ValidationError{File: path, Problems: []Problem{{
				Line:    yamlErrorLine(err),
				Message: "PortCloak could not make sense of this file: " + trimYAMLPrefix(err.Error()),
			}}}
		}
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}

	locate := newLocator(&doc)
	if problems := Validate(&cfg, locate); len(problems) > 0 {
		return &ValidationError{File: path, Problems: problems}
	}

	baseline, err := marshal(&cfg)
	if err != nil {
		return err
	}

	s.cfg, s.raw, s.baseline, s.locate = cfg, raw, baseline, locate
	return nil
}

// Config returns a deep-enough copy that a caller mutating slices cannot
// corrupt the store's own view.
func (s *Store) Config() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneConfig(s.cfg)
}

// Preferences returns the effective preferences, defaults filled in.
func (s *Store) Preferences() Preferences {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.Preferences.Resolved()
}

// Update applies fn to a copy of the configuration, validates the result, and
// saves it. Nothing is written if the result would be invalid, so a bad edit
// cannot destroy a good file.
func (s *Store) Update(fn func(*Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := cloneConfig(s.cfg)
	if err := fn(&next); err != nil {
		return err
	}
	if problems := Validate(&next, nil); len(problems) > 0 {
		return &ValidationError{File: s.home.ConfigFile(), Problems: problems}
	}
	return s.saveLocked(next)
}

// Save writes the current configuration.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(cloneConfig(s.cfg))
}

func (s *Store) saveLocked(next Config) error {
	out, err := marshal(&next)
	if err != nil {
		return err
	}

	// If nothing the schema knows about has changed, write back exactly what
	// was read. That is what keeps a hand-maintained file's comments and
	// layout intact across an app session.
	body := out
	if s.baseline != nil && bytes.Equal(out, s.baseline) && s.raw != nil {
		body = s.raw
	}

	if err := writeFileAtomic(s.home.ConfigFile(), body, 0o600); err != nil {
		return err
	}
	s.cfg = next
	s.raw = body
	s.baseline = out
	return nil
}

func marshal(c *Config) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(c); err != nil {
		return nil, fmt.Errorf("writing the configuration: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeFileAtomic writes through a temp file in the same directory, fsyncs it,
// and renames. A crash during a save therefore leaves the previous file whole
// rather than a truncated one that has lost every environment the operator
// ever defined.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".portcloak-*.tmp")
	if err != nil {
		return fmt.Errorf("creating a temporary file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) //nolint:errcheck // best effort; the rename below usually consumed it.

	if err := tmp.Chmod(mode); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close() //nolint:errcheck
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close() //nolint:errcheck
		return fmt.Errorf("flushing %s to disk: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	// Fsync the directory so the rename itself survives a power loss.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

func cloneConfig(c Config) Config {
	out := c
	out.Environments = append([]Environment(nil), c.Environments...)
	out.Storage = append([]Storage(nil), c.Storage...)
	for i := range out.Environments {
		if p := out.Environments[i].LastProbe; p != nil {
			cp := *p
			out.Environments[i].LastProbe = &cp
		}
		if j := out.Environments[i].JumpHost; j != nil {
			cp := *j
			out.Environments[i].JumpHost = &cp
		}
	}
	for i := range out.Storage {
		if p := out.Storage[i].LastProbe; p != nil {
			cp := *p
			out.Storage[i].LastProbe = &cp
		}
		if j := out.Storage[i].JumpHost; j != nil {
			cp := *j
			out.Storage[i].JumpHost = &cp
		}
	}
	return out
}

// newLocator builds a path→line lookup over the parsed document, so a problem
// with environments[2].host can name the line the operator has to edit.
func newLocator(doc *yaml.Node) func(string) int {
	lines := map[string]int{}
	if len(doc.Content) == 0 {
		return func(string) int { return 0 }
	}
	var walk func(prefix string, n *yaml.Node)
	walk = func(prefix string, n *yaml.Node) {
		switch n.Kind {
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				k, v := n.Content[i], n.Content[i+1]
				p := k.Value
				if prefix != "" {
					p = prefix + "." + k.Value
				}
				lines[p] = k.Line
				walk(p, v)
			}
		case yaml.SequenceNode:
			for i, c := range n.Content {
				p := prefix + "[" + strconv.Itoa(i) + "]"
				lines[p] = c.Line
				walk(p, c)
			}
		case yaml.DocumentNode:
			for _, c := range n.Content {
				walk(prefix, c)
			}
		}
	}
	walk("", doc.Content[0])

	return func(path string) int {
		if l, ok := lines[path]; ok {
			return l
		}
		// Fall back to the nearest enclosing container, so a problem about a
		// missing field still points at the entry it is missing from.
		for {
			cut := strings.LastIndexAny(path, ".[")
			if cut <= 0 {
				return 0
			}
			path = path[:cut]
			if l, ok := lines[path]; ok {
				return l
			}
		}
	}
}

func yamlErrorLine(err error) int {
	var te *yaml.TypeError
	if errors.As(err, &te) && len(te.Errors) > 0 {
		return parseLeadingLine(te.Errors[0])
	}
	return parseLeadingLine(err.Error())
}

func parseLeadingLine(s string) int {
	const marker = "line "
	i := strings.Index(s, marker)
	if i < 0 {
		return 0
	}
	rest := s[i+len(marker):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil {
		return 0
	}
	return n
}

func trimYAMLPrefix(s string) string {
	s = strings.TrimPrefix(s, "yaml: ")
	return strings.TrimSpace(s)
}
