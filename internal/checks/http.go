package checks

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/noklash/netprobe/internal/diagnostic"
)

// CheckHTTP performs an HTTP GET request against an absolute HTTP or HTTPS
// URL and compares the received status code with the expected status code.
//
// The supplied client controls transport configuration, including HTTPS
// certificate verification. The client is cloned so this check can enforce
// its own redirect policy without mutating the caller's client.
//
// Redirects are not followed. Their response status codes are evaluated
// directly, allowing the caller to distinguish a redirect from the expected
// application response.
//
// The timeout bounds the request without extending an earlier deadline
// imposed by the parent context.
//
// Response bodies are not read or retained. Response metadata is recorded
// without including URL query parameters, URL fragments, or user information.
//
// Invalid input and invalid dependencies are returned as Go errors.
// Request outcomes are represented as diagnostic results.
func CheckHTTP(
	ctx context.Context,
	client *http.Client,
	targetURL string,
	expectedStatus int,
	timeout time.Duration,
) (diagnostic.Result, error) {
	if ctx == nil {
		return diagnostic.Result{}, errors.New("context must not be nil")
	}

	if client == nil {
		return diagnostic.Result{}, errors.New("HTTP client must not be nil")
	}

	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return diagnostic.Result{}, fmt.Errorf("invalid target URL: %w", err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return diagnostic.Result{}, errors.New(
			"target URL scheme must be http or https",
		)
	}

	if parsedURL.Host == "" || parsedURL.Hostname() == "" {
		return diagnostic.Result{}, errors.New(
			"target URL must include a hostname",
		)
	}

	if parsedURL.User != nil {
		return diagnostic.Result{}, errors.New(
			"target URL must not contain user information",
		)
	}

	if expectedStatus < 100 || expectedStatus > 599 {
		return diagnostic.Result{}, fmt.Errorf(
			"expected HTTP status must be between 100 and 599: %d",
			expectedStatus,
		)
	}

	if timeout <= 0 {
		return diagnostic.Result{}, errors.New(
			"timeout must be greater than zero",
		)
	}

	safeURL := sanitizeHTTPURL(parsedURL)

	result := diagnostic.Result{
		Name:     "http",
		Required: true,
		Details: map[string]any{
			"method":                http.MethodGet,
			"url":                   safeURL,
			"expected_status_code":  expectedStatus,
			"timeout_ms":            timeout.Milliseconds(),
			"query_parameters_used": parsedURL.RawQuery != "",
		},
	}

	attemptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(
		attemptCtx,
		http.MethodGet,
		parsedURL.String(),
		nil,
	)
	if err != nil {
		return diagnostic.Result{}, fmt.Errorf(
			"create HTTP request: %w",
			err,
		)
	}

	// Clone the client to avoid changing redirect behavior for other users
	// of the supplied client.
	requestClient := *client
	requestClient.CheckRedirect = func(
		_ *http.Request,
		_ []*http.Request,
	) error {
		return http.ErrUseLastResponse
	}

	startedAt := time.Now()
	response, requestErr := requestClient.Do(request)
	result.Duration = time.Since(startedAt)

	if requestErr != nil {
		result.Error = sanitizeHTTPError(requestErr, targetURL, safeURL)

		if errors.Is(requestErr, context.Canceled) ||
			errors.Is(requestErr, context.DeadlineExceeded) ||
			ctx.Err() != nil ||
			attemptCtx.Err() != nil {
			result.Status = diagnostic.StatusInconclusive
			result.Message = "HTTP request did not complete within the available context"
		} else {
			result.Status = diagnostic.StatusFailed
			result.Message = "HTTP request failed"
		}

		return result, nil
	}

	if response == nil {
		result.Status = diagnostic.StatusFailed
		result.Message = "HTTP client returned no response"
		result.Error = "HTTP client returned a nil response without an error"

		return result, nil
	}

	defer response.Body.Close()

	result.Details["status_code"] = response.StatusCode
	result.Details["status"] = response.Status
	result.Details["protocol"] = response.Proto
	result.Details["content_type"] = response.Header.Get("Content-Type")
	result.Details["content_length"] = response.ContentLength
	result.Details["is_redirect"] = response.StatusCode >= 300 &&
		response.StatusCode < 400
	result.Details["location_present"] = response.Header.Get("Location") != ""

	if response.StatusCode != expectedStatus {
		result.Status = diagnostic.StatusFailed
		result.Message = fmt.Sprintf(
			"HTTP request returned status %d; expected %d",
			response.StatusCode,
			expectedStatus,
		)

		return result, nil
	}

	result.Status = diagnostic.StatusPassed
	result.Message = fmt.Sprintf(
		"HTTP request returned the expected status %d",
		expectedStatus,
	)

	return result, nil
}

// sanitizeHTTPURL produces a URL suitable for diagnostic output.
//
// Query parameters and fragments are removed because they can contain tokens,
// credentials, or other sensitive values.
func sanitizeHTTPURL(parsedURL *url.URL) string {
	safeURL := *parsedURL
	safeURL.User = nil
	safeURL.RawQuery = ""
	safeURL.ForceQuery = false
	safeURL.Fragment = ""
	safeURL.RawFragment = ""

	return safeURL.String()
}

// sanitizeHTTPError removes the original target URL from an error message
// when possible, preventing query parameters from appearing in diagnostics.
func sanitizeHTTPError(
	err error,
	targetURL string,
	safeURL string,
) string {
	if err == nil {
		return ""
	}

	message := err.Error()

	if targetURL != "" {
		message = strings.ReplaceAll(message, targetURL, safeURL)
	}

	return message
}
