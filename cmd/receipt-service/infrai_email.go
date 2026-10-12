package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type EmailRequest struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	HTML    string `json:"html"`
}

type SendResult struct {
	MessageID string `json:"message_id"`
}

type EmailRecord struct {
	MessageID string `json:"message_id"`
	Status    string `json:"state"`
}

type envelope[T any] struct {
	OK       bool            `json:"ok"`
	Data     T               `json:"data"`
	Error    *envelopeError  `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint"`
}

type InfraiError struct {
	Code    string
	Message string
	Status  int
}

func (e *InfraiError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

type InfraiClient struct {
	baseURL string
	key     string
	http    *http.Client
	wait    func(context.Context, time.Duration) error
}

func NewInfraiClient(baseURL, key string, client *http.Client) *InfraiClient {
	return &InfraiClient{baseURL: strings.TrimRight(baseURL, "/"), key: key, http: client, wait: waitContext}
}

func (c *InfraiClient) SendReceipt(ctx context.Context, email EmailRequest, eventID string) (SendResult, error) {
	return doRequest[SendResult](c, ctx, http.MethodPost, "/v1/email/send", email, eventID)
}

func (c *InfraiClient) GetEmail(ctx context.Context, messageID string) (EmailRecord, error) {
	path := "/v1/email/get/" + url.PathEscape(messageID)
	return doRequest[EmailRecord](c, ctx, http.MethodGet, path, nil, "")
}

func doRequest[T any](c *InfraiClient, ctx context.Context, method, path string, body any, idempotencyKey string) (T, error) {
	var zero T
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return zero, fmt.Errorf("encode request: %w", err)
		}
	}

	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(encoded))
		if err != nil {
			return zero, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}

		res, err := c.http.Do(req)
		if err != nil {
			return zero, fmt.Errorf("send request: %w", err)
		}
		payload, readErr := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		if readErr != nil {
			return zero, fmt.Errorf("read response: %w", readErr)
		}

		var reply envelope[T]
		if err := json.Unmarshal(payload, &reply); err != nil {
			return zero, fmt.Errorf("decode response: %w", err)
		}
		if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			delay := retryDelay(res.Header.Get("Retry-After"), attempt)
			if err := c.wait(ctx, delay); err != nil {
				return zero, err
			}
			continue
		}
		if !reply.OK {
			apiErr := &InfraiError{Status: res.StatusCode, Message: "request rejected"}
			if reply.Error != nil {
				apiErr.Code = reply.Error.Code
				apiErr.Message = reply.Error.Message
				if apiErr.Message == "" {
					apiErr.Message = reply.Error.Hint
				}
			}
			return zero, apiErr
		}
		if res.StatusCode >= 500 {
			return zero, fmt.Errorf("upstream status %d", res.StatusCode)
		}
		return reply.Data, nil
	}
	return zero, fmt.Errorf("retry limit reached")
}

func retryDelay(header string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(header); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 250 * time.Millisecond
}

func waitContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
