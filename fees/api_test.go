package feesapi

import (
	"errors"
	"testing"

	"encore.dev/beta/errs"

	"fees-api/internal/fees"

	"go.temporal.io/api/serviceerror"
)

func TestNormalizeIdempotencyKey(t *testing.T) {
	key, err := normalizeIdempotencyKey(" retry-123 ")
	if err != nil {
		t.Fatal(err)
	}
	if key != "retry-123" {
		t.Fatalf("unexpected key: %q", key)
	}

	_, err = normalizeIdempotencyKey("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	if !errors.Is(err, fees.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for long key, got %v", err)
	}
}

func TestDeterministicResourceID(t *testing.T) {
	first := deterministicResourceID("line", "bill_123:retry-123")
	second := deterministicResourceID("line", "bill_123:retry-123")
	other := deterministicResourceID("line", "bill_123:retry-456")

	if first != second {
		t.Fatalf("expected same key to produce same id, first=%s second=%s", first, second)
	}
	if first == other {
		t.Fatalf("expected different keys to produce different ids, got %s", first)
	}
	if len(first) != len("line_00000000000000000000000000000000") {
		t.Fatalf("unexpected deterministic id length: %s", first)
	}
}

func TestCreateBillIdempotencyIDIsCustomerScoped(t *testing.T) {
	firstCustomer := deterministicResourceID("bill", "customer_123:2026-07")
	secondCustomer := deterministicResourceID("bill", "customer_456:2026-07")

	if firstCustomer == secondCustomer {
		t.Fatalf("expected different customers to have different deterministic bill ids, got %s", firstCustomer)
	}
}

func TestDomainErrorMapsTemporalNotFound(t *testing.T) {
	err := domainError(serviceerror.NewNotFound("workflow not found"))

	var apiErr *errs.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected Encore error, got %T", err)
	}
	if apiErr.Code != errs.NotFound {
		t.Fatalf("expected not_found, got %s", apiErr.Code)
	}
}

func TestIsWorkflowCompleted(t *testing.T) {
	if !isWorkflowCompleted(errors.New("workflow execution already completed")) {
		t.Fatal("expected completed workflow error to be detected")
	}
	if isWorkflowCompleted(errors.New("workflow not found")) {
		t.Fatal("did not expect unrelated error to be detected as completed workflow")
	}
}
