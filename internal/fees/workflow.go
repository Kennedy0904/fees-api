package fees

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/workflow"
)

const (
	BillTaskQueue = "fees-bills"

	BillWorkflowName = "BillWorkflow"

	AddLineItemUpdateName = "AddLineItem"
	CloseBillUpdateName   = "CloseBill"

	GetBillQueryName    = "GetBill"
	GetInvoiceQueryName = "GetInvoice"
)

type BillWorkflowInput struct {
	BillID      string
	CustomerID  string
	PeriodStart time.Time
	PeriodEnd   time.Time
	CreatedAt   time.Time
}

func (input BillWorkflowInput) Validate() error {
	if input.BillID == "" {
		return fmt.Errorf("%w: bill_id is required", ErrInvalidInput)
	}
	if input.CustomerID == "" {
		return fmt.Errorf("%w: customer_id is required", ErrInvalidInput)
	}
	if !input.PeriodEnd.After(input.PeriodStart) {
		return fmt.Errorf("%w: period_end must be after period_start", ErrInvalidInput)
	}
	return nil
}

type AddLineItemWorkflowInput struct {
	Item LineItem
}

func (input AddLineItemWorkflowInput) Validate() error {
	if input.Item.ID == "" {
		return fmt.Errorf("%w: line_item_id is required", ErrInvalidInput)
	}
	if input.Item.BillID == "" {
		return fmt.Errorf("%w: bill_id is required", ErrInvalidInput)
	}
	if input.Item.Description == "" {
		return fmt.Errorf("%w: description is required", ErrInvalidInput)
	}
	return validateMoney(input.Item.Amount)
}

type CloseBillWorkflowInput struct {
	ClosedAt time.Time
}

func BillWorkflow(ctx workflow.Context, input BillWorkflowInput) (Invoice, error) {
	if err := input.Validate(); err != nil {
		return Invoice{}, err
	}

	state := Bill{
		ID:          input.BillID,
		CustomerID:  input.CustomerID,
		Status:      BillStatusOpen,
		PeriodStart: input.PeriodStart.UTC(),
		PeriodEnd:   input.PeriodEnd.UTC(),
		LineItems:   []LineItem{},
		CreatedAt:   input.CreatedAt.UTC(),
	}

	if err := workflow.SetQueryHandler(ctx, GetBillQueryName, func() (Bill, error) {
		return cloneBill(state), nil
	}); err != nil {
		return Invoice{}, err
	}

	if err := workflow.SetQueryHandler(ctx, GetInvoiceQueryName, func() (Invoice, error) {
		if state.Status != BillStatusClosed {
			return Invoice{}, ErrBillOpen
		}
		return invoiceFromBill(state), nil
	}); err != nil {
		return Invoice{}, err
	}

	if err := workflow.SetUpdateHandler(ctx, AddLineItemUpdateName, func(ctx workflow.Context, input AddLineItemWorkflowInput) (LineItem, error) {
		if err := input.Validate(); err != nil {
			return LineItem{}, err
		}
		if input.Item.BillID != state.ID {
			return LineItem{}, ErrNotFound
		}
		if state.Status == BillStatusClosed {
			return LineItem{}, ErrBillClosed
		}

		item := input.Item
		item.CreatedAt = item.CreatedAt.UTC()
		state.LineItems = append(state.LineItems, item)
		return item, nil
	}); err != nil {
		return Invoice{}, err
	}

	var finalInvoice Invoice
	closed := false
	if err := workflow.SetUpdateHandler(ctx, CloseBillUpdateName, func(ctx workflow.Context, input CloseBillWorkflowInput) (Invoice, error) {
		if state.Status == BillStatusClosed {
			return invoiceFromBill(state), nil
		}

		closedAt := input.ClosedAt.UTC()
		state.Status = BillStatusClosed
		state.ClosedAt = &closedAt
		finalInvoice = invoiceFromBill(state)
		closed = true
		return finalInvoice, nil
	}); err != nil {
		return Invoice{}, err
	}

	if err := workflow.Await(ctx, func() bool { return closed }); err != nil {
		return Invoice{}, err
	}
	return finalInvoice, nil
}
