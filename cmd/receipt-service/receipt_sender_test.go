package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestReceiptWorkflow(t *testing.T) {
	tests := []struct {
		name       string
		event      PaymentEvent
		wantStatus int
		wantCalls  []string
	}{
		{
			name:       "approved payment sends and audits receipt",
			event:      PaymentEvent{EventID: "evt_42", Type: "payment.succeeded", OrderID: "ord_42", CustomerEmail: "buyer@example.com", AmountCents: 2599, Currency: "USD", RiskState: "approved"},
			wantStatus: http.StatusCreated,
			wantCalls:  []string{"POST /v1/email/send", "GET /v1/email/get/msg_42"},
		},
		{
			name:       "review payment does not send",
			event:      PaymentEvent{EventID: "evt_43", Type: "payment.succeeded", OrderID: "ord_43", CustomerEmail: "buyer@example.com", AmountCents: 2599, Currency: "USD", RiskState: "review"},
			wantStatus: http.StatusUnprocessableEntity,
			wantCalls:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.Method+" "+r.URL.Path)
				if r.URL.Path == "/v1/email/send" {
					if got := r.Header.Get("Idempotency-Key"); got != tt.event.EventID {
						t.Fatalf("idempotency key = %q", got)
					}
					writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]string{"message_id": "msg_42"}, "metadata": map[string]string{"vendor": "auto"}})
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]string{"message_id": "msg_42", "state": "queued"}})
			}))
			defer api.Close()

			client := NewInfraiClient(api.URL, "test-key", api.Client())
			handler := newReceiptHandler(&ReceiptSender{Email: client})
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/payment-events", jsonReader(t, tt.event))
			handler.ServeHTTP(recorder, request)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if tt.wantStatus == http.StatusCreated {
				var result ReceiptResult
				if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.DeliveryState != "queued" {
					t.Fatalf("delivery state = %q, want queued", result.DeliveryState)
				}
			}
			if !reflect.DeepEqual(calls, tt.wantCalls) {
				t.Fatalf("calls = %v, want %v", calls, tt.wantCalls)
			}
		})
	}
}

func TestClientDecodesEnvelopeBeforeHTTPStatus(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": map[string]string{"code": "recipient_invalid", "message": "recipient is invalid"}})
	}))
	defer api.Close()

	client := NewInfraiClient(api.URL, "test-key", api.Client())
	_, err := client.SendReceipt(context.Background(), EmailRequest{To: "bad", Subject: "Receipt", HTML: "<p>Receipt</p>"}, "evt_1")
	apiErr, ok := err.(*InfraiError)
	if !ok || apiErr.Code != "recipient_invalid" || apiErr.Status != http.StatusBadRequest {
		t.Fatalf("error = %#v", err)
	}
}

func jsonReader(t *testing.T, value any) *reader {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return &reader{data: b}
}

type reader struct {
	data []byte
	pos  int
}

func (r *reader) Read(p []byte) (int, error) {
	if r.pos == len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}
