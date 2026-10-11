package diagnostic

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Status represents the outcome of an individual diagnostic check.
type Status string

const (
	StatusPassed       Status = "passed"
	StatusFailed       Status = "failed"
	StatusSkipped      Status = "skipped"
	StatusInconclusive Status = "inconclusive"
)

// Validate reports whether s is a supported diagnostic status.
func (s Status) Validate() error {
	switch s {
	case StatusPassed, StatusFailed, StatusSkipped, StatusInconclusive:
		return nil
	default:
		return fmt.Errorf("invalid diagnostic status %q", s)
	}
}

// Result records the outcome and evidence produced by one diagnostic check.
//
// Required indicates whether this check must pass for the overall
// diagnostic to be considered successful.
//
// Duration is represented internally as time.Duration and serialized
// as an integer number of nanoseconds.
type Result struct {
	Name     string         `json:"name"`
	Status   Status         `json:"status"`
	Required bool           `json:"required"`
	Duration time.Duration  `json:"-"`
	Message  string         `json:"message"`
	Error    string         `json:"error,omitempty"`
	Details  map[string]any `json:"details,omitempty"`
}

// Validate checks the basic invariants of a diagnostic result.
func (r Result) Validate() error {
	if r.Name == "" {
		return errors.New("result name must not be empty")
	}

	if err := r.Status.Validate(); err != nil {
		return err
	}

	if r.Duration < 0 {
		return errors.New("result duration must not be negative")
	}

	if r.Message == "" {
		return errors.New("result message must not be empty")
	}

	return nil
}

// MarshalJSON makes the duration unit explicit in serialized reports.
func (r Result) MarshalJSON() ([]byte, error) {
	type resultJSON struct {
		Name       string         `json:"name"`
		Status     Status         `json:"status"`
		Required   bool           `json:"required"`
		DurationNS int64          `json:"duration_ns"`
		Message    string         `json:"message"`
		Error      string         `json:"error,omitempty"`
		Details    map[string]any `json:"details,omitempty"`
	}

	return json.Marshal(resultJSON{
		Name:       r.Name,
		Status:     r.Status,
		Required:   r.Required,
		DurationNS: int64(r.Duration),
		Message:    r.Message,
		Error:      r.Error,
		Details:    r.Details,
	})
}
