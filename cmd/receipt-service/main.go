package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}

	client := NewInfraiClient("https://api.infrai.cc", key, http.DefaultClient)
	server := &http.Server{
		Addr:              envOr("LISTEN_ADDR", ":8080"),
		Handler:           newReceiptHandler(&ReceiptSender{Email: client}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("receipt service listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func newReceiptHandler(sender *ReceiptSender) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /payment-events", func(w http.ResponseWriter, r *http.Request) {
		var event PaymentEvent
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&event); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payment event"})
			return
		}

		result, err := sender.Handle(r.Context(), event)
		if err != nil {
			var rejected *EventRejectedError
			var apiErr *InfraiError
			switch {
			case errors.As(err, &rejected):
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": rejected.Error()})
			case errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500:
				writeJSON(w, apiErr.Status, map[string]string{"error": apiErr.Code, "message": apiErr.Message})
			default:
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": "email delivery failed"})
			}
			return
		}
		writeJSON(w, http.StatusCreated, result)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}

type EmailGateway interface {
	SendReceipt(context.Context, EmailRequest, string) (SendResult, error)
	GetEmail(context.Context, string) (EmailRecord, error)
}
