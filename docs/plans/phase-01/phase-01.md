# Plan: mnemonic-enricher Focused Scope + Queue Abstraction

## Context

`mnemonic-enricher` is being split off from the monolithic `mnemonic` project to be a
standalone enrichment worker. Its sole purpose: subscribe to a message queue, process
enrichment jobs (embeddings + Neo4j graph sync), and expose `/health` and `/version` via Gin.
All REST API handlers, MCP server, and unrelated services are being removed. The
`enrichment_jobs` Postgres table is kept for job status observability and re-queuing failed
jobs.

Queue interaction is abstracted behind a `queue.Subscriber` interface so providers
(RabbitMQ, AWS SQS, Azure Service Bus) can be swapped without touching the Worker.

**Module path mismatch:** all source files currently import
`github.com/twistingmercury/mnemonic` but `go.mod` declares
`github.com/twistingmercury/mnemonic-enricher`. This must be fixed globally.

---

## Phase 1 — Delete non-enrichment code

### Directories/files to delete entirely

| Path                                   | Reason                                                |
| -------------------------------------- | ----------------------------------------------------- |
| `src/internal/handlers/agents/`        | REST CRUD — belongs to main mnemonic service          |
| `src/internal/handlers/patterns/`      | REST CRUD — belongs to main mnemonic service          |
| `src/internal/handlers/skills/`        | Not enrichment-related                                |
| `src/internal/handlers/skillfiles/`    | Not enrichment-related                                |
| `src/internal/service/agent/`          | REST service layer (repos still needed by enrichment) |
| `src/internal/service/pattern/`        | REST service layer (repos still needed by enrichment) |
| `src/internal/service/search/`         | REST semantic search (not needed in enricher)         |
| `src/internal/service/skill/`          | Not enrichment-related                                |
| `src/internal/service/skillfile/`      | Not enrichment-related                                |
| `src/internal/repository/skill/`       | Not enrichment-related                                |
| `src/internal/repository/skillfile/`   | Not enrichment-related                                |
| `src/internal/mcpserver/`              | MCP server — not part of enricher                     |
| `src/docs/swagger/`                    | API docs for removed REST endpoints                   |
| `src/tests/e2e/api/agents_test.go`     | REST endpoint being removed                           |
| `src/tests/e2e/api/patterns_test.go`   | REST endpoint being removed                           |
| `src/tests/e2e/api/skills_test.go`     | REST endpoint being removed                           |
| `src/tests/e2e/api/skillfiles_test.go` | REST endpoint being removed                           |
| `src/tests/e2e/mcp/`                   | MCP server being removed                              |

### Files to verify and remove if unused

- `src/internal/handlers/respond.go` + `respond_test.go` — remove if only used by deleted handlers
- `src/internal/server/routes.go` — remove entirely; health/version registered directly in `server.go`

---

## Phase 2 — Fix module path

Global find-replace across all `.go` files:

```
github.com/twistingmercury/mnemonic/ → github.com/twistingmercury/mnemonic-enricher/
```

(Exact prefix match to avoid touching `mnemonic-enricher` occurrences.)

---

## Phase 3 — Queue abstraction + RabbitMQ integration

### 3a. New `internal/queue/` package

Create the provider-agnostic queue abstraction. The enrichment Worker imports only this
package — it has zero knowledge of RabbitMQ, SQS, or any provider.

**`internal/queue/queue.go`**

```go
package queue

import "context"

// Delivery is a provider-agnostic message wrapper.
type Delivery struct {
    Body []byte
    Ack  func() error
    Nack func(requeue bool) error
}

// Subscriber is the interface implemented by all queue providers.
type Subscriber interface {
    // Subscribe returns a channel of Delivery messages and runs until ctx is cancelled.
    // Reconnect logic is handled internally by the implementation.
    Subscribe(ctx context.Context) (<-chan Delivery, error)
    // Name returns a human-readable description of the subscription (e.g. queue name).
    // Used for logging at Worker startup.
    Name() string
    // Close cleanly shuts down the subscriber.
    Close() error
}
```

**`internal/queue/rabbitmq/subscriber.go`**

```go
package rabbitmq

// RabbitMQSubscriber implements queue.Subscriber.
type RabbitMQSubscriber struct { /* amqp091.Connection, channel, cfg */ }

// NewSubscriber dials RabbitMQ and returns a queue.Subscriber.
func NewSubscriber(cfg config.RabbitMQConfig) (queue.Subscriber, error)

// Subscribe opens a consumer channel and returns deliveries.
// Internally handles reconnect on connection loss (respects ctx).
// Adapts amqp091.Delivery → queue.Delivery (Body/Ack/Nack).
func (s *RabbitMQSubscriber) Subscribe(ctx context.Context) (<-chan queue.Delivery, error)

func (s *RabbitMQSubscriber) Name() string  // returns cfg.Queue
func (s *RabbitMQSubscriber) Close() error
```

**`internal/queue/rabbitmq/subscriber_test.go`** — unit tests with a mock AMQP connection.

Future providers follow the same pattern: `internal/queue/sqs/subscriber.go`, etc.

### 3b. Add dependency

```
go get github.com/rabbitmq/amqp091-go
```

### 3c. Config changes (`internal/config/config.go` + `defaults.go`)

Add a `QueueConfig` wrapper with a provider selector, nested under `MnemonicConfig`:

```go
type QueueConfig struct {
    Provider string        // MNEMONIC_QUEUE_PROVIDER (default "rabbitmq")
    RabbitMQ RabbitMQConfig
}

type RabbitMQConfig struct {
    Host           string        // MNEMONIC_QUEUE_RABBITMQ_HOST
    Port           int           // MNEMONIC_QUEUE_RABBITMQ_PORT           (default 5672)
    User           string        // MNEMONIC_QUEUE_RABBITMQ_USER
    Password       string        // MNEMONIC_QUEUE_RABBITMQ_PASSWORD
    VHost          string        // MNEMONIC_QUEUE_RABBITMQ_VHOST          (default "/")
    Queue          string        // MNEMONIC_QUEUE_RABBITMQ_QUEUE          (default "enrichment-jobs")
    PrefetchCount  int           // MNEMONIC_QUEUE_RABBITMQ_PREFETCH_COUNT (default = EnrichmentConfig.WorkerCount)
    ReconnectDelay time.Duration // MNEMONIC_QUEUE_RABBITMQ_RECONNECT_DELAY (default 5s)
}
```

Add `Queue QueueConfig` to `MnemonicConfig`. Add all defaults and viper bindings in
`internal/config/defaults.go`.

Also remove `EnrichmentConfig.PollInterval` — it becomes dead config once polling is gone.

### 3d. Update enrichment Service interface (`internal/service/enrichment/service.go`)

- **Remove** `ClaimNextJob(ctx context.Context) (*enrichmentjob.Job, error)`
- **Add** `GetJob(ctx context.Context, jobID uuid.UUID) (*enrichmentjob.Job, error)` — delegates to `jobRepo.Get(ctx, id)`
- **Add** `MarkJobProcessing(ctx context.Context, jobID uuid.UUID) error` — delegates to `jobRepo.MarkProcessing(ctx, id)`

Unchanged: `ProcessJob`, `ReclaimStaleJobs`, `CleanupCompletedJobs`, `CleanupFailedJobs`.

Rationale: queue single-delivery-per-consumer replaces `FOR UPDATE SKIP LOCKED`. The DB
still tracks status; the queue drives delivery.

### 3e. Refactor `internal/enricher/enrichment.go`

Replace the polling loop with a `queue.Subscriber` consumer. The Worker has zero AMQP
imports — all provider specifics live in `internal/queue/rabbitmq/`.

**Updated Worker struct:**

```go
type Worker struct {
    svc    enrichmentsvc.Service
    sub    queue.Subscriber
    cfg    config.EnrichmentConfig
    logger zerolog.Logger
}

func New(svc enrichmentsvc.Service, sub queue.Subscriber, cfg config.EnrichmentConfig, logger zerolog.Logger) *Worker
```

**Updated `Run` flow:**

1. Log `w.sub.Name()` at startup
2. Call `w.sub.Subscribe(ctx)` → receive `<-chan queue.Delivery`
3. Start maintenance goroutine (unchanged: reclaim stale, cleanup completed/failed)
4. Fan out deliveries to goroutines bounded by semaphore of size `cfg.WorkerCount`
5. On `ctx.Done()`: stop accepting new deliveries, drain in-flight within `cfg.DrainTimeout`, call `w.sub.Close()`

**`handleDelivery(ctx context.Context, d queue.Delivery)` flow:**

```
1. Unmarshal d.Body: {"job_id": "uuid"}
2. svc.GetJob(ctx, jobID)         — if not found: d.Nack(requeue=false), return
3. svc.MarkJobProcessing(ctx, jobID)
4. svc.ProcessJob(ctx, job)
5. unrecoverable error → d.Nack(requeue=false)
   success             → d.Ack()
```

Remove: `runWorker`, `sleep`, `cfg.PollInterval` references — none survive the refactor.

---

## Phase 4 — Simplify `internal/server/server.go`

Remove:

- All skill / agent / pattern / search service wiring
- `mcpserver` setup and `runMCPServer` goroutine
- MCP port from startup log line

Simplify `wireDependencies` to wire only enrichment dependencies:

- Repos: `agentRepo`, `patternRepo`, `chunkRepo`, `enrichmentJobRepo`, `graphRepo`
- Services: `embeddingSvc`, `extractionSvc`, `enrichmentSvc`
- Worker: `enrichWorker`
- Returns `(*enricher.Worker, error)` instead of `(Services, mcpserver.ToolDependencies, *enricher.Worker, error)`

Construct the `queue.Subscriber` in `wireDependencies` based on `cfg.Queue.Provider`:

```go
var sub queue.Subscriber
switch cfg.Queue.Provider {
case "rabbitmq":
    sub, err = rabbitmq.NewSubscriber(cfg.Queue.RabbitMQ)
default:
    return nil, fmt.Errorf("unknown queue provider: %s", cfg.Queue.Provider)
}
```

Pass `sub` into `enricher.New(enrichmentSvc, sub, cfg.Enrichment, logger)`.

Health checks (Postgres + Neo4j) and Admin HTTP server (operations routes only) remain.

---

## Phase 5 — Update docker-compose files

### `docker-compose.yaml` (dev)

Add:

```yaml
dev_rabbitmq:
  image: rabbitmq:4.0-management
  ports:
    - "5672:5672"
    - "15672:15672"
  environment:
    RABBITMQ_DEFAULT_USER: guest
    RABBITMQ_DEFAULT_PASS: guest
  networks:
    - dev_network
  healthcheck:
    test: ["CMD", "rabbitmq-diagnostics", "ping"]
    interval: 10s
    timeout: 5s
    retries: 5
```

Add `MNEMONIC_QUEUE_*` env vars to the enricher service. Add `dev_rabbitmq` to its
`depends_on`.

### `src/tests/docker-compose.yaml` (e2e)

Same RabbitMQ service addition scoped to `enrichment_network`.
Add `MNEMONIC_QUEUE_*` env vars to `e2e_enricher`.

---

## Phase 6 — Update GitHub workflow

File: `.github/workflows/mnemonic-enrichment-ci.yaml`

**Fix bug:** push trigger references `.github/workflows/mnemonic-ci.yaml` (wrong filename) →
change to `.github/workflows/mnemonic-enrichment-ci.yaml`.

No other changes needed — the workflow invokes `./build/build.sh` which handles unit tests;
e2e tests are not currently run in CI.

---

## Phase 7 — Cleanup remaining files

| File                               | Action                                                |
| ---------------------------------- | ----------------------------------------------------- |
| `src/tests/e2e/helpers/types.go`   | Remove types for agents, patterns, skills, skillfiles |
| `src/tests/e2e/helpers/helpers.go` | Remove helpers for deleted resource types             |
| `src/internal/handlers/doc.go`     | Update package doc (only operations remains)          |
| `src/internal/service/doc.go`      | Update package doc                                    |
| `src/internal/repository/doc.go`   | Update package doc                                    |
| `Makefile`                         | Remove targets for deleted components                 |

---

## Files to keep (enrichment core)

| Path                                         | Role                                       |
| -------------------------------------------- | ------------------------------------------ |
| `internal/enricher/enrichment.go`            | Worker (refactored; uses queue.Subscriber) |
| `internal/queue/queue.go`                    | Delivery struct + Subscriber interface     |
| `internal/queue/rabbitmq/subscriber.go`      | RabbitMQ implementation of Subscriber      |
| `internal/queue/rabbitmq/subscriber_test.go` | Unit tests (mock AMQP)                     |
| `internal/service/enrichment/service.go`     | Enrichment service (interface updated)     |
| `internal/service/openai/`                   | Embedding + concept extraction             |
| `internal/repository/enrichmentjob/`         | Job tracking, retry, observability         |
| `internal/repository/pattern/`               | Pattern loading + enrichment status        |
| `internal/repository/chunk/`                 | Chunk loading + embedding storage          |
| `internal/repository/agent/`                 | Agent name resolution for Neo4j            |
| `internal/repository/graph/`                 | Neo4j sync                                 |
| `internal/handlers/operations/`              | `/health` + `/version` endpoints           |
| `internal/health/`                           | Health check registry                      |
| `internal/version/`                          | Version info                               |
| `internal/config/`                           | Config (extended with QueueConfig)         |
| `internal/database/`                         | PG pool + Neo4j driver                     |
| `internal/metrics/`                          | Prometheus metrics                         |
| `internal/middleware/`                       | Gin middleware                             |
| `internal/telemetry/`                        | OTel setup                                 |
| `internal/server/server.go`                  | Startup/wiring (simplified)                |
| `cmd/main/main.go`                           | Entry point                                |
| `tests/e2e/api/operations_test.go`           | Health + version e2e tests                 |

---

## Verification

Run the full CI build from the project root:

```
make build
```

This invokes `src/build/build.sh`, which runs unit tests, builds the binary, builds the Docker
image, and runs e2e tests. All steps must pass with zero errors before the phase is considered
complete.
