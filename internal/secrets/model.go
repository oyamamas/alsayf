// Package secrets implements local-only secret discovery. It never validates
// credentials, fetches rules, or opens network connections.
package secrets

import "errors"

var ErrIncomplete = errors.New("scan incomplete; see issues in the report")

type Occurrence struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	StartByte int64  `json:"start_byte"`
	EndByte   int64  `json:"end_byte"`
}

// Finding contains the captured secret values without surrounding source text.
// Compound credentials are represented in Secret as newline-separated captures.
type Finding struct {
	ID          string       `json:"id"`
	RuleID      string       `json:"rule_id"`
	RuleName    string       `json:"rule_name"`
	Secret      string       `json:"secret"`
	Occurrences []Occurrence `json:"occurrences"`
}

type PathIssue struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type Report struct {
	Complete     bool        `json:"complete"`
	FilesScanned int         `json:"files_scanned"`
	BytesScanned int64       `json:"bytes_scanned"`
	Findings     []Finding   `json:"findings"`
	Skipped      []PathIssue `json:"skipped"`
	Issues       []PathIssue `json:"issues"`
}

type Rule struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
