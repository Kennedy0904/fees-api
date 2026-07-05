package fees

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCloseBillSummarizesTotalsByCurrency(t *testing.T) {
	service := NewService(NewStore())
	service.now = func() time.Time { return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) }

	bill, err := service.CreateBill(context.Background(), CreateBillInput{
		CustomerID:  "customer_123",
		PeriodStart: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, _ = service.AddLineItem(context.Background(), AddLineItemInput{
		BillID:      bill.ID,
		Description: "account fee",
		Money:       Money{Currency: CurrencyUSD, Minor: 1200},
	})
	_, _ = service.AddLineItem(context.Background(), AddLineItemInput{
		BillID:      bill.ID,
		Description: "card fee",
		Money:       Money{Currency: CurrencyGEL, Minor: 900},
	})
	_, _ = service.AddLineItem(context.Background(), AddLineItemInput{
		BillID:      bill.ID,
		Description: "transfer fee",
		Money:       Money{Currency: CurrencyUSD, Minor: 300},
	})

	invoice, err := service.CloseBill(context.Background(), bill.ID)
	if err != nil {
		t.Fatal(err)
	}

	if invoice.Status != BillStatusClosed {
		t.Fatalf("expected closed invoice, got %s", invoice.Status)
	}
	if len(invoice.Totals) != 2 {
		t.Fatalf("expected 2 currency totals, got %d", len(invoice.Totals))
	}
	if invoice.Totals[0] != (Money{Currency: CurrencyGEL, Minor: 900}) {
		t.Fatalf("unexpected GEL total: %+v", invoice.Totals[0])
	}
	if invoice.Totals[1] != (Money{Currency: CurrencyUSD, Minor: 1500}) {
		t.Fatalf("unexpected USD total: %+v", invoice.Totals[1])
	}
}

func TestCannotAddLineItemAfterBillClosed(t *testing.T) {
	service := NewService(NewStore())
	bill, err := service.CreateBill(context.Background(), CreateBillInput{
		CustomerID:  "customer_123",
		PeriodStart: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.CloseBill(context.Background(), bill.ID); err != nil {
		t.Fatal(err)
	}

	_, err = service.AddLineItem(context.Background(), AddLineItemInput{
		BillID:      bill.ID,
		Description: "late fee",
		Money:       Money{Currency: CurrencyUSD, Minor: 100},
	})
	if !errors.Is(err, ErrBillClosed) {
		t.Fatalf("expected ErrBillClosed, got %v", err)
	}
}

func TestCreateBillValidatesInput(t *testing.T) {
	service := NewService(NewStore())

	_, err := service.CreateBill(context.Background(), CreateBillInput{
		PeriodStart: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for missing customer, got %v", err)
	}

	_, err = service.CreateBill(context.Background(), CreateBillInput{
		CustomerID:  "customer_123",
		PeriodStart: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for invalid period, got %v", err)
	}
}

func TestAddLineItemValidatesInput(t *testing.T) {
	service := NewService(NewStore())

	tests := []struct {
		name  string
		input AddLineItemInput
	}{
		{
			name: "missing bill id",
			input: AddLineItemInput{
				Description: "account fee",
				Money:       Money{Currency: CurrencyUSD, Minor: 100},
			},
		},
		{
			name: "missing description",
			input: AddLineItemInput{
				BillID: "bill_123",
				Money:  Money{Currency: CurrencyUSD, Minor: 100},
			},
		},
		{
			name: "non-positive amount",
			input: AddLineItemInput{
				BillID:      "bill_123",
				Description: "account fee",
				Money:       Money{Currency: CurrencyUSD, Minor: 0},
			},
		},
		{
			name: "unsupported currency",
			input: AddLineItemInput{
				BillID:      "bill_123",
				Description: "account fee",
				Money:       Money{Currency: Currency("EUR"), Minor: 100},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.AddLineItem(context.Background(), tt.input)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("expected ErrInvalidInput, got %v", err)
			}
		})
	}
}

func TestCloseBillIsIdempotent(t *testing.T) {
	service := NewService(NewStore())
	service.now = func() time.Time { return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) }

	bill, err := service.CreateBill(context.Background(), CreateBillInput{
		CustomerID:  "customer_123",
		PeriodStart: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	first, err := service.CloseBill(context.Background(), bill.ID)
	if err != nil {
		t.Fatal(err)
	}

	service.now = func() time.Time { return time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC) }
	second, err := service.CloseBill(context.Background(), bill.ID)
	if err != nil {
		t.Fatal(err)
	}

	if !first.ClosedAt.Equal(second.ClosedAt) {
		t.Fatalf("expected close time to stay unchanged, first=%s second=%s", first.ClosedAt, second.ClosedAt)
	}
}

func TestStoreReturnsDefensiveCopies(t *testing.T) {
	service := NewService(NewStore())
	bill, err := service.CreateBill(context.Background(), CreateBillInput{
		CustomerID:  "customer_123",
		PeriodStart: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.AddLineItem(context.Background(), AddLineItemInput{
		BillID:      bill.ID,
		Description: "account fee",
		Money:       Money{Currency: CurrencyUSD, Minor: 100},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := service.GetBill(context.Background(), bill.ID)
	if err != nil {
		t.Fatal(err)
	}
	got.LineItems[0].Description = "mutated outside store"

	again, err := service.GetBill(context.Background(), bill.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.LineItems[0].Description != "account fee" {
		t.Fatalf("store leaked mutable line item slice: %+v", again.LineItems[0])
	}
}
