# Contributing to Harness Observer

Thanks for helping improve Harness Observer.

The project is intentionally conservative about evidence: local session artifacts are treated as source data, and unsupported fields stay unknown rather than being inferred for presentation convenience.

## Before you start

For bug fixes and small improvements, open a focused pull request directly.

For a new harness adapter or a change to the normalized evidence contract, explain the persisted source format and semantic mapping in the PR description first. Real format evidence is more useful than an abstract compatibility claim.

## Development checks

Backend:

```bash
gofmt -l .
go vet ./...
go test ./...
```

Frontend:

```bash
cd webui
npm ci
npm run typecheck
npm test -- --run
npm run build
```

Platform-specific DuckDB/CGO setup is documented in [`docs/build.md`](docs/build.md).

## Adapter rules

A new adapter should document and test:

1. source discovery and platform paths
2. which local artifact is authoritative
3. token/cache/reasoning field semantics
4. session/turn/project/model/provider attribution
5. dedup and incremental checkpoint strategy
6. structured tool/error evidence where available
7. unsupported or ambiguous fields
8. minimized regression fixtures for format mismatches

Do not modify agent-owned session files, credentials or configuration.

Do not add guessed values merely to make cross-harness tables look complete. In particular, do not infer reasoning effort, request latency, HTTP status, retries or tool success when the source did not persist that evidence.

## Sensitive fixtures

Do not commit real prompts, responses, tool arguments, command output, credentials, OAuth material or identifiable private session content.

When a real session exposes a parser bug, reduce it to the smallest synthetic fixture that preserves the relevant structure and counters.

## Pull requests

Keep changes scoped and production-shaped. Prefer implementation plus regression tests over mock-only or design-only submissions when the code can reasonably be changed.

Please keep the following green:

```text
frontend typecheck/tests/build
gofmt
go vet ./...
go test ./...
```

## Commit email privacy

Maintainers and contributors who want GitHub email privacy should use their GitHub-provided noreply address for commits. Do not put personal email addresses into project documentation, examples or generated files.

## License

By contributing, you agree that your contributions are licensed under the repository's MIT License.
