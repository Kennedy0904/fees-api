# Fees API

Go implementation of a fees billing API using Encore for the API layer and Temporal for the bill lifecycle workflow.

The main design choice is that each bill is represented by a long-running Temporal workflow. The workflow starts when the bill period begins, owns the open/closed state, accepts line-item updates while open, rejects late line items after close, and returns the final invoice summary.

## Prerequisites

- Go 1.24 or newer
- Encore CLI
- Temporal CLI

## Requirements Covered

- Create new bill: `POST /bills` starts a Temporal workflow for the billing period.
- Add line items: `POST /bills/{id}/line-items` accrues fees while the bill is open.
- Close active bill: `POST /bills/{id}/close` returns the final invoice with all line items and totals.
- State integrity: line-item additions after close are rejected with `failed_precondition`.
- Multi-currency: amounts support `USD` and `GEL`, represented in minor units and totaled per currency.
- Temporal workflow lifecycle: closing a bill completes the workflow, and `GET /bills/{id}/invoice` reads the completed workflow result.

## Run

Start Temporal:

```sh
temporal server start-dev
```

Start the worker in another terminal:

```sh
go run ./cmd/fees-worker
```

Start the Encore API in another terminal:

```sh
encore run
```

## Local URLs

- Fees API: `http://127.0.0.1:4000`
- Temporal UI: `http://localhost:8233`
- Encore local dashboard: printed by `encore run`, usually `http://127.0.0.1:9400/...`

In Temporal UI, open the `default` namespace and look for `BillWorkflow`. Open bills show as running workflows. After calling `POST /bills/{id}/close`, the workflow should move to completed and its result contains the final invoice.

You can also verify the workflow from the CLI:

```sh
temporal workflow describe --workflow-id {bill_id}
```

After close, the expected workflow status is `COMPLETED`.

## Test

```sh
go test ./...
go vet ./...
```

The tests cover the domain service and the Temporal bill workflow state machine.

## API Example

The examples use regular `curl` output so connection errors are visible. Add `-v` to any command for more request/response detail while debugging.

Create a bill and start its Temporal workflow:

```sh
curl -X POST http://127.0.0.1:4000/bills \
  -H 'content-type: application/json' \
  -d '{
    "customer_id": "customer_123",
    "period_start": "2026-07-01T00:00:00Z",
    "period_end": "2026-08-01T00:00:00Z"
  }'
```

Use the returned bill ID to add line items:

```sh
curl -X POST http://127.0.0.1:4000/bills/{bill_id}/line-items \
  -H 'content-type: application/json' \
  -d '{
    "description": "account maintenance fee",
    "currency": "USD",
    "amount_minor": 1200
  }'
```

```sh
curl -X POST http://127.0.0.1:4000/bills/{bill_id}/line-items \
  -H 'content-type: application/json' \
  -d '{
    "description": "card fee",
    "currency": "GEL",
    "amount_minor": 900
  }'
```

Get the active bill state before closing:

```sh
curl http://127.0.0.1:4000/bills/{bill_id}
```

Close the bill and receive the final invoice summary:

```sh
curl -X POST http://127.0.0.1:4000/bills/{bill_id}/close
```

Example invoice response:

```json
{
  "bill_id": "bill_123",
  "status": "closed",
  "totals": [
    {
      "currency": "GEL",
      "minor": 900
    },
    {
      "currency": "USD",
      "minor": 1200
    }
  ],
  "line_items": [
    {
      "id": "line_123",
      "bill_id": "bill_123",
      "description": "account maintenance fee",
      "amount": {
        "currency": "USD",
        "minor": 1200
      },
      "created_at": "2026-07-01T00:00:00Z"
    },
    {
      "id": "line_456",
      "bill_id": "bill_123",
      "description": "card fee",
      "amount": {
        "currency": "GEL",
        "minor": 900
      },
      "created_at": "2026-07-01T00:00:00Z"
    }
  ],
  "closed_at": "2026-08-01T00:00:00Z"
}
```

Fetch the finalized invoice after close:

```sh
curl http://127.0.0.1:4000/bills/{bill_id}/invoice
```

After close, the Temporal workflow completes and appears as completed in Temporal UI. Further line-item additions are rejected as closed-bill operations. Closed bills are not reopened; production correction flows should use explicit adjustments or credit/debit notes instead of mutating a finalized invoice.

## Edge Case Examples

Adding both `USD` and `GEL` to the same bill is allowed. The invoice keeps separate totals per currency instead of mixing exchange rates.

Unsupported currencies are rejected:

```sh
curl -X POST http://127.0.0.1:4000/bills/{bill_id}/line-items \
  -H 'content-type: application/json' \
  -d '{
    "description": "unsupported currency fee",
    "currency": "EUR",
    "amount_minor": 100
  }'
```

Non-positive amounts are rejected:

```sh
curl -X POST http://127.0.0.1:4000/bills/{bill_id}/line-items \
  -H 'content-type: application/json' \
  -d '{
    "description": "invalid amount fee",
    "currency": "USD",
    "amount_minor": 0
  }'
```

Line items are rejected after the bill is closed:

```sh
curl -X POST http://127.0.0.1:4000/bills/{bill_id}/line-items \
  -H 'content-type: application/json' \
  -d '{
    "description": "late fee",
    "currency": "USD",
    "amount_minor": 100
  }'
```

## Architecture

- `fees/api.go` exposes the REST API with Encore endpoint annotations.
- `internal/fees/workflow.go` contains the Temporal bill workflow.
- `cmd/fees-worker/main.go` runs the Temporal worker for the `fees-bills` task queue.
- `internal/fees/model.go` contains the financial data model.
- `internal/fees/service.go` and `internal/fees/store.go` keep a small pure-Go domain service and in-memory store used by tests and as a contrast to the Temporal-backed API path.

Temporal is used as the bill state machine. `POST /bills` starts the workflow, `POST /bills/{id}/line-items` sends a synchronous workflow update, `GET /bills/{id}` queries the active workflow state, `POST /bills/{id}/close` sends a close update, returns the final invoice, and completes the workflow, and `GET /bills/{id}/invoice` reads the completed workflow result.

Using workflow updates instead of fire-and-forget signals lets the API return validation errors synchronously. This is important for financial state integrity because adding a line item to a closed bill must fail at the API boundary.

## Money And State

- Money is represented in minor units with `int64`, not floating point values.
- Supported currencies are `USD` and `GEL`.
- Invoice totals are grouped by currency.
- Bills move from `open` to `closed`.
- Closing is idempotent.
- Closed bills reject new line items.

## Tradeoffs

For this implementation, Temporal workflow history is the source of truth for bill lifecycle state. That keeps the required Encore + Temporal flow focused and avoids requiring Postgres/Docker setup for reviewers.

In production, I would usually add a database-backed read model for listing/searching bills, reporting across many bills, and operational recovery workflows. The write-side state transition rules would still fit well in Temporal.
