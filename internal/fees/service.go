package fees

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
)

var (
	ErrInvalidInput = errors.New("invalid input")
	ErrNotFound     = errors.New("bill not found")
	ErrBillClosed   = errors.New("bill is closed")
	ErrBillOpen     = errors.New("bill is still open")
)

type Service struct {
	store BillStore
	now   func() time.Time
}

type BillStore interface {
	CreateBill(context.Context, Bill) (Bill, error)
	GetBill(context.Context, string) (Bill, error)
	AddLineItem(context.Context, string, LineItem) (LineItem, error)
	CloseBill(context.Context, string, time.Time) (Bill, error)
}

func NewService(store BillStore) *Service {
	return &Service{
		store: store,
		now:   time.Now,
	}
}

type CreateBillInput struct {
	CustomerID  string
	PeriodStart time.Time
	PeriodEnd   time.Time
}

func (s *Service) CreateBill(ctx context.Context, input CreateBillInput) (Bill, error) {
	if input.CustomerID == "" {
		return Bill{}, fmt.Errorf("%w: customer_id is required", ErrInvalidInput)
	}
	if !input.PeriodEnd.After(input.PeriodStart) {
		return Bill{}, fmt.Errorf("%w: period_end must be after period_start", ErrInvalidInput)
	}

	now := s.now().UTC()
	bill := Bill{
		ID:          NewBillID(),
		CustomerID:  input.CustomerID,
		Status:      BillStatusOpen,
		PeriodStart: input.PeriodStart.UTC(),
		PeriodEnd:   input.PeriodEnd.UTC(),
		LineItems:   []LineItem{},
		CreatedAt:   now,
	}

	return s.store.CreateBill(ctx, bill)
}

type AddLineItemInput struct {
	BillID      string
	Description string
	Money       Money
}

func (s *Service) AddLineItem(ctx context.Context, input AddLineItemInput) (LineItem, error) {
	if input.BillID == "" {
		return LineItem{}, fmt.Errorf("%w: bill_id is required", ErrInvalidInput)
	}
	if input.Description == "" {
		return LineItem{}, fmt.Errorf("%w: description is required", ErrInvalidInput)
	}
	if err := validateMoney(input.Money); err != nil {
		return LineItem{}, err
	}

	item := LineItem{
		ID:          NewLineItemID(),
		BillID:      input.BillID,
		Description: input.Description,
		Amount:      input.Money,
		CreatedAt:   s.now().UTC(),
	}

	return s.store.AddLineItem(ctx, input.BillID, item)
}

func (s *Service) CloseBill(ctx context.Context, billID string) (Invoice, error) {
	if billID == "" {
		return Invoice{}, fmt.Errorf("%w: bill_id is required", ErrInvalidInput)
	}

	bill, err := s.store.CloseBill(ctx, billID, s.now().UTC())
	if err != nil {
		return Invoice{}, err
	}

	return invoiceFromBill(bill), nil
}

func (s *Service) GetBill(ctx context.Context, billID string) (Bill, error) {
	if billID == "" {
		return Bill{}, fmt.Errorf("%w: bill_id is required", ErrInvalidInput)
	}
	return s.store.GetBill(ctx, billID)
}

func validateMoney(money Money) error {
	if money.Minor <= 0 {
		return fmt.Errorf("%w: amount_minor must be positive", ErrInvalidInput)
	}
	if money.Currency != CurrencyUSD && money.Currency != CurrencyGEL {
		return fmt.Errorf("%w: currency must be USD or GEL", ErrInvalidInput)
	}
	return nil
}

func invoiceFromBill(bill Bill) Invoice {
	totalsByCurrency := make(map[Currency]int64)
	for _, item := range bill.LineItems {
		totalsByCurrency[item.Amount.Currency] += item.Amount.Minor
	}

	totals := make([]Money, 0, len(totalsByCurrency))
	for currency, minor := range totalsByCurrency {
		totals = append(totals, Money{Currency: currency, Minor: minor})
	}
	slices.SortFunc(totals, func(a, b Money) int {
		return cmpString(string(a.Currency), string(b.Currency))
	})

	closedAt := bill.CreatedAt
	if bill.ClosedAt != nil {
		closedAt = *bill.ClosedAt
	}

	return Invoice{
		BillID:    bill.ID,
		Status:    bill.Status,
		Totals:    totals,
		LineItems: bill.LineItems,
		ClosedAt:  closedAt,
	}
}

func cmpString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
