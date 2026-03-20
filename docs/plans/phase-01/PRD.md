# Product Requirements Document: mnemonic-enricher Phase 01

*Gralph processes cycles in this document from top to bottom. Checklist markers are significant: `- [ ]` (open), `- [x]` (complete), `- [~]` (abandoned). Each cycle must be small, independently verifiable, and assigned to exactly one agent.*

## Objective

Strip `mnemonic-enricher` down to a focused enrichment worker: subscribe to a message queue,
process enrichment jobs (embeddings + Neo4j graph sync), and expose `/health` and `/version`
via Gin. Queue interaction is abstracted behind a `queue.Subscriber` interface so providers
(RabbitMQ, AWS SQS, Azure Service Bus) can be swapped without touching the worker.

## Problem Statement

The current codebase is a copy of the monolithic `mnemonic` service. It carries REST API
handlers, MCP server, skill/skillfile services, and a DB-polling enrichment loop that belong
to the parent service. These must be removed so this repo has a single responsibility. The
polling loop must be replaced with queue subscription to decouple job delivery from the
database. Without this split, the enricher cannot be deployed or scaled independently.

## Success Criteria

- `make build` exits 0 from the project root (unit tests + Docker image + e2e tests all pass)
- The binary subscribes to a RabbitMQ queue and processes enrichment jobs end-to-end
- `GET /health` and `GET /version` return 200; no other HTTP routes are registered
- No source file imports any deleted package (agents, patterns, skills, skillfiles, mcpserver, search)
- The `queue.Subscriber` interface has no RabbitMQ-specific types in its signature
- Adding a second queue provider requires only a new `internal/queue/<provider>/` package and a case in the server's provider switch

## Scope

### In scope

- Delete all non-enrichment handlers, services, repositories, and MCP server
- Fix the global module path mismatch (`mnemonic` → `mnemonic-enricher`) across all source files
- Introduce `internal/queue/queue.go` (Delivery + Subscriber interface) and `internal/queue/rabbitmq/` (first implementation)
- Add `QueueConfig` / `RabbitMQConfig` to the config system with full env-var bindings
- Refactor `internal/enricher/enrichment.go` Worker to consume from `queue.Subscriber` instead of polling
- Update `internal/service/enrichment/service.go`: replace `ClaimNextJob` with `GetJob` + `MarkJobProcessing`
- Update `internal/server/server.go` wiring to construct the provider-correct Subscriber and pass it to the worker
- Add RabbitMQ service to `docker-compose.yaml` (dev) and `src/tests/docker-compose.yaml` (e2e)
- Fix the wrong workflow filename reference in `.github/workflows/mnemonic-enrichment-ci.yaml`
- Trim e2e test helpers and doc files to match reduced scope

### Out of scope

- Adding AWS SQS or Azure Service Bus implementations (interface only)
- Writing new enrichment e2e tests (a future phase)
- Updating `README.md` or architecture docs (deferred)
- Any change to the enrichment pipeline logic itself (`processChunkJob`, `runGraphPipeline`, etc.)

## Constraints and Decisions

- Go module: `github.com/twistingmercury/mnemonic-enricher`
- Queue client: `github.com/rabbitmq/amqp091-go` (official RabbitMQ Go client)
- Queue abstraction: `internal/queue/queue.go` — `Delivery` struct + `Subscriber` interface; Worker has zero provider-specific imports
- Provider selection: `MNEMONIC_QUEUE_PROVIDER` env var (default `"rabbitmq"`); server wiring uses a `switch` to construct the correct `Subscriber`
- RabbitMQ message body format: `{"job_id": "<uuid>"}` — references the `enrichment_jobs` DB row
- `enrichment_jobs` table is retained for job status observability and manual re-queuing of failed jobs
- `EnrichmentConfig.PollInterval` removed — dead config once polling is gone
- All commits must be signed (`git commit -S`)
- Full build verification: `make build` from the project root

## Implementation Plan

- [x] **Cycle 1 - Fix module path and remove routes.go**: Replace all remaining `github.com/twistingmercury/mnemonic/` import prefixes with `github.com/twistingmercury/mnemonic-enricher/` across all Go source files; delete `internal/server/routes.go` and remove its call site from `server.go` so the project compiles cleanly.
  - Agent: `go software engineer`
  - Files: `src/internal/server/server.go`, `src/internal/server/routes.go` (delete), all `.go` files with old import prefix
  - Steps:
    - Find every `.go` file under `src/` that imports `github.com/twistingmercury/mnemonic/` (excluding already-correct `mnemonic-enricher` occurrences) and replace the prefix with `github.com/twistingmercury/mnemonic-enricher/`
    - Delete `src/internal/server/routes.go`
    - Remove the `RegisterAPIRoutes` call and its swagger-related imports from `src/internal/server/server.go`
  - Verify: `cd src && go build ./...`
  - Done: `go build ./...` exits 0 with zero errors

- [x] **Cycle 2 - Delete non-enrichment packages**: Remove all handler, service, repository, and MCP packages unrelated to enrichment; remove their imports and wiring from `server.go`.
  - Agent: `go software engineer`
  - Files: `src/internal/handlers/agents/`, `src/internal/handlers/patterns/`, `src/internal/handlers/skills/`, `src/internal/handlers/skillfiles/`, `src/internal/service/agent/`, `src/internal/service/pattern/`, `src/internal/service/search/`, `src/internal/service/skill/`, `src/internal/service/skillfile/`, `src/internal/repository/skill/`, `src/internal/repository/skillfile/`, `src/internal/mcpserver/`, `src/docs/swagger/`, `src/internal/handlers/respond.go`, `src/internal/handlers/respond_test.go`, `src/internal/server/server.go`
  - Steps:
    - Delete the directories and files listed above
    - Remove all imports of deleted packages from `src/internal/server/server.go`
    - Remove `MCP` port from the startup log line in `server.go`
    - Simplify `wireDependencies` in `server.go`: remove skill, agent (service layer), pattern (service layer), search, and skillfile wiring; retain enrichmentSvc and enrichWorker; return `(*enricher.Worker, error)`
    - Remove the `runMCPServer` goroutine and its `g.Go` call from `ListenAndServe`
  - Verify: `cd src && go build ./... && go test ./...`
  - Done: `go build ./...` exits 0; `go test ./...` passes; none of the deleted package paths exist under `src/internal/`

- [x] **Cycle 3 - Add queue abstraction package**: Create `internal/queue/queue.go` with the provider-agnostic `Delivery` struct and `Subscriber` interface; create the RabbitMQ implementation with reconnect logic; add `github.com/rabbitmq/amqp091-go` to `go.mod`.
  - Agent: `go software engineer`
  - Files: `src/internal/queue/queue.go`, `src/internal/queue/rabbitmq/subscriber.go`, `src/internal/queue/rabbitmq/subscriber_test.go`, `src/go.mod`, `src/go.sum`
  - Steps:
    - Run `cd src && go get github.com/rabbitmq/amqp091-go` to add the dependency
    - Create `src/internal/queue/queue.go` with the `Delivery` struct (`Body []byte`, `Ack func() error`, `Nack func(requeue bool) error`) and the `Subscriber` interface (`Subscribe(ctx context.Context) (<-chan Delivery, error)`, `Name() string`, `Close() error`)
    - Create `src/internal/queue/rabbitmq/subscriber.go` implementing `queue.Subscriber`: dial RabbitMQ, set QoS prefetch, consume the configured queue, internally reconnect on connection loss (respects ctx), adapt `amqp091.Delivery` → `queue.Delivery`; `Name()` returns the configured queue name
    - Create `src/internal/queue/rabbitmq/subscriber_test.go` with unit tests covering message delivery and Ack/Nack behaviour using a mock or stub
  - Verify: `cd src && go test ./internal/queue/...`
  - Done: `go test ./internal/queue/...` exits 0; `queue.Subscriber` interface has no amqp091 types in its signature

- [x] **Cycle 4 - Add QueueConfig to config**: Introduce `QueueConfig` and `RabbitMQConfig` structs in `config.go`; add `Queue QueueConfig` to `MnemonicConfig`; register defaults and viper bindings; remove `EnrichmentConfig.PollInterval`.
  - Agent: `go software engineer`
  - Files: `src/internal/config/config.go`, `src/internal/config/defaults.go`, `src/internal/config/config_test.go`
  - Steps:
    - Add `QueueConfig` struct with `Provider string` (env `MNEMONIC_QUEUE_PROVIDER`, default `"rabbitmq"`) and `RabbitMQ RabbitMQConfig` field
    - Add `RabbitMQConfig` struct with fields: `Host` (`MNEMONIC_QUEUE_RABBITMQ_HOST`), `Port` (`MNEMONIC_QUEUE_RABBITMQ_PORT`, default `5672`), `User` (`MNEMONIC_QUEUE_RABBITMQ_USER`), `Password` (`MNEMONIC_QUEUE_RABBITMQ_PASSWORD`), `VHost` (`MNEMONIC_QUEUE_RABBITMQ_VHOST`, default `"/"`), `Queue` (`MNEMONIC_QUEUE_RABBITMQ_QUEUE`, default `"enrichment-jobs"`), `PrefetchCount` (`MNEMONIC_QUEUE_RABBITMQ_PREFETCH_COUNT`, default `5`), `ReconnectDelay time.Duration` (`MNEMONIC_QUEUE_RABBITMQ_RECONNECT_DELAY`, default `5s`)
    - Add `Queue QueueConfig` field to `MnemonicConfig`
    - Register all new viper keys and defaults in `defaults.go`
    - Remove `PollInterval` field from `EnrichmentConfig` and its default/binding
    - Update `config_test.go` to cover new fields and absence of `PollInterval`
  - Verify: `cd src && go test ./internal/config/...`
  - Done: `go test ./internal/config/...` exits 0; `MnemonicConfig` has a `Queue QueueConfig` field; `EnrichmentConfig` has no `PollInterval` field

- [x] **Cycle 5 - Add GetJob and MarkJobProcessing to enrichment service**: Extend the `Service` interface and its implementation with `GetJob` and `MarkJobProcessing`; keep `ClaimNextJob` in place for now (removed in Cycle 6).
  - Agent: `go software engineer`
  - Files: `src/internal/service/enrichment/service.go`, `src/internal/service/enrichment/service_test.go`
  - Steps:
    - Add `GetJob(ctx context.Context, jobID uuid.UUID) (*enrichmentjob.Job, error)` to the `Service` interface; implement it as a delegate to `jobRepo.Get(ctx, jobID)`
    - Add `MarkJobProcessing(ctx context.Context, jobID uuid.UUID) error` to the `Service` interface; implement it as a delegate to `jobRepo.MarkProcessing(ctx, jobID)`
    - Update `service_test.go` to cover the two new methods
  - Verify: `cd src && go test ./internal/service/enrichment/...`
  - Done: `go test ./internal/service/enrichment/...` exits 0; `Service` interface exposes `GetJob` and `MarkJobProcessing`

- [x] **Cycle 6 - Refactor enrichment worker and finalise service interface**: Remove `ClaimNextJob` from the `Service` interface; refactor the enrichment `Worker` to consume from `queue.Subscriber`; update `server.go` to construct the provider-correct subscriber and pass it to the worker.
  - Agent: `go software engineer`
  - Files: `src/internal/service/enrichment/service.go`, `src/internal/service/enrichment/service_test.go`, `src/internal/enricher/enrichment.go`, `src/internal/enricher/enrichment_test.go`, `src/internal/server/server.go`
  - Steps:
    - Remove `ClaimNextJob` from the `Service` interface and its implementation
    - Update `Worker` struct: replace `svc enrichmentsvc.Service, cfg config.EnrichmentConfig` fields with `svc enrichmentsvc.Service, sub queue.Subscriber, cfg config.EnrichmentConfig`; update `New` constructor signature accordingly
    - Replace `runWorker` and `sleep` methods with a `handleDelivery(ctx context.Context, d queue.Delivery)` method: unmarshal `{"job_id":"<uuid>"}` from `d.Body`, call `svc.GetJob`, call `svc.MarkJobProcessing`, call `svc.ProcessJob`, then `d.Ack()` on success or `d.Nack(false)` on unrecoverable error
    - Update `Run`: call `w.sub.Subscribe(ctx)` to get `<-chan queue.Delivery`; log `w.sub.Name()` at startup; fan out deliveries to goroutines bounded by semaphore of size `cfg.WorkerCount`; on `ctx.Done()` drain in-flight within `cfg.DrainTimeout` then call `w.sub.Close()`; keep maintenance goroutine unchanged
    - Remove all `PollInterval` references from `enrichment.go`
    - In `server.go` `wireDependencies`: add a `switch cfg.Queue.Provider` block that constructs `rabbitmq.NewSubscriber(cfg.Queue.RabbitMQ)` for `"rabbitmq"` and returns an error for unknown providers; pass the subscriber into `enricher.New`
    - Update `enrichment_test.go` and `service_test.go` to reflect removed `ClaimNextJob`
  - Verify: `cd src && go build ./... && go test ./internal/enricher/... ./internal/service/enrichment/...`
  - Done: `go build ./...` exits 0; enricher package imports no amqp091 types; `Worker` struct has no `cfg.PollInterval` references; tests pass

- [x] **Cycle 7 - Update docker-compose files and fix CI workflow**: Add RabbitMQ service to both compose files; add `MNEMONIC_QUEUE_*` env vars to enricher service entries; fix the wrong workflow filename in the CI push trigger.
  - Agent: `devops engineer`
  - Files: `docker-compose.yaml`, `src/tests/docker-compose.yaml`, `.github/workflows/mnemonic-enrichment-ci.yaml`
  - Steps:
    - Add `dev_rabbitmq` service (`rabbitmq:4.0-management`, ports 5672/15672, healthcheck via `rabbitmq-diagnostics ping`) to `docker-compose.yaml` on `dev_network`; add `MNEMONIC_QUEUE_PROVIDER`, `MNEMONIC_QUEUE_RABBITMQ_HOST`, `MNEMONIC_QUEUE_RABBITMQ_USER`, `MNEMONIC_QUEUE_RABBITMQ_PASSWORD`, and `MNEMONIC_QUEUE_RABBITMQ_QUEUE` env vars to the enricher service; add `dev_rabbitmq` to its `depends_on`
    - Apply the same RabbitMQ service addition to `src/tests/docker-compose.yaml` scoped to `enrichment_network`; add the same env vars to `e2e_enricher`
    - In `.github/workflows/mnemonic-enrichment-ci.yaml`, change the push-trigger path `.github/workflows/mnemonic-ci.yaml` to `.github/workflows/mnemonic-enrichment-ci.yaml`
  - Verify: `docker compose -f docker-compose.yaml config > /dev/null && docker compose -f src/tests/docker-compose.yaml config > /dev/null`
  - Done: both compose files parse without error; each contains a RabbitMQ service; the CI workflow push trigger references the correct filename

- [ ] **Cycle 8 - Cleanup helpers and doc comments**: Remove non-enrichment types from e2e helpers; update package doc comments to reflect reduced scope.
  - Agent: `go software engineer`
  - Files: `src/tests/e2e/helpers/types.go`, `src/tests/e2e/helpers/helpers.go`, `src/internal/handlers/doc.go`, `src/internal/service/doc.go`, `src/internal/repository/doc.go`
  - Steps:
    - Remove types and helper functions for agents, patterns, skills, and skillfiles from `src/tests/e2e/helpers/types.go` and `helpers.go`; retain only types and helpers used by `operations_test.go`
    - Update `src/internal/handlers/doc.go` to reflect that only the `operations` sub-package remains
    - Update `src/internal/service/doc.go` and `src/internal/repository/doc.go` to remove references to deleted packages
  - Verify: `cd src && go test ./...`
  - Done: `go test ./...` exits 0; no references to deleted packages remain in doc comments or e2e helpers

- [ ] **Cycle 9 - Update README.md, CHANGELOG.md, and Makefile**: Rewrite README.md and CHANGELOG.md to reflect the enricher as a new standalone project; update the Makefile to remove stale targets and document the current build/run workflow.
  - Agent: `technical writer`
  - Files: `README.md`, `CHANGELOG.md`, `Makefile`
  - Steps:
    - Rewrite `README.md` using the `/readme-writer` skill: describe the service as a standalone enrichment worker, document the queue abstraction and RabbitMQ as the default provider, list required environment variables (`MNEMONIC_QUEUE_*`, `MNEMONIC_DATABASE_*`, `MNEMONIC_OPENAI_*`), document `make build` as the primary build command, and include a brief getting-started section using `docker-compose up`
    - Rewrite `CHANGELOG.md` from scratch as a new project: establish an initial entry dated today marking this as the first release of `mnemonic-enricher` as a standalone service extracted from `mnemonic`; follow Keep a Changelog format
    - Remove any Makefile targets that reference deleted packages, the MCP server, skill/skillfile services, or the REST API; ensure `make build` invokes `src/build/build.sh` and the target is clearly documented
  - Verify: `make build`
  - Done: `make build` exits 0; `README.md` and `CHANGELOG.md` contain no references to agents, patterns, skills, skillfiles, MCP, or search REST endpoints

## Risks and Mitigations

- Risk: Cycle 1 find-replace touches `mnemonic-enricher` occurrences and double-replaces them.
  - Mitigation: Match the exact prefix `"github.com/twistingmercury/mnemonic/` (with trailing slash and leading quote) to avoid hitting already-correct paths.

- Risk: Removing `ClaimNextJob` in Cycle 6 breaks the worker before `handleDelivery` is wired; causes a compile gap between steps within the cycle.
  - Mitigation: Cycle 6 is a single atomic commit — all changes (interface, worker, server wiring) land together.

- Risk: RabbitMQ reconnect logic in `subscriber.go` is complex to unit-test without a real broker.
  - Mitigation: The subscriber test uses a mock/stub for connection and channel; reconnect behaviour is covered by integration tests (future phase). Cycle 3 Done condition only requires the existing tests to pass.

- Risk: `EnrichmentConfig.PollInterval` removal in Cycle 4 may break callers that still reference the field.
  - Mitigation: After Cycle 4, `go build ./...` catches any dangling references immediately; fix in the same cycle before committing.

- Risk: Cycle 2 `wireDependencies` simplification leaves server.go importing agentRepo/patternRepo (still needed by enrichmentSvc). These repos must not be deleted.
  - Mitigation: The delete list explicitly names only service-layer packages (`service/agent`, `service/pattern`); repository packages (`repository/agent`, `repository/pattern`) are retained.

## Definition of Done

- `make build` exits 0 from the project root
- `GET /health` returns 200; `GET /version` returns 200; no other routes respond
- `go build ./...` exits 0 under `src/`
- No source file under `src/internal/` imports a deleted package
- `queue.Subscriber` interface contains no provider-specific types
