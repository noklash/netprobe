# Changelog

Notable changes to NetProbe are documented here.

## Unreleased

### Added
- Established the initial Go module and package structure.
- Defined the diagnostic result model, including passed, failed, skipped, and inconclusive statuses.
- Implemented diagnostic report aggregation based on required check outcomes.
- Added DNS resolution checks with IPv4 and IPv6 address separation, address normalization, duplicate removal, and context-aware error classification.
- Added TCP connectivity checks with destination validation, bounded connection attempts, context cancellation handling, and connection resource cleanup.
- Added unit tests for diagnostic models, report aggregation, DNS resolution, and TCP connectivity.

### Documentation
- Documented the system architecture and component boundaries.
- Defined diagnostic result semantics and overall status aggregation rules.
- Documented environmental limitations and the interpretation of DNS and TCP results.
- Recorded the initial IPv6 connectivity experiment and its limitations.

### Testing
- Verified the current packages with `go test ./...`.
- Verified the test suite with `go test -race ./...`.
- Ran static analysis with `go vet ./...`.
- Verified package builds with `go build ./...`.
- Repeated the network-check tests to check for inconsistent behavior.

### Planned
- Implement diagnostic orchestration.
- Introduce the command-line interface.
- Add TLS handshake diagnostics.
- Add HTTP response checks.
- Implement human-readable and JSON output.
- Expand integration testing and document end-to-end diagnostic workflows.