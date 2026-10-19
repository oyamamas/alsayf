package secrets

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const DefaultMaxFileSize int64 = 1 << 20 // 1 MiB, bounded before reading

type Options struct {
	MaxFileSize int64
	Exclude     []string
}

// Scan scans local regular files only. Symlinks, special files, binary files,
// .git metadata, and oversized files are reported as skipped. Git history is
// deliberately not interpreted by this filesystem source.
func Scan(ctx context.Context, paths []string, opts Options) (Report, error) {
	if err := validateOptions(paths, opts); err != nil {
		return Report{}, err
	}
	e, err := newEngine()
	if err != nil {
		return Report{}, err
	}
	defer e.close()
	return scanWithEngine(ctx, paths, opts, e)
}

func validateOptions(paths []string, opts Options) error {
	if len(paths) == 0 {
		return errors.New("provide at least one local file or directory")
	}
	if opts.MaxFileSize <= 0 || opts.MaxFileSize == math.MaxInt64 {
		return errors.New("max-file-size must be positive and smaller than MaxInt64")
	}
	for _, pattern := range opts.Exclude {
		if _, err := filepath.Match(pattern, ""); err != nil {
			return fmt.Errorf("invalid exclusion pattern %q", pattern)
		}
	}
	return nil
}

type scanState struct {
	ctx             context.Context
	opts            Options
	engine          engine
	report          Report
	key             [32]byte
	seenPaths       map[string]bool
	findings        map[string]int
	seenOccurrences map[Occurrence]map[string]bool
}

func scanWithEngine(ctx context.Context, paths []string, opts Options, e engine) (Report, error) {
	s := scanState{
		ctx: ctx, opts: opts, engine: e,
		report:    Report{Complete: true, Findings: []Finding{}, Skipped: []PathIssue{}, Issues: []PathIssue{}},
		seenPaths: make(map[string]bool), findings: make(map[string]int),
		seenOccurrences: make(map[Occurrence]map[string]bool),
	}
	if _, err := rand.Read(s.key[:]); err != nil {
		return s.report, errors.New("cannot initialize finding identifiers")
	}
	for _, path := range paths {
		if ctx.Err() != nil {
			break
		}
		if strings.Contains(path, "://") {
			s.issue(path, "only local filesystem paths are supported")
			continue
		}
		root, err := filepath.Abs(path)
		if err != nil {
			s.issue(path, "cannot resolve local path")
			continue
		}
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			// WalkDir may call back a second time for the same directory if
			// ReadDir fails. Handle that error before duplicate-path filtering.
			if walkErr != nil {
				s.issue(path, "cannot access path")
				return nil
			}
			if s.seenPaths[path] {
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			s.seenPaths[path] = true
			if s.excluded(path, root) {
				s.skip(path, "excluded")
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				s.skip(path, "symbolic link")
				return nil
			}
			if d.IsDir() {
				return nil
			}
			s.scanFile(path)
			return nil
		})
		if err != nil && ctx.Err() == nil {
			s.issue(root, "directory traversal failed")
		}
	}
	if ctx.Err() != nil {
		s.issue("", "scan canceled")
	}
	sort.Slice(s.report.Findings, func(i, j int) bool {
		a, b := s.report.Findings[i], s.report.Findings[j]
		if a.RuleID != b.RuleID {
			return a.RuleID < b.RuleID
		}
		x, y := a.Occurrences[0], b.Occurrences[0]
		if x.Path != y.Path {
			return x.Path < y.Path
		}
		return x.StartByte < y.StartByte
	})
	if !s.report.Complete {
		return s.report, ErrIncomplete
	}
	return s.report, nil
}

func (s *scanState) excluded(path, root string) bool {
	// Also reject an explicitly supplied path below .git.
	for _, component := range strings.Split(filepath.ToSlash(path), "/") {
		if component == ".git" {
			return true
		}
	}
	rel, _ := filepath.Rel(root, path)
	for _, pattern := range s.opts.Exclude {
		for _, candidate := range []string{filepath.Base(path), rel, path} {
			if matched, _ := filepath.Match(pattern, candidate); matched {
				return true
			}
		}
	}
	return false
}

func (s *scanState) scanFile(path string) {
	info, err := os.Lstat(path)
	if err != nil {
		s.issue(path, "cannot inspect file")
		return
	}
	if !info.Mode().IsRegular() {
		s.skip(path, "not a regular file")
		return
	}
	if info.Size() > s.opts.MaxFileSize {
		s.skip(path, "file exceeds max-file-size")
		return
	}
	f, err := os.Open(path)
	if err != nil {
		s.issue(path, "cannot open file")
		return
	}
	defer f.Close()
	// Verify that opening the path did not follow a replaced symlink/file.
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		s.issue(path, "file changed while opening")
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, s.opts.MaxFileSize+1))
	if err != nil {
		s.issue(path, "cannot read file")
		return
	}
	if int64(len(data)) > s.opts.MaxFileSize {
		s.skip(path, "file exceeds max-file-size")
		return
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		s.skip(path, "binary or non-UTF-8 file")
		return
	}
	if s.ctx.Err() != nil {
		return
	}
	matches, err := s.engine.scan(data)
	if err != nil {
		// Do not serialize raw engine errors: regexp engines can echo input.
		s.issue(path, "detection failed or timed out; results may be incomplete")
	}
	s.report.FilesScanned++
	s.report.BytesScanned += int64(len(data))
	lineStarts := []int{0}
	for i, b := range data {
		if b == '\n' {
			lineStarts = append(lineStarts, i+1)
		}
	}
	for _, m := range matches {
		if m.start < 0 || m.end < m.start || m.end > int64(len(data)) {
			s.issue(path, "detection returned an invalid location")
			continue
		}
		id := s.findingID(m)
		line := sort.Search(len(lineStarts), func(i int) bool { return int64(lineStarts[i]) > m.start })
		occ := Occurrence{path, line, utf8.RuneCount(data[lineStarts[line-1]:m.start]) + 1, m.start, m.end}
		if s.seenOccurrences[occ] == nil {
			s.seenOccurrences[occ] = make(map[string]bool)
		}
		if s.seenOccurrences[occ][id] {
			continue
		}
		s.seenOccurrences[occ][id] = true
		idx, exists := s.findings[id]
		if !exists {
			idx = len(s.report.Findings)
			s.findings[id] = idx
			s.report.Findings = append(s.report.Findings, Finding{
				ID: id, RuleID: m.ruleID, RuleName: m.ruleName,
				Secret: string(bytes.Join(m.groups, []byte("\n"))), Occurrences: []Occurrence{},
			})
		}
		s.report.Findings[idx].Occurrences = append(s.report.Findings[idx].Occurrences, occ)
	}
}

func (s *scanState) findingID(m detection) string {
	// Identifiers are scoped to one scan and group identical capture values.
	h := hmac.New(sha256.New, s.key[:])
	for _, value := range append([][]byte{[]byte(m.ruleID)}, m.groups...) {
		_ = binary.Write(h, binary.BigEndian, uint64(len(value)))
		_, _ = h.Write(value)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (s *scanState) issue(path, reason string) {
	s.report.Complete = false
	s.report.Issues = append(s.report.Issues, PathIssue{path, reason})
}

func (s *scanState) skip(path, reason string) {
	s.report.Skipped = append(s.report.Skipped, PathIssue{path, reason})
}
