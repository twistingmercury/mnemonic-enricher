# Build Pipeline Diagram

This diagram shows how `src/mnemonic/build/build.sh` drives both local builds and CI builds.
For a reusable org-level reference example, see [Docker-First CI/CD Reference Example](../../_draft-skills/docker-first-ci-cd-implementation/references/docker-first-ci-cd-diagram.md).

```mermaid
flowchart TD
    A["Build requested"] --> K{"Context?"}
    K -->|Local run| L["Run ./build/build.sh locally
    same build + quality gates + unit tests + E2E"]
    L --> L1["Done"]
    K -->|CI run| B

    subgraph CI0[CI workflow]
    B["Run ./build/build.sh"] --> B1["Resolve build metadata
    BUILD_VER / BUILD_DATE / BUILD_COMMIT"]
    subgraph C0[Docker build container stage]
    C["Build Docker image
    ghcr.io/twistingmercury/mnemonic:latest"] --> C1["Quality gates in container
    goimports / golangci-lint / govulncheck / gosec"]
    C1 --> C2["Unit tests in container
    go test ./internal/..."]
    C2 --> C3["Compile mnemonic binary
    embed BUILD_VER / BUILD_DATE / BUILD_COMMIT"]
    end
    B1 --> C

    subgraph D0[E2E test stage]
    E["docker compose up -d postgres neo4j"]
    F["docker compose run --rm migrate"]
    G["docker compose up --abort-on-container-exit
    --exit-code-from mnemonic_tests
    mnemonic_api mnemonic_tests"]
    E --> F
    F --> G
    end

    C3 --> E
    G --> H{"Tests pass?"}
    H -->|No| I["build.sh exits non-zero
    CI job fails"]
    H -->|Yes| M["Save image artifact
    mnemonic-image.tar"]
    end

    M --> N{"CD gate:
    upstream CI event is push?"}
    N -->|No PR| Q["Stop: CD job skipped"]

    subgraph CD0[CD workflow]
    O["Download mnemonic-image.tar"]
    P["docker load image"]
    R["Push tags to GHCR
    latest on main + branch tag"]
    O --> P
    P --> R
    end

    N -->|Yes push| O
```
