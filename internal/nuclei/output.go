package nuclei

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/logrusorgru/aurora/v4"
	"github.com/projectdiscovery/gologger/levels"
	"github.com/projectdiscovery/nuclei/v3/pkg/output"
)

type resultWriter struct {
	mu        sync.Mutex
	out       io.Writer
	jsonl     bool
	colorizer *aurora.Aurora
	count     int
	err       error
}

func (w *resultWriter) Write(event *output.ResultEvent) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return w.err
	}
	if w.jsonl {
		w.err = json.NewEncoder(w.out).Encode(event)
	} else {
		matched := event.Matched
		if matched == "" {
			matched = event.Host
		}
		_, w.err = fmt.Fprintf(w.out, "[%s] [%s] [%s] %q", event.TemplateID, event.Type, event.Info.SeverityHolder.Severity.String(), matched)
		for _, value := range event.ExtractedResults {
			if w.err == nil {
				_, w.err = fmt.Fprintf(w.out, " %q", value)
			}
		}
		if w.err == nil {
			_, w.err = fmt.Fprintln(w.out)
		}
	}
	if w.err == nil {
		w.count++
	}
	return w.err
}

func (w *resultWriter) resultError() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

func (w *resultWriter) ResultCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.count
}

func (w *resultWriter) Colorizer() *aurora.Aurora                        { return w.colorizer }
func (*resultWriter) Close()                                             {}
func (*resultWriter) WriteFailure(*output.InternalWrappedEvent) error    { return nil }
func (*resultWriter) Request(string, string, string, error)              {}
func (*resultWriter) RequestStatsLog(string, string)                     {}
func (*resultWriter) WriteStoreDebugData(string, string, string, string) {}

type scanProgress struct{ errors atomic.Int64 }

func (*scanProgress) Stop()                               {}
func (*scanProgress) Init(int64, int, int64)              {}
func (*scanProgress) AddToTotal(int64)                    {}
func (*scanProgress) IncrementRequests()                  {}
func (*scanProgress) SetRequests(uint64)                  {}
func (*scanProgress) IncrementMatched()                   {}
func (p *scanProgress) IncrementErrorsBy(n int64)         { p.errors.Add(n) }
func (p *scanProgress) IncrementFailedRequestsBy(n int64) { p.errors.Add(n) }

type logWriter struct {
	mu     sync.Mutex
	out    io.Writer
	errors int
}

func (w *logWriter) Write(data []byte, level levels.Level) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if level == levels.LevelError || level == levels.LevelFatal {
		w.errors++
	}
	if _, err := fmt.Fprintln(w.out, string(data)); err != nil {
		w.errors++
	}
}

func (w *logWriter) errorCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.errors
}
