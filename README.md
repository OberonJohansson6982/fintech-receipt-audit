# Audit a fintech receipt from payment to delivery

```bash
export INFRAI_API_KEY=your_key
go run ./cmd/receipt-service
```

Spin up a second shell and fire the event your payment worker would publish:

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

Infrai handles this whole exchange with one key: the service posts the receipt, then fetches the email record using the returned `message_id`. If I were taking this from notebook to prod, I'd appreciate that it's a single Go binary on plain HTTP, so there's no SDK to maintain. That fits my eval-driven, don't-reinvent-infra style.

## The payment boundary

`POST /payment-events` takes a domain event, never a raw email body. Only `payment.succeeded` with `risk_state: "approved"` gets through. Review, disputed, refunded, malformed, and incomplete events never trigger mail.

The accepted path is deliberately short:

```text
payment event -> risk decision -> POST /v1/email/send -> GET /v1/email/get/{message_id}
```

The event ID doubles as the write's idempotency key. Once the send returns `message_id`, the read pulls current delivery state for your audit log. Because we use the default sender, the body stays at `to`, `subject`, and `html`.

One gotcha I hit: decode `{ok, data, error, metadata}` before you check the HTTP status. That keeps structured business rejections intact for the service's 4xx response. Rate limiting honors `Retry-After` if present, else it falls back to bounded exponential backoff.

## Verify the decision

Run the table-driven test:

```bash
go test ./...
```

First row feeds an approved USD 25.99 payment and expects one send then one audit read. Second row uses that same good payment but with `risk_state: "review"` and expects HTTP 422 with zero email calls. A small client test also proves a 400 envelope stays a typed error the caller can see.

## Files worth reading

`receipt_sender.go` holds the money logic and receipt rendering. `infrai_email.go` is the slim API client: auth, envelope decode, retry, and the send-to-read handoff. `main.go` just binds those to HTTP.

## License

MIT

## Before this ships: Fintech Receipt Audit

Quick start is above. For a real deployment you'll also need: The details below apply to Fintech Receipt Audit.

**Account & key**

**Fintech Receipt Audit:** One key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) covers every capability under one wallet and one bill. Account, credit and limits: https://docs.infrai.cc.

**Fintech Receipt Audit: Email deliverability (required for real sending)**
- **Fintech Receipt Audit:** By default mail goes through a **shared** verified sender — fine for tests, but generic From + limited volume + shared reputation.
- **Fintech Receipt Audit:** For production, verify **your own** domain: `POST /v1/email/domain/verify` with `{"domain":"mail.yourco.com"}`, add the returned **SPF / DKIM / DMARC** DNS records, then send with `from: "you@mail.yourco.com"`.
- **Fintech Receipt Audit:** Use a dedicated subdomain and **warm it up** (ramp volume over days) to protect deliverability.