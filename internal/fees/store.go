package fees

import (
	"context"
	"sync"
	"time"
)

type Store struct {
	mu    sync.Mutex
	bills map[string]Bill
}

func NewStore() *Store {
	return &Store{bills: make(map[string]Bill)}
}

func (s *Store) CreateBill(_ context.Context, bill Bill) (Bill, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bills[bill.ID] = cloneBill(bill)
	return cloneBill(bill), nil
}

func (s *Store) GetBill(_ context.Context, billID string) (Bill, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	bill, ok := s.bills[billID]
	if !ok {
		return Bill{}, ErrNotFound
	}
	return cloneBill(bill), nil
}

func (s *Store) AddLineItem(_ context.Context, billID string, item LineItem) (LineItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	bill, ok := s.bills[billID]
	if !ok {
		return LineItem{}, ErrNotFound
	}
	if bill.Status == BillStatusClosed {
		return LineItem{}, ErrBillClosed
	}

	bill.LineItems = append(bill.LineItems, item)
	s.bills[billID] = bill
	return item, nil
}

func (s *Store) CloseBill(_ context.Context, billID string, closedAt time.Time) (Bill, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	bill, ok := s.bills[billID]
	if !ok {
		return Bill{}, ErrNotFound
	}
	if bill.Status == BillStatusClosed {
		return cloneBill(bill), nil
	}

	bill.Status = BillStatusClosed
	bill.ClosedAt = &closedAt
	s.bills[billID] = bill
	return cloneBill(bill), nil
}

func cloneBill(bill Bill) Bill {
	if bill.LineItems != nil {
		bill.LineItems = append([]LineItem{}, bill.LineItems...)
	}
	return bill
}
