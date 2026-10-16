package checks

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/noklash/netprobe/internal/diagnostic"
)

// roundTripperFunc adapts a function into an http.RoundTripper.
// It allows tests to control HTTP transport behavior without relying on
// external network services.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	return f(request)
}

// trackingBody records whether the HTTP response body was closed.
type trackingBody struct {
	reader io.Reader
	closed atomic.Bool
}

func (b *trackingBody) Read(p []byte) (int, error) {
	return b.reader.Read(p)
}

func (b *trackingBody) Close() error {
	b.closed.Store(true)
	return nil
}

func TestCheckHTTPSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf(
					"request method = %q, want %q",
					r.Method,
					http.MethodGet,
				)
			}

			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)

			_, _ = io.WriteString(w, "service is healthy")
		},
	))
	defer server.Close()

	result, err := CheckHTTP(
		context.Background(),
		server.Client(),
		server.URL,
		http.StatusOK,
		time.Second,
	)
	if err != nil {
		t.Fatalf("CheckHTTP() returned an unexpected error: %v", err)
	}

	if result.Status != diagnostic.StatusPassed {
		t.Fatalf(
			"status = %q, want passed; error: %s",
			result.Status,
			result.Error,
		)
	}

	if result.Name != "http" {
		t.Errorf("name = %q, want http", result.Name)
	}

	if result.Message == "" {
		t.Error("expected a diagnostic message")
	}

	if result.Duration < 0 {
		t.Errorf("duration must not be negative: %v", result.Duration)
	}

	if result.Details["method"] != http.MethodGet {
		t.Errorf(
			"method detail = %v, want GET",
			result.Details["method"],
		)
	}

	if result.Details["expected_status_code"] != http.StatusOK {
		t.Errorf(
			"expected status detail = %v, want %d",
			result.Details["expected_status_code"],
			http.StatusOK,
		)
	}

	if result.Details["status_code"] != http.StatusOK {
		t.Errorf(
			"status code detail = %v, want %d",
			result.Details["status_code"],
			http.StatusOK,
		)
	}

	if result.Details["status"] != "200 OK" {
		t.Errorf(
			"status detail = %v, want %q",
			result.Details["status"],
			"200 OK",
		)
	}

	if result.Details["content_type"] != "text/plain" {
		t.Errorf(
			"content type = %v, want text/plain",
			result.Details["content_type"],
		)
	}

	if result.Details["is_redirect"] != false {
		t.Errorf("expected is_redirect to be false")
	}

	if err := result.Validate(); err != nil {
		t.Fatalf("result validation failed: %v", err)
	}
}

func TestCheckHTTPUnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			http.Error(
				w,
				"service unavailable",
				http.StatusServiceUnavailable,
			)
		},
	))
	defer server.Close()

	result, err := CheckHTTP(
		context.Background(),
		server.Client(),
		server.URL,
		http.StatusOK,
		time.Second,
	)
	if err != nil {
		t.Fatalf("CheckHTTP() returned an unexpected error: %v", err)
	}

	if result.Status != diagnostic.StatusFailed {
		t.Fatalf(
			"status = %q, want failed",
			result.Status,
		)
	}

	if result.Details["status_code"] != http.StatusServiceUnavailable {
		t.Errorf(
			"status code = %v, want %d",
			result.Details["status_code"],
			http.StatusServiceUnavailable,
		)
	}

	if !strings.Contains(result.Message, "503") {
		t.Errorf(
			"message should include the received status code: %q",
			result.Message,
		)
	}

	if !strings.Contains(result.Message, "200") {
		t.Errorf(
			"message should include the expected status code: %q",
			result.Message,
		)
	}

	if err := result.Validate(); err != nil {
		t.Fatalf("result validation failed: %v", err)
	}
}

func TestCheckHTTPDoesNotFollowRedirects(t *testing.T) {
	var destinationRequests atomic.Int32

	mux := http.NewServeMux()

	mux.HandleFunc("/start", func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		http.Redirect(
			w,
			r,
			"/destination",
			http.StatusFound,
		)
	})

	mux.HandleFunc("/destination", func(
		w http.ResponseWriter,
		_ *http.Request,
	) {
		destinationRequests.Add(1)
		w.WriteHeader(http.StatusOK)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	result, err := CheckHTTP(
		context.Background(),
		server.Client(),
		server.URL+"/start",
		http.StatusOK,
		time.Second,
	)
	if err != nil {
		t.Fatalf("CheckHTTP() returned an unexpected error: %v", err)
	}

	if result.Status != diagnostic.StatusFailed {
		t.Fatalf(
			"status = %q, want failed because the received status was a redirect",
			result.Status,
		)
	}

	if result.Details["status_code"] != http.StatusFound {
		t.Errorf(
			"status code = %v, want %d",
			result.Details["status_code"],
			http.StatusFound,
		)
	}

	if result.Details["is_redirect"] != true {
		t.Error("expected is_redirect to be true")
	}

	if result.Details["location_present"] != true {
		t.Error("expected location_present to be true")
	}

	if got := destinationRequests.Load(); got != 0 {
		t.Errorf(
			"redirect destination was requested %d times, want 0",
			got,
		)
	}

	if err := result.Validate(); err != nil {
		t.Fatalf("result validation failed: %v", err)
	}
}

func TestCheckHTTPSuccessWithHTTPS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "secure service")
		},
	))
	defer server.Close()

	// httptest's client trusts the test server's certificate.
	// This exercises HTTPS verification without disabling verification.
	result, err := CheckHTTP(
		context.Background(),
		server.Client(),
		server.URL,
		http.StatusOK,
		time.Second,
	)
	if err != nil {
		t.Fatalf("CheckHTTP() returned an unexpected error: %v", err)
	}

	if result.Status != diagnostic.StatusPassed {
		t.Fatalf(
			"status = %q, want passed; error: %s",
			result.Status,
			result.Error,
		)
	}

	if result.Details["status_code"] != http.StatusOK {
		t.Errorf(
			"status code = %v, want %d",
			result.Details["status_code"],
			http.StatusOK,
		)
	}

	if err := result.Validate(); err != nil {
		t.Fatalf("result validation failed: %v", err)
	}
}

func TestCheckHTTPRejectsUntrustedHTTPSCertificate(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	))
	defer server.Close()

	// A default client does not trust the temporary certificate generated
	// by httptest.NewTLSServer.
	result, err := CheckHTTP(
		context.Background(),
		&http.Client{},
		server.URL,
		http.StatusOK,
		time.Second,
	)
	if err != nil {
		t.Fatalf("CheckHTTP() returned an unexpected error: %v", err)
	}

	if result.Status != diagnostic.StatusFailed {
		t.Fatalf(
			"status = %q, want failed for an untrusted certificate",
			result.Status,
		)
	}

	if result.Error == "" {
		t.Error("expected a TLS certificate verification error")
	}

	if err := result.Validate(); err != nil {
		t.Fatalf("result validation failed: %v", err)
	}
}

func TestCheckHTTPConnectionFailure(t *testing.T) {
	client := &http.Client{
		Transport: roundTripperFunc(
			func(_ *http.Request) (*http.Response, error) {
				return nil, errors.New("simulated connection failure")
			},
		),
	}

	result, err := CheckHTTP(
		context.Background(),
		client,
		"http://example.test/health",
		http.StatusOK,
		time.Second,
	)
	if err != nil {
		t.Fatalf("CheckHTTP() returned an unexpected error: %v", err)
	}

	if result.Status != diagnostic.StatusFailed {
		t.Errorf(
			"status = %q, want failed",
			result.Status,
		)
	}

	if result.Error == "" {
		t.Error("expected a request error")
	}

	if err := result.Validate(); err != nil {
		t.Fatalf("result validation failed: %v", err)
	}
}

func TestCheckHTTPTimeoutIsInconclusive(t *testing.T) {
	client := &http.Client{
		Transport: roundTripperFunc(
			func(request *http.Request) (*http.Response, error) {
				<-request.Context().Done()
				return nil, request.Context().Err()
			},
		),
	}

	result, err := CheckHTTP(
		context.Background(),
		client,
		"http://example.test/health",
		http.StatusOK,
		30*time.Millisecond,
	)
	if err != nil {
		t.Fatalf("CheckHTTP() returned an unexpected error: %v", err)
	}

	if result.Status != diagnostic.StatusInconclusive {
		t.Fatalf(
			"status = %q, want inconclusive",
			result.Status,
		)
	}

	if result.Error == "" {
		t.Error("expected a timeout or cancellation error")
	}

	if err := result.Validate(); err != nil {
		t.Fatalf("result validation failed: %v", err)
	}
}

func TestCheckHTTPParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := &http.Client{
		Transport: roundTripperFunc(
			func(request *http.Request) (*http.Response, error) {
				return nil, request.Context().Err()
			},
		),
	}

	result, err := CheckHTTP(
		ctx,
		client,
		"http://example.test/health",
		http.StatusOK,
		time.Second,
	)
	if err != nil {
		t.Fatalf("CheckHTTP() returned an unexpected error: %v", err)
	}

	if result.Status != diagnostic.StatusInconclusive {
		t.Errorf(
			"status = %q, want inconclusive",
			result.Status,
		)
	}

	if err := result.Validate(); err != nil {
		t.Fatalf("result validation failed: %v", err)
	}
}

func TestCheckHTTPQueryParametersAreNotRecorded(t *testing.T) {
	const secret = "sensitive-test-token"

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			// Query parameters should still be sent to the server.
			if got := r.URL.Query().Get("token"); got != secret {
				t.Errorf(
					"server received token %q, want %q",
					got,
					secret,
				)
			}

			w.WriteHeader(http.StatusOK)
		},
	))
	defer server.Close()

	targetURL := server.URL +
		"/health?token=" + secret +
		"#private-fragment"

	result, err := CheckHTTP(
		context.Background(),
		server.Client(),
		targetURL,
		http.StatusOK,
		time.Second,
	)
	if err != nil {
		t.Fatalf("CheckHTTP() returned an unexpected error: %v", err)
	}

	if result.Status != diagnostic.StatusPassed {
		t.Fatalf(
			"status = %q, want passed; error: %s",
			result.Status,
			result.Error,
		)
	}

	recordedURL, ok := result.Details["url"].(string)
	if !ok {
		t.Fatalf(
			"URL detail has unexpected type: %T",
			result.Details["url"],
		)
	}

	if strings.Contains(recordedURL, secret) {
		t.Error("diagnostic URL contains the sensitive query value")
	}

	if strings.Contains(recordedURL, "token=") {
		t.Error("diagnostic URL contains the query parameter")
	}

	if strings.Contains(recordedURL, "#private-fragment") {
		t.Error("diagnostic URL contains the URL fragment")
	}

	if result.Details["query_parameters_used"] != true {
		t.Error("expected query_parameters_used to be true")
	}

	if err := result.Validate(); err != nil {
		t.Fatalf("result validation failed: %v", err)
	}
}

func TestCheckHTTPDoesNotMutateClientRedirectPolicy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/start" {
				http.Redirect(
					w,
					r,
					"/destination",
					http.StatusFound,
				)
				return
			}

			w.WriteHeader(http.StatusOK)
		},
	))
	defer server.Close()

	client := server.Client()

	originalRedirectPolicy := func(
		_ *http.Request,
		_ []*http.Request,
	) error {
		return nil
	}

	client.CheckRedirect = originalRedirectPolicy

	_, err := CheckHTTP(
		context.Background(),
		client,
		server.URL+"/start",
		http.StatusOK,
		time.Second,
	)
	if err != nil {
		t.Fatalf("CheckHTTP() returned an unexpected error: %v", err)
	}

	if client.CheckRedirect == nil {
		t.Fatal("CheckRedirect was unexpectedly set to nil")
	}

	// Compare function identity through behavior rather than function values.
	if err := client.CheckRedirect(
		&http.Request{},
		nil,
	); err != nil {
		t.Errorf(
			"original redirect policy was changed: %v",
			err,
		)
	}
}

func TestCheckHTTPClosesResponseBody(t *testing.T) {
	body := &trackingBody{
		reader: strings.NewReader("response body"),
	}

	client := &http.Client{
		Transport: roundTripperFunc(
			func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode:    http.StatusOK,
					Status:        "200 OK",
					Proto:         "HTTP/1.1",
					ProtoMajor:    1,
					ProtoMinor:    1,
					Header:        make(http.Header),
					Body:          body,
					ContentLength: -1,
					Request:       request,
				}, nil
			},
		),
	}

	result, err := CheckHTTP(
		context.Background(),
		client,
		"http://example.test/health",
		http.StatusOK,
		time.Second,
	)
	if err != nil {
		t.Fatalf("CheckHTTP() returned an unexpected error: %v", err)
	}

	if result.Status != diagnostic.StatusPassed {
		t.Fatalf(
			"status = %q, want passed",
			result.Status,
		)
	}

	if !body.closed.Load() {
		t.Error("expected response body to be closed")
	}
}

func TestCheckHTTPResponseBodyIsNotRecorded(t *testing.T) {
	const privateBody = "private response content"

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, privateBody)
		},
	))
	defer server.Close()

	result, err := CheckHTTP(
		context.Background(),
		server.Client(),
		server.URL,
		http.StatusOK,
		time.Second,
	)
	if err != nil {
		t.Fatalf("CheckHTTP() returned an unexpected error: %v", err)
	}

	if result.Status != diagnostic.StatusPassed {
		t.Fatalf(
			"status = %q, want passed",
			result.Status,
		)
	}

	if strings.Contains(result.Message, privateBody) {
		t.Error("diagnostic message contains the response body")
	}

	if strings.Contains(result.Error, privateBody) {
		t.Error("diagnostic error contains the response body")
	}

	for key, value := range result.Details {
		if strings.Contains(
			strings.ToLower(key),
			"body",
		) {
			t.Errorf(
				"unexpected response-body detail %q: %v",
				key,
				value,
			)
		}

		if textValue, ok := value.(string); ok &&
			strings.Contains(textValue, privateBody) {
			t.Errorf(
				"detail %q contains the response body",
				key,
			)
		}
	}
}

func TestCheckHTTPInvalidConfiguration(t *testing.T) {
	validClient := &http.Client{}

	tests := []struct {
		name           string
		ctx            context.Context
		client         *http.Client
		targetURL      string
		expectedStatus int
		timeout        time.Duration
	}{
		{
			name:           "nil context",
			ctx:            nil,
			client:         validClient,
			targetURL:      "http://example.test/health",
			expectedStatus: http.StatusOK,
			timeout:        time.Second,
		},
		{
			name:           "nil client",
			ctx:            context.Background(),
			client:         nil,
			targetURL:      "http://example.test/health",
			expectedStatus: http.StatusOK,
			timeout:        time.Second,
		},
		{
			name:           "malformed URL",
			ctx:            context.Background(),
			client:         validClient,
			targetURL:      "://invalid",
			expectedStatus: http.StatusOK,
			timeout:        time.Second,
		},
		{
			name:           "unsupported scheme",
			ctx:            context.Background(),
			client:         validClient,
			targetURL:      "ftp://example.test/health",
			expectedStatus: http.StatusOK,
			timeout:        time.Second,
		},
		{
			name:           "missing hostname",
			ctx:            context.Background(),
			client:         validClient,
			targetURL:      "http:///health",
			expectedStatus: http.StatusOK,
			timeout:        time.Second,
		},
		{
			name:           "URL contains user information",
			ctx:            context.Background(),
			client:         validClient,
			targetURL:      "http://user:password@example.test/health",
			expectedStatus: http.StatusOK,
			timeout:        time.Second,
		},
		{
			name:           "status below valid range",
			ctx:            context.Background(),
			client:         validClient,
			targetURL:      "http://example.test/health",
			expectedStatus: 99,
			timeout:        time.Second,
		},
		{
			name:           "status above valid range",
			ctx:            context.Background(),
			client:         validClient,
			targetURL:      "http://example.test/health",
			expectedStatus: 600,
			timeout:        time.Second,
		},
		{
			name:           "zero timeout",
			ctx:            context.Background(),
			client:         validClient,
			targetURL:      "http://example.test/health",
			expectedStatus: http.StatusOK,
			timeout:        0,
		},
		{
			name:           "negative timeout",
			ctx:            context.Background(),
			client:         validClient,
			targetURL:      "http://example.test/health",
			expectedStatus: http.StatusOK,
			timeout:        -time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CheckHTTP(
				tt.ctx,
				tt.client,
				tt.targetURL,
				tt.expectedStatus,
				tt.timeout,
			)

			if err == nil {
				t.Fatal("expected a configuration validation error")
			}

			if result.Name != "" {
				t.Errorf(
					"expected an empty result for invalid configuration, got %q",
					result.Name,
				)
			}
		})
	}
}
