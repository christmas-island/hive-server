# CLAUDE.md

Go service scaffold for christmas-island projects.

## Structure

- `cmd/app/` — CLI entrypoint (cobra)
- `pkg/` — Public packages
- `internal/` — Private packages (log)
- `k8s/` — Kubernetes manifests
- `.github/` — CI/CD workflows

## Conventions

- Idiomatic Go, `golangci-lint` for linting
- Tests alongside code (`_test.go`)
- Multi-stage Docker builds
- k8s deploy via kustomize
