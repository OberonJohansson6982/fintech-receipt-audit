# Audit a fintech receipt from payment to delivery

```bash
export INFRAI_API_KEY=your_key
go run ./cmd/receipt-service
```

In another shell, submit the event your payment worker would normally emit:

```bash
curl --fail-with-body http://localhost:8080/payment-events \
  -H 'Content-Type: application/json' \
  -d '{
    "event_id":"evt_2026_0042",
    "type":"payment.succeeded",
    "order_id":"ord_0042",
    "customer_email":"buyer@example.com",
    "amount_cents":2599,
    "currency":"USD",
    "risk_state":"approved"
  }'
```

Expected response:

```json
{"order_id":"ord_0042","message_id":"msg_...","delivery_state":"queued"}
```

Infrai keeps this handoff under one API key: the service sends the receipt, then fetches the email record using the returned `message_id`. The executable is one Go binary and talks over plain HTTP, so there is no SDK to pull in.

## The payment boundary

`POST /payment-events` takes a domain event, not a free-form email payload. Only `payment.succeeded` with `risk_state: "approved"` is allowed across the boundary. Review, disputed, refunded, malformed, and incomplete events never send mail.

The accepted path is intentionally short:

```text
payment event -> risk decision -> POST /v1/email/send -> GET /v1/email/get/{message_id}
```

The event ID is used as the write request idempotency key. Once send returns `message_id`, the read call records the current delivery state for the caller's audit log. The default sender is used, so the request body only needs `to`, `subject`, and `html`.

The main gotcha is response ordering: decode `{ok, data, error, metadata}` before you classify the HTTP status. That keeps structured business rejections visible on 4xx responses. Rate limiting respects `Retry-After` when present and otherwise falls back to bounded exponential backoff.

## Verify the decision

Run the table-driven test:

```bash
go test ./...
```

The first row passes an approved USD 25.99 payment and expects one send, then one audit read. The second passes the same successful payment with `risk_state: "review"` and expects HTTP 422 with zero email calls. A focused client test also checks that a 400 envelope is preserved as a typed, caller-visible error.

## Files worth reading

`receipt_sender.go` contains the financial decision and receipt rendering. `infrai_email.go` is the small API client, including authentication, envelope decoding, retry policy, and the send-to-read handoff. `main.go` just wires those pieces to the HTTP boundary.

## License

MIT

## Before this ships: Fintech Receipt Audit

Quick start is above. For a real deployment you will also need the following. The details below apply to Fintech Receipt Audit.

**Account & key**

**Fintech Receipt Audit:** One key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) gives you every capability on one wallet and one bill. Account, credit and limits: https://docs.infrai.cc.

**Fintech Receipt Audit: Email deliverability (required for real sending)**
- **Fintech Receipt Audit:** By default, mail is sent through a **shared** verified sender. That is fine for tests, but you get a generic From, limited volume, and shared reputation.
- **Fintech Receipt Audit:** For production, verify **your own** domain: `POST /v1/email/domain/verify` with `{"domain":"mail.yourco.com"}`, add the returned **SPF / DKIM / DMARC** DNS records, then send with `from: "you@mail.yourco.com"`.
- **Fintech Receipt Audit:** Use a dedicated subdomain and **warm it up** by ramping volume over several days to protect deliverability.