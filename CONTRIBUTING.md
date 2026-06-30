<!-- SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Contributing to opensearch-query-exporter

Thank you for your interest in contributing to opensearch-query-exporter! This document explains how to get involved.

## Reporting Bugs

Please open a [GitHub issue](https://github.com/SAP-cloud-infrastructure/opensearch-query-exporter/issues) with:

- A clear, descriptive title
- Steps to reproduce the problem
- Expected vs. actual behavior
- Go version, OS, and any relevant environment details

**Do not report security vulnerabilities as GitHub issues.** See [SECURITY.md](SECURITY.md) instead.

## Submitting Changes

1. **Fork** the repository and create a feature branch from `main`.
2. **Make your changes** on the branch.
3. **Ensure all checks pass** (see below).
4. **Open a pull request** against `main` with a clear description of the change and its motivation.

## Development Setup

Prerequisites: Go 1.26+

```bash
# Build
make build

# Run tests
make test
```

## Code Style

- Follow standard Go conventions (`gofmt`, `goimports`).
- Use table-driven tests for unit tests where applicable.

## Commit Messages

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add new query field for support_group label
fix: handle empty aggregation buckets
refactor: extract response parsing into pkg/parser
test: cover credential failover edge cases
docs: update configuration reference
```

## License

This project is licensed under the [Apache License 2.0](LICENSES/Apache-2.0.txt).

## Developer Certificate of Origin (DCO)

Due to legal reasons, contributors will be asked to accept a DCO when they create the first pull request to this project. This happens in an automated fashion during the submission process. SAP uses [the standard DCO text of the Linux Foundation](https://developercertificate.org/).

By contributing, you agree that your contributions will be licensed under the same license.

Contributions must follow our [guidelines on AI-generated code](https://github.com/SAP/.github/blob/main/CONTRIBUTING_USING_GENAI.md) in case you are using such tools.

All new source files must include SPDX license headers:

```go
// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0
```

## Code of Conduct

This project follows the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code.
