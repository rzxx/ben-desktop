# Task completion requirements

All checks should pass:

Go format + checks:

```sh
goimports -w .
golangci-lint run ./...
go test ./...
govulncheck ./...
```

For race tests on Windows, use the MSYS2 MINGW64 terminal with libmpv installed:

```sh
CGO_ENABLED=1 CC=gcc go test -race ./...
```

Frontend format + checks from `frontend/`.

```sh
bun format
bun lint
bun typecheck
bun run test:run
```
