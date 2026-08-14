# Agent Vault Development Guide

This file is for contributors and coding agents working in this repository. Agent Vault is a Go credential broker with a React management UI, a transparent HTTP(S) proxy, and SQLite and PostgreSQL storage backends. Product usage belongs in `README.md`, `docs/`, and `skills/agent-vault-cli/`, not in this file.

## Toolchain and commands

Tool versions and local development tasks are defined in `mise.toml`.

- `mise run web:setup` installs the web development dependencies.
- `mise run dev` starts the Go API with hot reload and the Vite frontend.
- `mise run test` runs the Go test suite.
- `mise run lint` runs Go linting and the web TypeScript check.
- `mise run build` builds the frontend and the `agent-vault` binary.
- `mise run sdk:check` installs, typechecks, tests, and builds the TypeScript SDK.
- `mise run docker:build` builds the Docker image.

Use focused package tests while iterating, then run the applicable repository-level checks before considering the work complete.

## Repository map

- `main.go` and `cmd/`: Cobra CLI entrypoint and commands.
- `internal/server/`: HTTP API, authentication boundaries, and embedded SPA serving.
- `internal/broker/` and `internal/brokercore/`: service matching, request transformation, and credential injection.
- `internal/proposal/`: proposal types, validation, and merge behavior.
- `internal/mitm/` and `internal/ca/`: HTTP(S) proxying, TLS interception, and CA management.
- `internal/store/`: SQLite and PostgreSQL persistence, dialect handling, and schema migrations.
- `internal/isolation/`: host and container execution modes for `vault run`.
- `web/`: React, TypeScript, Vite, and Tailwind management UI.
- `sdks/sdk-typescript/`: published TypeScript SDK.
- `skills/agent-vault-cli/`: the agent-facing runtime contract embedded in the binary.
- `docs/`: Mintlify documentation source.

## Development invariants

- `web/` builds into `internal/server/webdist/`, which is embedded with `go:embed`. Treat `webdist` as generated output: change the source under `web/` and do not edit or commit generated files.
- Schema migrations are Go files under `internal/store/` registered with `RegisterGORMMigration`. New migrations must work for both SQLite and PostgreSQL and include migration verification coverage.
- Packages whose tests can inherit `AGENT_VAULT_*` variables from the developer shell should use the existing `internal/testenv` `TestMain` pattern. Tests that exercise an environment variable explicitly should set it with `t.Setenv`.
- For agent token lifecycle work, use the domain terms defined in `CONTEXT.md`: an agent token renews itself, while an authorized operator rotates it.

## Change propagation

- When agent-facing endpoints, request or response fields, authentication behavior, or failure handling change, update `skills/agent-vault-cli/` and any applicable pages under `docs/`.
- When adding, consuming, or changing the fallback behavior of an environment variable, update `.env.example`, `docs/self-hosting/environment-variables.mdx`, and the environment variable table in `docs/reference/cli.mdx`.
- When adding or changing a CLI flag, update the affected command in `docs/reference/cli.mdx`.
- When operator-facing behavior changes, update `README.md` and scan the relevant quickstart, guide, self-hosting, learn, and reference pages under `docs/`.
- When the TypeScript SDK's public behavior changes, update its tests and `sdks/sdk-typescript/README.md`.

## Verification by scope

- Go changes: run focused tests while iterating, followed by `mise run test` and `mise run lint`.
- Web changes: run `mise run lint` and `mise run build`.
- TypeScript SDK changes: run `mise run sdk:check`.
- Go dependency changes: run `go mod tidy` and verify that only the intended `go.mod` and `go.sum` changes remain.
- Container isolation changes: run the Docker integration tests documented in `internal/isolation/integration_test.go` when Docker is available.

## Sources of truth

- CLI behavior: Cobra commands under `cmd/` and `agent-vault --help`.
- HTTP API behavior: handlers under `internal/server/`.
- Environment variables: `.env.example`.
- User and operator documentation: `README.md` and `docs/`.
- Agent runtime behavior: `skills/agent-vault-cli/`.
