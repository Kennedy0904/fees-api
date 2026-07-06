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
    "period_end": "2026-08-01T00:00:00Z",
    "idempotency_key": "create-bill-customer-123-2026-07"
  }'
```

Use the returned bill ID to add line items:

```sh
curl -X POST http://127.0.0.1:4000/bills/{bill_id}/line-items \
  -H 'content-type: application/json' \
  -d '{
    "description": "account maintenance fee",
    "currency": "USD",
    "amount_minor": 1200,
    "idempotency_key": "line-item-account-fee-2026-07"
  }'
```

```sh
curl -X POST http://127.0.0.1:4000/bills/{bill_id}/line-items \
  -H 'content-type: application/json' \
  -d '{
    "description": "card fee",
    "currency": "GEL",
    "amount_minor": 900,
    "idempotency_key": "line-item-card-fee-2026-07"
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

`idempotency_key` is optional on create-bill and add-line-item requests. When provided, it is used to derive stable Temporal workflow/update IDs so a client retry does not create a duplicate bill or append the same fee twice. A production system would also persist request fingerprints and original responses in a database, as described below.

## Money And State

- Money is represented in minor units with `int64`, not floating point values.
- Supported currencies are `USD` and `GEL`.
- Invoice totals are grouped by currency.
- Invoice total calculation rejects `int64` overflow instead of wrapping.
- Bills move from `open` to `closed`.
- Closing is idempotent.
- Closed bills reject new line items.

## Tradeoffs

For this implementation, Temporal workflow history is the source of truth for bill lifecycle state. That keeps the required Encore + Temporal flow focused and avoids requiring Postgres/Docker setup for reviewers.

In production, I would usually add a database-backed read model for listing/searching bills, reporting across many bills, and operational recovery workflows. The write-side state transition rules would still fit well in Temporal.

## Future Improvements

Persistence and query model:

- Add Postgres as a read model while keeping Temporal as the write-side state machine. The workflow would still decide whether a bill is open or closed, but activities would persist bill snapshots, line items, and finalized invoices into tables such as `bills`, `line_items`, `invoices`, and `invoice_totals`.
- Add unique constraints around financial identifiers: `bills.id`, `line_items.id`, `invoices.bill_id`, and idempotency keys. That gives the database a second layer of protection against duplicate writes if clients or workers retry.
- Use Postgres for read-heavy endpoints like `GET /bills`, `GET /invoices`, customer billing history, reporting, and dashboards. This avoids scanning Temporal history for operational queries.

Idempotent request handling:

- The current implementation accepts an explicit `idempotency_key` request field for create bill and add line item. It uses the key to derive stable Temporal workflow/update IDs, which protects against duplicate operations during client retries.
- Store records in an `idempotency_keys` table with scope, key, request fingerprint, status, response body or resource ID, and timestamps. A useful scope would be `customer_id + endpoint + key`, so different customers can safely use the same key value.
- On request start, insert the idempotency record inside a transaction. If insert succeeds, process the request. If the key already exists, compare the request fingerprint: same fingerprint returns the original result, different fingerprint returns a conflict.
- Use database unique constraints and Temporal IDs together. For example, create-bill can use the idempotency record to return the original bill ID and response, while add-line-item can store the resulting line item ID and avoid appending the same fee twice.
- Mark in-progress idempotency records carefully. If the API crashes after starting a Temporal workflow but before storing the final response, a retry should look up the workflow/resource by the stored operation reference and complete the response instead of creating a second operation.

Security and access control:

- Add an Encore auth handler to validate bearer tokens or service credentials and place the caller identity into request context.
- Add ownership checks before bill reads or mutations. For example, a customer caller can only access bills belonging to its customer ID, while an internal service caller may need a narrower service permission such as `fees:write` or `fees:read`.
- Store customer/account ownership in the database read model so authorization does not depend on querying a completed Temporal workflow.

Financial integrity:

- Add FX support only if a single reporting currency is required. That would need a rate table containing source, currency pair, rate, effective timestamp, and rounding mode. Invoice generation should record the exact rate used so later rate changes do not rewrite history.
- Add explicit adjustment, credit-note, or debit-note workflows for corrections instead of reopening finalized invoices. This keeps the original invoice immutable and creates an auditable correction trail.
- Add stronger validation rules as the domain grows, such as line-item category validation, maximum amount controls, and period-bound checks to reject fees outside the bill period.

Operations:

- Add Temporal search attributes such as customer ID, bill status, currency, and period end so operators can filter workflows in Temporal UI without opening each workflow manually.
- Add reconciliation jobs that compare finalized workflow invoices against the database read model and any downstream ledger/accounting entries. Differences should produce alerts or a repair workflow rather than silent drift.
- Add metrics and alerts for failed workflow updates, rejected line items, invoice close failures, and retry volume. These are the signals that usually reveal distributed-system or integration problems.
- Add workflow archival and retention planning. Completed workflows should remain inspectable long enough for audits, while finalized invoices should live in durable storage beyond Temporal retention.
