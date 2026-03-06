# Contributing to go-audit

Thank you for your interest in contributing to go-audit! This guide will help
you get started.

## Getting Started

1. **Fork the repo** and clone your fork:
   ```bash
   git clone https://github.com/<your-fork>/go-audit.git
   cd go-audit
   ```

2. **Install Go 1.21+** (any version ≥ 1.21 is supported).

3. **Run the tests**:
   ```bash
   go test ./... -race -count=1
   ```

4. **Run the linter**:
   ```bash
   golangci-lint run
   ```

## Development Workflow

1. Create a feature branch from `main`:
   ```bash
   git checkout -b feat/my-feature
   ```

2. Write your changes with tests. We aim for ≥85% test coverage.

3. Ensure all tests pass:
   ```bash
   make test
   ```

4. Commit with a descriptive message following
   [Conventional Commits](https://www.conventionalcommits.org/):
   ```
   feat(diff): add support for nested map comparison
   fix(wal): handle fsync failure on macOS gracefully
   docs: update README quick start section
   ```

5. Push and open a Pull Request against `main`.

## Code Style

- Follow standard Go conventions (`gofmt`, `goimports`).
- All public symbols must have doc comments.
- Prefer table-driven tests.
- Use `domain.Err*` sentinel errors — never return raw `errors.New()` from
  public APIs.
- Keep the core `go.mod` dependency-free (except `pgregory.net/rapid` for
  property tests). External dependencies go in sub-modules.

## Architecture

go-audit uses **hexagonal (ports & adapters) architecture**:

```
domain/          ← pure business logic, zero dependencies
domain/port/     ← interfaces (ports)
adapter/         ← implementations (adapters)
app/             ← application services (worker pool, routing)
```

See [ARCHITECTURE.md](ARCHITECTURE.md) for details.

## Adding a New Adapter

1. Create a new package under `adapter/<category>/<name>/`.
2. Implement the corresponding port interface from `domain/port/`.
3. Add a compile-time interface check: `var _ port.XxxPort = (*MyAdapter)(nil)`.
4. If the adapter requires external dependencies, create a **sub-module**
   with its own `go.mod`.
5. Add tests and a mock in `testing/mocks.go`.

## Running Integration Tests

Integration tests require Docker:

```bash
make test-integ
```

This starts PostgreSQL, ClickHouse, and LocalStack via `docker-compose.test.yml`.

## Reporting Issues

Please use the [issue templates](.github/ISSUE_TEMPLATE/) when filing bugs or
feature requests.

## Code of Conduct

This project follows the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md).

## License

By contributing, you agree that your contributions will be licensed under the
[Apache License 2.0](LICENSE).
