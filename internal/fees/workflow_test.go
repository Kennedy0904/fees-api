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
	var invoice Invoice
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

	env.RegisterDelayedCallback(func() {
		value, err := env.QueryWorkflow(GetInvoiceQueryName)
		if err != nil {
			t.Fatal(err)
		}
		if err := value.Get(&invoice); err != nil {
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
		env.CancelWorkflow()
	}, 2*time.Second)

	env.ExecuteWorkflow(BillWorkflow, BillWorkflowInput{
		BillID:      billID,
		CustomerID:  "customer_123",
		PeriodStart: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		CreatedAt:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("expected workflow to finish after test cancellation")
	}
}

func TestBillWorkflowRejectsLineItemAfterClose(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(BillWorkflow)

	billID := "bill_test"
	var completedErr error
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflowNoRejection(CloseBillUpdateName, "close", t, CloseBillWorkflowInput{
			ClosedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		})
	}, time.Second)

	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow(AddLineItemUpdateName, "line_late", &testsuite.TestUpdateCallback{
			OnReject: func(err error) {
				t.Fatalf("late line item should be accepted by protocol and fail in handler: %v", err)
			},
			OnAccept: func() {},
			OnComplete: func(_ interface{}, err error) {
				completedErr = err
			},
		}, AddLineItemWorkflowInput{
			Item: LineItem{
				ID:          "line_late",
				BillID:      billID,
				Description: "late fee",
				Amount:      Money{Currency: CurrencyUSD, Minor: 100},
				CreatedAt:   time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC),
			},
		})
	}, 2*time.Second)

	env.RegisterDelayedCallback(func() {
		if !errors.Is(completedErr, ErrBillClosed) {
			t.Fatalf("expected ErrBillClosed, got %v", completedErr)
		}
		env.CancelWorkflow()
	}, 3*time.Second)

	env.ExecuteWorkflow(BillWorkflow, BillWorkflowInput{
		BillID:      billID,
		CustomerID:  "customer_123",
		PeriodStart: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		CreatedAt:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("expected workflow to finish after test cancellation")
	}
}
