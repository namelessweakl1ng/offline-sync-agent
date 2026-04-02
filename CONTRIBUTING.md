# Contributing

Thanks for contributing to `offline-sync-agent`.

## Local setup

1. Install Go 1.22 or newer.
2. Copy `.env.example` into your shell environment.
3. Start the backend with `make run-server`.
4. Use `make run-client ARGS='help'` to explore the CLI.

## Development workflow

- Keep changes incremental and focused.
- Prefer extending the existing abstractions over bypassing them.
- Preserve the offline-first behavior when refactoring.
- Use structured logs instead of ad-hoc `fmt.Println` debugging.

## Commands

```bash
make build
make test
make fmt
make tidy
make run-server
make run-client ARGS='sync --once'
```

## Testing expectations

- Add or update unit tests for behavior changes.
- Make sure `go test ./...` and `go build ./...` succeed before submitting a PR.
- When touching sync behavior, include conflict-path coverage whenever practical.

## Code style

- Follow idiomatic Go naming and error handling.
- Keep packages loosely coupled and testable.
- Prefer typed request and response structs over `map[string]any`.
- Avoid hidden global state in new code.

## Pull requests

- Describe the problem and the intended fix clearly.
- Call out any behavior changes, especially around retry logic, conflict handling, or API responses.
- Include manual verification steps when the change is operational in nature.
