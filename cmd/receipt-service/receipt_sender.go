package main

import (
	"context"
	"fmt"
	"html"
)

type PaymentEvent struct {
	EventID       string `json:"event_id"`
	Type          string `json:"type"`
	OrderID       string `json:"order_id"`
	CustomerEmail string `json:"customer_email"`
	AmountCents   int64  `json:"amount_cents"`
	Currency      string `json:"currency"`
	RiskState     string `json:"risk_state"`
}

type ReceiptResult struct {
	OrderID       string `json:"order_id"`
	MessageID     string `json:"message_id"`
	DeliveryState string `json:"delivery_state"`
}

type EventRejectedError struct{ Reason string }

func (e *EventRejectedError) Error() string { return e.Reason }

type ReceiptSender struct{ Email EmailGateway }

func (s *ReceiptSender) Handle(ctx context.Context, event PaymentEvent) (ReceiptResult, error) {
	if event.EventID == "" || event.OrderID == "" || event.CustomerEmail == "" || event.AmountCents <= 0 || event.Currency == "" {
		return ReceiptResult{}, &EventRejectedError{Reason: "payment event is incomplete"}
	}
	if event.Type != "payment.succeeded" {
		return ReceiptResult{}, &EventRejectedError{Reason: "receipt requires a successful payment"}
	}
	if event.RiskState != "approved" {
		return ReceiptResult{}, &EventRejectedError{Reason: "receipt requires approved risk state"}
	}

	amount := fmt.Sprintf("%s %.2f", event.Currency, float64(event.AmountCents)/100)
	email := EmailRequest{
		To:      event.CustomerEmail,
		Subject: fmt.Sprintf("Receipt for order %s", event.OrderID),
		HTML: fmt.Sprintf(
			"<h1>Payment received</h1><p>Order %s</p><p>Total: %s</p>",
			html.EscapeString(event.OrderID), html.EscapeString(amount),
		),
	}

	sent, err := s.Email.SendReceipt(ctx, email, event.EventID)
	if err != nil {
		return ReceiptResult{}, fmt.Errorf("send receipt: %w", err)
	}
	record, err := s.Email.GetEmail(ctx, sent.MessageID)
	if err != nil {
		return ReceiptResult{}, fmt.Errorf("read delivery record: %w", err)
	}
	return ReceiptResult{OrderID: event.OrderID, MessageID: sent.MessageID, DeliveryState: record.Status}, nil
}
