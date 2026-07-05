package fees

import "time"

type Currency string

const (
	CurrencyUSD Currency = "USD"
	CurrencyGEL Currency = "GEL"
)

type BillStatus string

const (
	BillStatusOpen   BillStatus = "open"
	BillStatusClosed BillStatus = "closed"
)

type Money struct {
	Currency Currency `json:"currency"`
	Minor    int64    `json:"minor"`
}

type Bill struct {
	ID          string     `json:"id"`
	CustomerID  string     `json:"customer_id"`
	Status      BillStatus `json:"status"`
	PeriodStart time.Time  `json:"period_start"`
	PeriodEnd   time.Time  `json:"period_end"`
	LineItems   []LineItem `json:"line_items"`
	CreatedAt   time.Time  `json:"created_at"`
	ClosedAt    *time.Time `json:"closed_at,omitempty"`
}

type LineItem struct {
	ID          string    `json:"id"`
	BillID      string    `json:"bill_id"`
	Description string    `json:"description"`
	Amount      Money     `json:"amount"`
	CreatedAt   time.Time `json:"created_at"`
}

type Invoice struct {
	BillID    string     `json:"bill_id"`
	Status    BillStatus `json:"status"`
	Totals    []Money    `json:"totals"`
	LineItems []LineItem `json:"line_items"`
	ClosedAt  time.Time  `json:"closed_at"`
}
