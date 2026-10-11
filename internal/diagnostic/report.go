package diagnostic

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	// ExitCodeSuccess means all required, applicable checks passed.
	ExitCodeSuccess = 0

	// ExitCodeDiagnosticFailure means the diagnostic did not establish
	// that all required checks passed.
	ExitCodeDiagnosticFailure = 1

	// ExitCodeInvalidArguments is reserved for CLI argument or
	// configuration errors.
	ExitCodeInvalidArguments = 2

	// ExitCodeInternalError indicates that an internal error prevented
	// the program from producing a valid diagnostic report.
	ExitCodeInternalError = 3
)

// Report contains the observations collected during one diagnostic run.
type Report struct {
	Target    string        `json:"target"`
	StartedAt time.Time     `json:"started_at"`
	Duration  time.Duration `json:"-"`
	Status    Status        `json:"status"`
	Results   []Result      `json:"results"`
}

// NewReport validates the supplied results and derives the overall status.
func NewReport(
	target string,
	startedAt time.Time,
	duration time.Duration,
	results []Result,
) (Report, error) {
	if target == "" {
		return Report{}, errors.New("report target must not be empty")
	}

	if startedAt.IsZero() {
		return Report{}, errors.New("report start time must not be zero")
	}

	if duration < 0 {
		return Report{}, errors.New("report duration must not be negative")
	}

	for i, result := range results {
		if err := result.Validate(); err != nil {
			return Report{}, fmt.Errorf("result %d: %w", i, err)
		}
	}

	copiedResults := append([]Result(nil), results...)

	report := Report{
		Target:    target,
		StartedAt: startedAt,
		Duration:  duration,
		Results:   copiedResults,
	}

	report.Status = aggregateStatus(copiedResults)

	return report, nil
}

// aggregateStatus derives the overall status from required checks.
//
// Failed required checks take precedence over inconclusive or skipped
// required checks. Optional checks remain visible in Results but do not
// determine the overall status.
//
// An empty result set or a result set without required checks is
// inconclusive because neither establishes that the required diagnostic
// succeeded.
func aggregateStatus(results []Result) Status {
	requiredCount := 0
	hasInconclusive := false

	for _, result := range results {
		if !result.Required {
			continue
		}

		requiredCount++

		switch result.Status {
		case StatusFailed:
			return StatusFailed
		case StatusInconclusive, StatusSkipped:
			hasInconclusive = true
		}
	}

	if requiredCount == 0 {
		return StatusInconclusive
	}

	if hasInconclusive {
		return StatusInconclusive
	}

	return StatusPassed
}

// Validate checks the report's fields, its results, and its derived status.
func (r Report) Validate() error {
	if r.Target == "" {
		return errors.New("report target must not be empty")
	}

	if r.StartedAt.IsZero() {
		return errors.New("report start time must not be zero")
	}

	if r.Duration < 0 {
		return errors.New("report duration must not be negative")
	}

	for i, result := range r.Results {
		if err := result.Validate(); err != nil {
			return fmt.Errorf("result %d: %w", i, err)
		}
	}

	if err := r.Status.Validate(); err != nil {
		return fmt.Errorf("report status: %w", err)
	}

	expected := aggregateStatus(r.Results)
	if r.Status != expected {
		return fmt.Errorf(
			"report status %q does not match derived status %q",
			r.Status,
			expected,
		)
	}

	return nil
}

// ExitCode maps the report's overall status to a process exit code.
//
// CLI argument errors and fatal internal errors are handled separately
// by the CLI layer.
func (r Report) ExitCode() int {
	switch r.Status {
	case StatusPassed:
		return ExitCodeSuccess
	case StatusFailed, StatusInconclusive, StatusSkipped:
		return ExitCodeDiagnosticFailure
	default:
		return ExitCodeInternalError
	}
}

// MarshalJSON serializes report duration with an explicit unit.
func (r Report) MarshalJSON() ([]byte, error) {
	type reportJSON struct {
		Target     string    `json:"target"`
		StartedAt  time.Time `json:"started_at"`
		DurationNS int64     `json:"duration_ns"`
		Status     Status    `json:"status"`
		Results    []Result  `json:"results"`
	}

	return json.Marshal(reportJSON{
		Target:     r.Target,
		StartedAt:  r.StartedAt,
		DurationNS: int64(r.Duration),
		Status:     r.Status,
		Results:    r.Results,
	})
}
