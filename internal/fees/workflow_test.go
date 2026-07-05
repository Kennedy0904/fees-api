package fees

import (
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
