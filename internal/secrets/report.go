package secrets

import (
	"encoding/json"
	"fmt"
	"io"
)

func WriteReport(w io.Writer, report Report, format string) error {
	if format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	if format != "text" {
		return fmt.Errorf("unsupported format %q (use text or json)", format)
	}
	// Quote paths, names, and values so input cannot emit terminal escapes.
	if _, err := fmt.Fprintf(w, "Local secrets scan: %d files, %d bytes, %d findings; complete=%t\n", report.FilesScanned, report.BytesScanned, len(report.Findings), report.Complete); err != nil {
		return err
	}
	for _, f := range report.Findings {
		if _, err := fmt.Fprintf(w, "\n%q (%q)\n  Secret: %q\n", f.RuleName, f.RuleID, f.Secret); err != nil {
			return err
		}
		for _, o := range f.Occurrences {
			if _, err := fmt.Fprintf(w, "  %q:%d:%d\n", o.Path, o.Line, o.Column); err != nil {
				return err
			}
		}
	}
	for _, p := range report.Skipped {
		if _, err := fmt.Fprintf(w, "Skipped %q: %s\n", p.Path, p.Reason); err != nil {
			return err
		}
	}
	for _, p := range report.Issues {
		if _, err := fmt.Fprintf(w, "Issue %q: %s\n", p.Path, p.Reason); err != nil {
			return err
		}
	}
	return nil
}
