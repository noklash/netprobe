package diagnostic

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func makeResult(name string, status Status, required bool) Result {
	return Result{
		Name:     name,
		Status:   status,
		Required: required,
		Duration: 10 * time.Millisecond,
		Message:  "test result",
	}
}

func TestNewReportAggregation(t *testing.T) {
	tests := []struct {
		name       string
		results    []Result
		wantStatus Status
		wantCode   int
	}{
		{
			name: "all required checks pass",
			results: []Result{
				makeResult("dns", StatusPassed, true),
				makeResult("tcp", StatusPassed, true),
			},
			wantStatus: StatusPassed,
			wantCode:   ExitCodeSuccess,
		},
		{
			name: "required failure determines overall status",
			results: []Result{
				makeResult("dns", StatusPassed, true),
				makeResult("tcp", StatusFailed, true),
			},
			wantStatus: StatusFailed,
			wantCode:   ExitCodeDiagnosticFailure,
		},
		{
			name: "failure takes precedence over inconclusive",
			results: []Result{
				makeResult("ipv4_tcp", StatusInconclusive, true),
				makeResult("ipv6_tcp", StatusFailed, true),
			},
			wantStatus: StatusFailed,
			wantCode:   ExitCodeDiagnosticFailure,
		},
		{
			name: "inconclusive required check prevents success",
			results: []Result{
				makeResult("dns", StatusPassed, true),
				makeResult("tcp", StatusInconclusive, true),
			},
			wantStatus: StatusInconclusive,
			wantCode:   ExitCodeDiagnosticFailure,
		},
		{
			name: "skipped required check prevents success",
			results: []Result{
				makeResult("dns", StatusPassed, true),
				makeResult("tls", StatusSkipped, true),
			},
			wantStatus: StatusInconclusive,
			wantCode:   ExitCodeDiagnosticFailure,
		},
		{
			name: "optional failure does not override required success",
			results: []Result{
				makeResult("ipv4_tcp", StatusPassed, true),
				makeResult("ipv6_tcp", StatusFailed, false),
			},
			wantStatus: StatusPassed,
			wantCode:   ExitCodeSuccess,
		},
		{
			name: "optional checks alone are inconclusive",
			results: []Result{
				makeResult("ipv4_tcp", StatusPassed, false),
				makeResult("ipv6_tcp", StatusFailed, false),
			},
			wantStatus: StatusInconclusive,
			wantCode:   ExitCodeDiagnosticFailure,
		},
		{
			name:       "empty results are inconclusive",
			results:    nil,
			wantStatus: StatusInconclusive,
			wantCode:   ExitCodeDiagnosticFailure,
		},
		{
			name: "all required checks skipped",
			results: []Result{
				makeResult("dns", StatusSkipped, true),
				makeResult("tcp", StatusSkipped, true),
			},
			wantStatus: StatusInconclusive,
			wantCode:   ExitCodeDiagnosticFailure,
		},
	}

	startedAt := time.Date(
		2026, time.October, 11, 7, 0, 0, 0, time.UTC,
	)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := NewReport(
				"example.com",
				startedAt,
				100*time.Millisecond,
				tt.results,
			)
			if err != nil {
				t.Fatalf("NewReport() error: %v", err)
			}

			if report.Status != tt.wantStatus {
				t.Errorf(
					"Status = %q, want %q",
					report.Status,
					tt.wantStatus,
				)
			}

			if got := report.ExitCode(); got != tt.wantCode {
				t.Errorf("ExitCode() = %d, want %d", got, tt.wantCode)
			}

			if err := report.Validate(); err != nil {
				t.Errorf("Validate() error: %v", err)
			}
		})
	}
}

func TestNewReportRejectsInvalidInput(t *testing.T) {
	startedAt := time.Date(
		2026, time.October, 11, 7, 0, 0, 0, time.UTC,
	)

	tests := []struct {
		name      string
		target    string
		startedAt time.Time
		duration  time.Duration
		results   []Result
	}{
		{
			name:      "empty target",
			target:    "",
			startedAt: startedAt,
		},
		{
			name:   "zero start time",
			target: "example.com",
		},
		{
			name:      "negative duration",
			target:    "example.com",
			startedAt: startedAt,
			duration:  -time.Second,
		},
		{
			name:      "invalid result",
			target:    "example.com",
			startedAt: startedAt,
			results: []Result{
				{
					Name:    "dns",
					Status:  Status("unknown"),
					Message: "invalid status",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewReport(
				tt.target,
				tt.startedAt,
				tt.duration,
				tt.results,
			)
			if err == nil {
				t.Fatal("NewReport() expected an error")
			}
		})
	}
}

func TestReportValidateRejectsInconsistentStatus(t *testing.T) {
	report := Report{
		Target:    "example.com",
		StartedAt: time.Date(2026, time.October, 11, 7, 0, 0, 0, time.UTC),
		Duration:  100 * time.Millisecond,
		Status:    StatusPassed,
		Results: []Result{
			makeResult("tcp", StatusFailed, true),
		},
	}

	if err := report.Validate(); err == nil {
		t.Fatal("Validate() expected an error for inconsistent status")
	}
}

func TestReportMarshalJSON(t *testing.T) {
	startedAt := time.Date(
		2026, time.October, 11, 7, 0, 0, 0, time.UTC,
	)

	report, err := NewReport(
		"example.com",
		startedAt,
		250*time.Millisecond,
		[]Result{
			makeResult("dns", StatusPassed, true),
			makeResult("ipv6_tcp", StatusFailed, false),
		},
	)
	if err != nil {
		t.Fatalf("NewReport() error: %v", err)
	}

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}

	if got["target"] != "example.com" {
		t.Errorf("target = %v, want example.com", got["target"])
	}

	if got["status"] != string(StatusPassed) {
		t.Errorf("status = %v, want %q", got["status"], StatusPassed)
	}

	if got["duration_ns"] != float64(250*time.Millisecond) {
		t.Errorf(
			"duration_ns = %v, want %d",
			got["duration_ns"],
			250*time.Millisecond,
		)
	}

	if _, exists := got["duration"]; exists {
		t.Error("JSON must use duration_ns, not duration")
	}

	results, ok := got["results"].([]any)
	if !ok {
		t.Fatalf("results has unexpected type %T", got["results"])
	}

	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}

	first, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("first result has unexpected type %T", results[0])
	}

	if first["required"] != true {
		t.Errorf("first result required = %v, want true", first["required"])
	}

	second, ok := results[1].(map[string]any)
	if !ok {
		t.Fatalf("second result has unexpected type %T", results[1])
	}

	if second["required"] != false {
		t.Errorf("second result required = %v, want false", second["required"])
	}

	if strings.Contains(string(data), `"duration":`) {
		t.Errorf("JSON contains ambiguous duration field: %s", data)
	}
}

func TestReportExitCodeForInvalidStatus(t *testing.T) {
	report := Report{
		Status: Status("unknown"),
	}

	if got := report.ExitCode(); got != ExitCodeInternalError {
		t.Errorf("ExitCode() = %d, want %d", got, ExitCodeInternalError)
	}
}
