package feesapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"encore.dev/beta/errs"

	"fees-api/internal/fees"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
)

var (
	temporalClientOnce sync.Once
	temporalClient     client.Client
	temporalClientErr  error
)

type CreateBillRequest struct {
	CustomerID     string `json:"customer_id"`
	PeriodStart    string `json:"period_start"`
	PeriodEnd      string `json:"period_end"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

//encore:api public method=POST path=/bills
func CreateBill(ctx context.Context, req *CreateBillRequest) (*fees.Bill, error) {
	periodStart, err := time.Parse(time.RFC3339, req.PeriodStart)
	if err != nil {
		return nil, apiError(errs.InvalidArgument, "period_start must be RFC3339")
	}

	periodEnd, err := time.Parse(time.RFC3339, req.PeriodEnd)
	if err != nil {
		return nil, apiError(errs.InvalidArgument, "period_end must be RFC3339")
	}

	idempotencyKey, err := normalizeIdempotencyKey(req.IdempotencyKey)
	if err != nil {
		return nil, domainError(err)
	}

	billID := fees.NewBillID()
	if idempotencyKey != "" {
		billID = deterministicResourceID("bill", req.CustomerID+":"+idempotencyKey)
	}
	now := time.Now().UTC()
	input := fees.BillWorkflowInput{
		BillID:      billID,
		CustomerID:  req.CustomerID,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		CreatedAt:   now,
	}
	if err := input.Validate(); err != nil {
		return nil, domainError(err)
	}

	c, err := getTemporalClient()
	if err != nil {
		return nil, domainError(err)
	}

	_, err = c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                                       billID,
		TaskQueue:                                fees.BillTaskQueue,
		WorkflowIDReusePolicy:                    enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
		WorkflowExecutionErrorWhenAlreadyStarted: idempotencyKey != "",
	}, fees.BillWorkflow, input)
	if err != nil {
		if idempotencyKey != "" && isTemporalAlreadyStarted(err) {
			return getExistingBillForIdempotency(ctx, c, billID, req, periodStart, periodEnd)
		}
		return nil, domainError(err)
	}

	return &fees.Bill{
		ID:          billID,
		CustomerID:  req.CustomerID,
		Status:      fees.BillStatusOpen,
		PeriodStart: periodStart.UTC(),
		PeriodEnd:   periodEnd.UTC(),
		LineItems:   []fees.LineItem{},
		CreatedAt:   now,
	}, nil
}

type AddLineItemRequest struct {
	Description    string `json:"description"`
	Currency       string `json:"currency"`
	AmountMinor    int64  `json:"amount_minor"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

//encore:api public method=POST path=/bills/:id/line-items
func AddLineItem(ctx context.Context, id string, req *AddLineItemRequest) (*fees.LineItem, error) {
	idempotencyKey, err := normalizeIdempotencyKey(req.IdempotencyKey)
	if err != nil {
		return nil, domainError(err)
	}

	item := fees.LineItem{
		ID:          fees.NewLineItemID(),
		BillID:      id,
		Description: req.Description,
		Amount: fees.Money{
			Currency: fees.Currency(strings.ToUpper(req.Currency)),
			Minor:    req.AmountMinor,
		},
		CreatedAt: time.Now().UTC(),
	}
	input := fees.AddLineItemWorkflowInput{
		Item:           item,
		IdempotencyKey: idempotencyKey,
	}
	if err := input.Validate(); err != nil {
		return nil, domainError(err)
	}

	c, err := getTemporalClient()
	if err != nil {
		return nil, domainError(err)
	}

	handle, err := c.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID:   id,
		UpdateID:     item.ID,
		UpdateName:   fees.AddLineItemUpdateName,
		Args:         []interface{}{input},
		WaitForStage: client.WorkflowUpdateStageCompleted,
	})
	if err != nil {
		if completedWorkflowExists(ctx, c, id) {
			return nil, domainError(fees.ErrBillClosed)
		}
		return nil, domainError(err)
	}

	var added fees.LineItem
	if err := handle.Get(ctx, &added); err != nil {
		return nil, domainError(err)
	}
	return &added, nil
}

//encore:api public method=POST path=/bills/:id/close
func CloseBill(ctx context.Context, id string) (*fees.Invoice, error) {
	c, err := getTemporalClient()
	if err != nil {
		return nil, domainError(err)
	}

	handle, err := c.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID:   id,
		UpdateID:     "close",
		UpdateName:   fees.CloseBillUpdateName,
		Args:         []interface{}{fees.CloseBillWorkflowInput{ClosedAt: time.Now().UTC()}},
		WaitForStage: client.WorkflowUpdateStageCompleted,
	})
	if err != nil {
		if isWorkflowCompleted(err) {
			return getCompletedInvoice(ctx, c, id)
		}
		return nil, domainError(err)
	}

	var invoice fees.Invoice
	if err := handle.Get(ctx, &invoice); err != nil {
		return nil, domainError(err)
	}
	return &invoice, nil
}

//encore:api public method=GET path=/bills/:id/invoice
func GetInvoice(ctx context.Context, id string) (*fees.Invoice, error) {
	c, err := getTemporalClient()
	if err != nil {
		return nil, domainError(err)
	}

	result, err := c.QueryWorkflow(ctx, id, "", fees.GetInvoiceQueryName)
	if err == nil {
		var invoice fees.Invoice
		if err := result.Get(&invoice); err != nil {
			return nil, domainError(err)
		}
		return &invoice, nil
	}
	if isTemporalNotFound(err) || isBillOpenError(err) {
		return nil, domainError(err)
	}

	return getCompletedInvoice(ctx, c, id)
}

//encore:api public method=GET path=/bills/:id
func GetBill(ctx context.Context, id string) (*fees.Bill, error) {
	c, err := getTemporalClient()
	if err != nil {
		return nil, domainError(err)
	}

	result, err := c.QueryWorkflow(ctx, id, "", fees.GetBillQueryName)
	if err != nil {
		return nil, domainError(err)
	}

	var bill fees.Bill
	if err := result.Get(&bill); err != nil {
		return nil, domainError(err)
	}
	return &bill, nil
}

func getTemporalClient() (client.Client, error) {
	temporalClientOnce.Do(func() {
		temporalClient, temporalClientErr = client.Dial(client.Options{})
	})
	return temporalClient, temporalClientErr
}

func domainError(err error) error {
	switch {
	case isTemporalNotFound(err):
		return apiError(errs.NotFound, fees.ErrNotFound.Error())
	case errors.Is(err, fees.ErrNotFound), strings.Contains(err.Error(), fees.ErrNotFound.Error()):
		return apiError(errs.NotFound, err.Error())
	case errors.Is(err, fees.ErrBillClosed), strings.Contains(err.Error(), fees.ErrBillClosed.Error()):
		return apiError(errs.FailedPrecondition, err.Error())
	case errors.Is(err, fees.ErrBillOpen), strings.Contains(err.Error(), fees.ErrBillOpen.Error()):
		return apiError(errs.FailedPrecondition, err.Error())
	case strings.Contains(err.Error(), "workflow execution already completed"):
		return apiError(errs.FailedPrecondition, fees.ErrBillClosed.Error())
	case errors.Is(err, fees.ErrInvalidInput), strings.Contains(err.Error(), fees.ErrInvalidInput.Error()):
		return apiError(errs.InvalidArgument, err.Error())
	default:
		return apiError(errs.Internal, err.Error())
	}
}

func apiError(code errs.ErrCode, message string) error {
	return &errs.Error{Code: code, Message: message}
}

func isTemporalNotFound(err error) bool {
	var notFound *serviceerror.NotFound
	return errors.As(err, &notFound) || strings.Contains(err.Error(), "workflow not found")
}

func isTemporalAlreadyStarted(err error) bool {
	var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
	return errors.As(err, &alreadyStarted)
}

func isBillOpenError(err error) bool {
	return errors.Is(err, fees.ErrBillOpen) || strings.Contains(err.Error(), fees.ErrBillOpen.Error())
}

func isWorkflowCompleted(err error) bool {
	return strings.Contains(err.Error(), "workflow execution already completed")
}

func getCompletedInvoice(ctx context.Context, c client.Client, id string) (*fees.Invoice, error) {
	var invoice fees.Invoice
	if err := c.GetWorkflow(ctx, id, "").Get(ctx, &invoice); err != nil {
		return nil, domainError(err)
	}
	return &invoice, nil
}

func completedWorkflowExists(ctx context.Context, c client.Client, id string) bool {
	var invoice fees.Invoice
	return c.GetWorkflow(ctx, id, "").Get(ctx, &invoice) == nil
}

func getExistingBillForIdempotency(ctx context.Context, c client.Client, billID string, req *CreateBillRequest, periodStart time.Time, periodEnd time.Time) (*fees.Bill, error) {
	result, err := c.QueryWorkflow(ctx, billID, "", fees.GetBillQueryName)
	if err != nil {
		return nil, domainError(err)
	}

	var bill fees.Bill
	if err := result.Get(&bill); err != nil {
		return nil, domainError(err)
	}
	if bill.CustomerID != req.CustomerID ||
		!bill.PeriodStart.Equal(periodStart.UTC()) ||
		!bill.PeriodEnd.Equal(periodEnd.UTC()) {
		return nil, apiError(errs.AlreadyExists, "idempotency_key was already used with different bill details")
	}
	return &bill, nil
}

func normalizeIdempotencyKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", nil
	}
	if len(key) > 128 {
		return "", fmt.Errorf("%w: idempotency_key must be 128 characters or fewer", fees.ErrInvalidInput)
	}
	return key, nil
}

func deterministicResourceID(prefix string, key string) string {
	sum := sha256.Sum256([]byte(prefix + ":" + key))
	return prefix + "_" + hex.EncodeToString(sum[:16])
}
