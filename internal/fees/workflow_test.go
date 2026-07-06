package fees

import (
	"errors"
	"testing"
	"time"

	"go.temporal.io/sdk/testsuite"
)

func TestBillWorkflowAccruesAndClosesInvoice(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(BillWorkflow)

	billID := "bill_test"
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflowNoRejection(AddLineItemUpdateName, "line_1", t, AddLineItemWorkflowInput{
			Item: LineItem{
				ID:          "line_1",
				BillID:      billID,
				Description: "account fee",
				Amount:      Money{Currency: CurrencyUSD, Minor: 1200},
				CreatedAt:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			},
		})
		env.UpdateWorkflowNoRejection(AddLineItemUpdateName, "line_2", t, AddLineItemWorkflowInput{
			Item: LineItem{
				ID:          "line_2",
				BillID:      billID,
				Description: "card fee",
				Amount:      Money{Currency: CurrencyGEL, Minor: 900},
				CreatedAt:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			},
		})
		env.UpdateWorkflowNoRejection(AddLineItemUpdateName, "line_3", t, AddLineItemWorkflowInput{
			Item: LineItem{
				ID:          "line_3",
				BillID:      billID,
				Description: "transfer fee",
				Amount:      Money{Currency: CurrencyUSD, Minor: 300},
				CreatedAt:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			},
		})
		env.UpdateWorkflowNoRejection(CloseBillUpdateName, "close", t, CloseBillWorkflowInput{
			ClosedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		})
	}, time.Second)

	env.ExecuteWorkflow(BillWorkflow, BillWorkflowInput{
		BillID:      billID,
		CustomerID:  "customer_123",
		PeriodStart: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		CreatedAt:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("expected workflow to complete after close")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}

	var invoice Invoice
	if err := env.GetWorkflowResult(&invoice); err != nil {
		t.Fatal(err)
	}
	if invoice.Status != BillStatusClosed {
		t.Fatalf("expected closed invoice, got %s", invoice.Status)
	}
	if invoice.Totals[0] != (Money{Currency: CurrencyGEL, Minor: 900}) {
		t.Fatalf("unexpected GEL total: %+v", invoice.Totals[0])
	}
	if invoice.Totals[1] != (Money{Currency: CurrencyUSD, Minor: 1500}) {
		t.Fatalf("unexpected USD total: %+v", invoice.Totals[1])
	}
	if len(invoice.LineItems) != 3 {
		t.Fatalf("expected 3 line items, got %d", len(invoice.LineItems))
	}
}

func TestBillWorkflowInvoiceQueryFailsWhileOpen(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(BillWorkflow)

	env.RegisterDelayedCallback(func() {
		_, err := env.QueryWorkflow(GetInvoiceQueryName)
		if !errors.Is(err, ErrBillOpen) {
			t.Fatalf("expected ErrBillOpen, got %v", err)
		}
		env.UpdateWorkflowNoRejection(CloseBillUpdateName, "close", t, CloseBillWorkflowInput{
			ClosedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		})
	}, time.Second)

	env.ExecuteWorkflow(BillWorkflow, BillWorkflowInput{
		BillID:      "bill_test",
		CustomerID:  "customer_123",
		PeriodStart: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		CreatedAt:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
}

func TestBillWorkflowLineItemIdempotencyReturnsOriginalItem(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(BillWorkflow)

	billID := "bill_test"
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflowNoRejection(AddLineItemUpdateName, "update_1", t, AddLineItemWorkflowInput{
			IdempotencyKey: "monthly-fee",
			Item: LineItem{
				ID:          "line_original",
				BillID:      billID,
				Description: "account fee",
				Amount:      Money{Currency: CurrencyUSD, Minor: 1200},
				CreatedAt:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			},
		})
		env.UpdateWorkflowNoRejection(AddLineItemUpdateName, "update_2", t, AddLineItemWorkflowInput{
			IdempotencyKey: "monthly-fee",
			Item: LineItem{
				ID:          "line_retry",
				BillID:      billID,
				Description: "account fee",
				Amount:      Money{Currency: CurrencyUSD, Minor: 1200},
				CreatedAt:   time.Date(2026, 7, 1, 0, 0, 1, 0, time.UTC),
			},
		})
		env.UpdateWorkflowNoRejection(CloseBillUpdateName, "close", t, CloseBillWorkflowInput{
			ClosedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		})
	}, time.Second)

	env.ExecuteWorkflow(BillWorkflow, BillWorkflowInput{
		BillID:      billID,
		CustomerID:  "customer_123",
		PeriodStart: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		CreatedAt:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}

	var invoice Invoice
	if err := env.GetWorkflowResult(&invoice); err != nil {
		t.Fatal(err)
	}
	if len(invoice.LineItems) != 1 {
		t.Fatalf("expected retry to return original line item without appending, got %d items", len(invoice.LineItems))
	}
	if invoice.LineItems[0].ID != "line_original" {
		t.Fatalf("expected original line item id, got %s", invoice.LineItems[0].ID)
	}
}

func TestBillWorkflowLineItemIdempotencyRejectsDifferentPayload(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(BillWorkflow)

	billID := "bill_test"
	var duplicateErr error
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflowNoRejection(AddLineItemUpdateName, "update_1", t, AddLineItemWorkflowInput{
			IdempotencyKey: "monthly-fee",
			Item: LineItem{
				ID:          "line_original",
				BillID:      billID,
				Description: "account fee",
				Amount:      Money{Currency: CurrencyUSD, Minor: 1200},
				CreatedAt:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			},
		})
		env.UpdateWorkflow(AddLineItemUpdateName, "update_2", &testsuite.TestUpdateCallback{
			OnReject: func(err error) {
				t.Fatalf("update should be accepted by protocol and fail in handler: %v", err)
			},
			OnAccept: func() {},
			OnComplete: func(_ interface{}, err error) {
				duplicateErr = err
			},
		}, AddLineItemWorkflowInput{
			IdempotencyKey: "monthly-fee",
			Item: LineItem{
				ID:          "line_retry",
				BillID:      billID,
				Description: "different fee",
				Amount:      Money{Currency: CurrencyUSD, Minor: 1200},
				CreatedAt:   time.Date(2026, 7, 1, 0, 0, 1, 0, time.UTC),
			},
		})
		env.UpdateWorkflowNoRejection(CloseBillUpdateName, "close", t, CloseBillWorkflowInput{
			ClosedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		})
	}, time.Second)

	env.ExecuteWorkflow(BillWorkflow, BillWorkflowInput{
		BillID:      billID,
		CustomerID:  "customer_123",
		PeriodStart: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		CreatedAt:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(duplicateErr, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for same idempotency key with different payload, got %v", duplicateErr)
	}
}
