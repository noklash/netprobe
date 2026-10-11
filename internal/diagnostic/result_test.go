package diagnostic

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestStatusValidate(t *testing.T) {
	tests := []struct {
		name    string
		status  Status
		wantErr bool
	}{
		{
			name:   "passed",
			status: StatusPassed,
		},
		{
			name:   "failed",
			status: StatusFailed,
		},
		{
			name:   "skipped",
			status: StatusSkipped,
		},
		{
			name:   "inconclusive",
			status: StatusInconclusive,
		},
		{
			name:    "unknown status",
			status:  Status("unknown"),
			wantErr: true,
		},
		{
			name:    "empty status",
			status:  Status(""),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.status.Validate()

			if tt.wantErr && err == nil {
				t.Fatal("Validate() expected an error")
			}

			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestResultValidate(t *testing.T) {
	valid := Result{
		Name:     "tcp_ipv4",
		Status:   StatusPassed,
		Duration: 150 * time.Millisecond,
		Message:  "TCP connection established",
	}

	tests := []struct {
		name    string
		result  Result
		wantErr bool
	}{
		{
			name:   "valid result",
			result: valid,
		},
		{
			name: "empty name",
			result: Result{
				Status:  StatusPassed,
				Message: "passed",
			},
			wantErr: true,
		},
		{
			name: "invalid status",
			result: Result{
				Name:    "tcp_ipv4",
				Status:  Status("unknown"),
				Message: "check completed",
			},
			wantErr: true,
		},
		{
			name: "negative duration",
			result: Result{
				Name:     "tcp_ipv4",
				Status:   StatusPassed,
				Duration: -time.Second,
				Message:  "passed",
			},
			wantErr: true,
		},
		{
			name: "empty message",
			result: Result{
				Name:   "tcp_ipv4",
				Status: StatusPassed,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.result.Validate()

			if tt.wantErr && err == nil {
				t.Fatal("Validate() expected an error")
			}

			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestResultMarshalJSON(t *testing.T) {
	result := Result{
		Name:     "tcp_ipv4",
		Status:   StatusPassed,
		Duration: 150 * time.Millisecond,
		Message:  "TCP connection established",
		Details: map[string]any{
			"address": "192.0.2.10:443",
		},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}

	if got["name"] != "tcp_ipv4" {
		t.Errorf("name = %v, want tcp_ipv4", got["name"])
	}

	if got["status"] != string(StatusPassed) {
		t.Errorf("status = %v, want %q", got["status"], StatusPassed)
	}

	if got["duration_ns"] != float64(150*time.Millisecond) {
		t.Errorf("duration_ns = %v, want %d", got["duration_ns"], 150*time.Millisecond)
	}

	if got["message"] != "TCP connection established" {
		t.Errorf("message = %v, want TCP connection established", got["message"])
	}

	if _, exists := got["duration"]; exists {
		t.Error("JSON must use duration_ns, not duration")
	}

	details, ok := got["details"].(map[string]any)
	if !ok {
		t.Fatalf("details has unexpected type %T", got["details"])
	}

	if details["address"] != "192.0.2.10:443" {
		t.Errorf("details.address = %v, want 192.0.2.10:443", details["address"])
	}
}

func TestResultMarshalJSONOmitsEmptyOptionalFields(t *testing.T) {
	result := Result{
		Name:    "dns",
		Status:  StatusPassed,
		Message: "DNS resolution completed",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}

	output := string(data)

	for _, field := range []string{`"error"`, `"details"`} {
		if strings.Contains(output, field) {
			t.Errorf("JSON unexpectedly contains %s: %s", field, output)
		}
	}
}
