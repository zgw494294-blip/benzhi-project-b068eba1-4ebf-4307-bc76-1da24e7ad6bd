# SeedPool

SeedPool is a small Go HTTP JSON service for allocating limited seed-packet inventory among community-garden plots. A process keeps distribution rounds in memory. Each round opens with ordered inventory, accepts one request per plot, and becomes immutable after finalization.

## Run

Check that the HTTP service can start, answer a request, and stop cleanly:

```text
go run ./cmd/seedpool
```

Start the service on port 8080:

```text
go run ./cmd/seedpool -serve -addr :8080
```

Run the bounded end-to-end workflow:

```text
go run ./cmd/seedpool -smoke
```

## API

Create a round with a nonempty ordered inventory:

```text
POST /rounds
Content-Type: application/json

{"inventory":[{"variety":"bean","packets":4},{"variety":"lettuce","packets":2}]}
```

Append one request per plot while the round is open:

```text
POST /rounds/{id}/requests
Content-Type: application/json

{"plot_id":"plot-a","items":[{"variety":"bean","packets":2}],"max_packets":2}
```

`max_packets` is optional. If it is absent, the request has no total packet cap. Every item quantity and a present cap must be positive.

Finalize once and retrieve the immutable result:

```text
POST /rounds/{id}/finalize
GET /rounds/{id}
```

Allocation follows request order and item order. Each result is marked `fulfilled` when all requested packets were assigned, or `partial` otherwise. JSON bodies reject unknown fields, malformed input, and trailing values.

## Test

```text
go test ./...
```
