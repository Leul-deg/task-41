# Meridian API Specification

## Authentication and Signing

All backend endpoints that mutate or expose protected state require signed requests from trusted clients.

Required signing headers:
- `X-Client-Key`
- `X-Timestamp`
- `X-Nonce`
- `X-Signature`

Protected auth endpoints are also signed:
- `POST /auth/login`
- `POST /auth/refresh`

Application endpoints under `/api/*` additionally require a valid bearer access token unless explicitly documented otherwise.

Sensitive action endpoints additionally require:
- `X-Step-Up-Token` for step-up protected routes such as deletion processing, exports, and ledger reversal

Public kiosk endpoints require:
- signing headers
- `X-Kiosk-Token`

## Core Routes

### Auth
- `POST /auth/login`: signed login request; returns `access_token` and `refresh_token`
- `POST /auth/refresh`: signed refresh request; returns a fresh `access_token`
- `POST /api/auth/step-up`: signed + bearer token; returns `step_up_token`
- `GET /api/auth/me`: signed + bearer token; returns roles, permissions, and scopes

### Hiring
- `GET /api/hiring/jobs`
- `POST /api/hiring/jobs`
- `POST /api/hiring/applications/manual`
- `POST /api/hiring/applications/kiosk`
- `POST /kiosk/applications`

### Support
- `GET /api/support/orders`
- `POST /api/support/tickets`
- `POST /api/support/tickets/:id/attachments`
- `POST /api/support/tickets/refund-approve`

### Inventory
- `POST /api/inventory/inbound`
- `POST /api/inventory/outbound`
- `POST /api/inventory/transfers`
- `POST /api/inventory/cycle-counts`
- `POST /api/inventory/ledger/:id/reverse`

### Compliance
- `POST /api/compliance/deletion-requests`
- `POST /api/compliance/deletion-requests/:id/process`
- `GET /api/compliance/audit-logs/export`
