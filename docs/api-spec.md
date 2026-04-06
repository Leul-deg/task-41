# Meridian Operations Hub — API Specification

## 1. Scope

This specification documents the **backend HTTP API** implemented in `repo/backend` (Go + Gin + PostgreSQL). Layout matches `repo/README.md` (`backend/` and `frontend/` at repository root).

| Item | Default (local / Compose) |
| :--- | :--- |
| **Base URL** | `http://localhost:8080` |
| **Frontend BFF** (browser) | `http://localhost:8081` — same-origin `/rpc/api/...` proxies to this API with signing + cookies (see `repo/README.md`) |

Direct API clients must satisfy **request signing** and **JWT** rules below. The H5 app is expected to call the BFF, not the backend origin directly from untrusted browsers without that proxy.

---

## 2. Conventions

### 2.1 Response shape

Gin handlers return JSON objects; there is **no** unified `{ success, data, meta }` envelope.

Typical patterns:

- **Success:** `200` with a domain-specific JSON body (e.g. `{"id":"...", "username":"..."}`).
- **Error:** `4xx` / `5xx` with `{"error":"message"}` (and sometimes extra fields).

### 2.2 Request signing (required for `/api/*` and kiosk routes)

All routes under `/api` use middleware that requires the same signing headers. **Kiosk** routes (`POST /kiosk/applications`, `GET /kiosk/jobs`) require signing plus `X-Kiosk-Token`; they do **not** use staff JWTs (see §5).

| Header | Description |
| :--- | :--- |
| `X-Client-Key` | Registered client key id (e.g. bootstrap `local-h5`) |
| `X-Timestamp` | RFC3339 UTC time; must be within **±2 minutes** of server time |
| `X-Nonce` | Unique per request for that key; **replay** returns `409` |
| `X-Signature` | HMAC-SHA256 over `METHOD`, path, timestamp, nonce, and SHA256 hex of raw body |

The canonical path for signing is the **backend** path (e.g. `/api/hiring/jobs`), not the frontend `/rpc` prefix.

Health check **`GET /healthz`** is **unsigned**.

### 2.3 Authentication

| Surface | Auth |
| :--- | :--- |
| `POST /auth/login`, `POST /auth/refresh` | Signing required; **No** JWT; JSON body with credentials / refresh token |
| `GET /kiosk/jobs`, `POST /kiosk/applications` | Signing + `X-Kiosk-Token` (no staff JWT) |
| `POST /api/auth/step-up`, `GET /api/auth/me` | Signing + `Authorization: Bearer <access_jwt>` |
| All other `/api/*` | Signing + Bearer access JWT |

Refresh tokens are **opaque** strings stored server-side; login returns both `access_token` and `refresh_token`. Refresh response returns **`access_token` only** (refresh cookie/token reuse until expiry).

### 2.4 Step-up (sensitive actions)

Some routes require a recent step-up proof:

1. `POST /api/auth/step-up` with JSON `{"password":"...","action_class":"..."}` → `{"step_up_token":"..."}`.
2. Client sends `X-Step-Up-Token: <token>` on the protected request.

Action classes used in routing include (non-exhaustive): `refund_approval`, `role_permission_change`, `delete_or_reversal`, `export`.

### 2.5 Idempotency

For **`POST`** only, these endpoints **require** header `Idempotency-Key`:

| Path |
| :--- |
| `/api/inventory/reservations/order-create` |
| `/api/support/tickets/refund-approve` |

Other POSTs under `/api` do not require this header (middleware passes through).

### 2.6 RBAC model

- **Permissions:** `module` + `action` (e.g. `hiring` + `view`, `support` + `approve`).
- **Scopes:** per-module rules such as `global`, `site`, `warehouse`, `assigned` with optional `value`.

Enforcement is via `RequirePermission` and `RequireScope` middleware on route groups. **`GET /api/auth/me`** returns `permissions` and `scopes` for UI gating.

---

## 3. Health

| Method | Path | Auth | Description |
| :--- | :--- | :--- | :--- |
| `GET` | `/healthz` | No | `{"ok": true}` |

---

## 4. Authentication and identity

| Method | Path | Signing | JWT | Description |
| :--- | :--- | :--- | :--- | :--- |
| `POST` | `/auth/login` | Yes | No | Body: `username`, `password` (min 12 chars). Returns `access_token`, `refresh_token`. Lockout after 5 failures / 15 minutes. |
| `POST` | `/auth/refresh` | Yes | No | Body: `refresh_token`. Returns `access_token`. |
| `POST` | `/api/auth/step-up` | Yes | Yes | Re-verify password; returns `step_up_token`. |
| `GET` | `/api/auth/me` | Yes | Yes | Current user `id`, `username`, `roles`, `permissions`, `scopes`. |

---

## 5. Public kiosk (hiring intake)

| Method | Path | Auth | Description |
| :--- | :--- | :--- | :--- |
| `GET` | `/kiosk/jobs` | Signing + `X-Kiosk-Token` | List jobs exposed for kiosk intake (public; no staff JWT). |
| `POST` | `/kiosk/applications` | Signing + `X-Kiosk-Token` | Create application from public kiosk (no staff JWT). |

---

## 6. Hiring (`/api/hiring`)

All routes: signing + JWT + permission/scope as noted.

| Method | Path | Permission | Scope (typical) | Description |
| :--- | :--- | :--- | :--- | :--- |
| `GET` | `/jobs` | `hiring:view` | global / site / assigned | List jobs |
| `GET` | `/jobs/for-intake` | `hiring:create` | global / site / assigned | List jobs for intake UIs (fallback when `GET /jobs` is forbidden) |
| `GET` | `/applications` | `hiring:view` | global / site / assigned | List applications |
| `GET` | `/pipelines/templates` | `hiring:view` | — | List pipeline templates |
| `GET` | `/pipelines/templates/:id` | `hiring:view` | — | Get template |
| `POST` | `/jobs` | `hiring:create` | global / site / assigned | Create job |
| `POST` | `/applications/manual` | `hiring:create` | global / site / assigned | Manual application |
| `POST` | `/applications/kiosk` | `hiring:create` | global / site / assigned | Staff kiosk intake |
| `POST` | `/applications/import-csv` | `hiring:create` | — | CSV import |
| `POST` | `/pipelines/templates` | `hiring:update` | — | Create template |
| `PUT` | `/pipelines/templates/:id` | `hiring:update` | — | Update template |
| `POST` | `/pipelines/validate` | `hiring:update` | — | Validate definition (no DB write) |
| `POST` | `/applications/:id/transition` | `hiring:update` | global / site / assigned | Pipeline transition |
| `GET` | `/applications/:id/allowed-transitions` | `hiring:view` | global / site / assigned | Allowed next states |
| `GET` | `/applications/:id/events` | `hiring:view` | global / site / assigned | Pipeline events |
| `GET` | `/candidates/:id` | `hiring:view` | global / site / assigned | Candidate detail |
| `POST` | `/blocklist/rules` | `hiring:update` | — | Create blocklist rule |

---

## 7. Support (`/api/support`)

| Method | Path | Permission | Scope | Notes |
| :--- | :--- | :--- | :--- | :--- |
| `GET` | `/orders` | `support:view` | global / site / assigned | List orders (for ticket/order pickers) |
| `GET` | `/orders/for-intake` | `support:create` | global / site / assigned | List orders when `GET /orders` is not allowed (intake fallback) |
| `GET` | `/tickets` | `support:view` | global / site / assigned | List tickets |
| `POST` | `/tickets` | `support:create` | global / site / assigned | Create ticket |
| `GET` | `/tickets/:id` | `support:view` | global / site / assigned | Get ticket |
| `PUT` | `/tickets/:id` | `support:update` | global / site / assigned | Update ticket |
| `POST` | `/tickets/:id/attachments` | `support:update` | global / site / assigned | Add attachment |
| `POST` | `/tickets/:id/conflict-resolve` | `support:update` | global / site / assigned | Resolve optimistic conflict |
| `POST` | `/tickets/refund-approve` | `support:approve` | global / site / assigned | **Idempotency-Key** + **step-up** `refund_approval` |

---

## 8. Inventory (`/api/inventory`)

| Method | Path | Permission | Scope | Notes |
| :--- | :--- | :--- | :--- | :--- |
| `GET` | `/orders` | `inventory:view` | global / site / warehouse | List orders (for reservation pickers) |
| `GET` | `/orders/for-intake` | `inventory:create` | global / site / warehouse | List orders when `GET /orders` is not allowed (intake fallback) |
| `GET` | `/balances` | `inventory:view` | global / site / warehouse | Balances |
| `GET` | `/reservations` | `inventory:view` | global / site / warehouse | Reservations |
| `POST` | `/inbound` | `inventory:create` | global / site / warehouse | Inbound move |
| `POST` | `/outbound` | `inventory:create` | global / site / warehouse | Outbound move |
| `POST` | `/transfers` | `inventory:create` | global / site / warehouse | Transfer |
| `POST` | `/cycle-counts` | `inventory:create` | global / site / warehouse | Cycle count |
| `POST` | `/reservations/order-create` | `inventory:create` | global / site / warehouse | **Idempotency-Key** required |
| `POST` | `/reservations/order-cancel` | `inventory:update` | global / site / warehouse | Cancel order holds |
| `POST` | `/reservations/:id/confirm` | `inventory:update` | global / site / warehouse | Confirm reservation |
| `POST` | `/reservations/:id/release` | `inventory:update` | global / site / warehouse | Release reservation |
| `POST` | `/ledger/:id/reverse` | `inventory:approve` | global / site / warehouse | **step-up** `delete_or_reversal` |

---

## 9. Admin (`/api/admin`)

| Method | Path | Permission | Notes |
| :--- | :--- | :--- | :--- |
| `GET` | `/roles` | `admin:view` | List roles |
| `PUT` | `/roles/:id/permissions` | `admin:update` | **step-up** `role_permission_change` |
| `PUT` | `/roles/:id/scopes` | `admin:update` | **step-up** `role_permission_change` |
| `POST` | `/client-keys/rotate` | `admin:update` | **step-up** `role_permission_change` |
| `POST` | `/client-keys/:id/revoke` | `admin:delete` | **step-up** `delete_or_reversal` |

---

## 10. Compliance (`/api/compliance`)

| Method | Path | Permission | Scope | Notes |
| :--- | :--- | :--- | :--- | :--- |
| `POST` | `/crawler/run` | `compliance:create` | global | Run crawler |
| `GET` | `/crawler/status` | `compliance:view` | global | Status |
| `POST` | `/deletion-requests` | `compliance:create` | global | Create deletion request |
| `GET` | `/deletion-requests` | `compliance:view` | global | List requests |
| `POST` | `/deletion-requests/:id/process` | `compliance:approve` | global | **step-up** `delete_or_reversal` |
| `GET` | `/retention/jobs` | `compliance:view` | global | Retention status |
| `GET` | `/audit-logs` | `compliance:view` | global | Audit log query |
| `GET` | `/audit-logs/export` | `compliance:export` | global | **step-up** `export` |

---

## 11. Representative request examples

### Login (signed)

`POST /auth/login`

```json
{
  "username": "admin",
  "password": "LocalAdminPass123!"
}
```

Response (illustrative): `{"access_token":"...","refresh_token":"..."}`

### Signed API call (conceptual)

`GET /api/auth/me` with headers:

- `Authorization: Bearer <access_token>`
- `X-Client-Key`, `X-Timestamp`, `X-Nonce`, `X-Signature` (computed per server rules)

### Step-up then refund approve

1. `POST /api/auth/step-up` — body `{"password":"...","action_class":"refund_approval"}`
2. `POST /api/support/tickets/refund-approve` — header `Idempotency-Key: <uuid>`, header `X-Step-Up-Token: <step_up_token>`, plus signing + JWT

### Reservation create (idempotent)

`POST /api/inventory/reservations/order-create` with `Idempotency-Key` and signed JSON body per handler contract.

---

## 12. Source of truth

Route registration: `repo/backend/cmd/api/main.go`.  
Behavioral details (payload fields, status codes): handler packages under `repo/backend/internal/http/handlers/`.

Idempotency enforcement (exact `POST` paths): `repo/backend/internal/http/middleware/idempotency.go` (`RequireIdempotency`).

---

## 13. Notes

- **401** / **403** / **404** / **409** are used for auth, permission, missing resources, and nonce replay respectively.
- **423** (`StatusLocked`) may be returned for locked accounts on login.
- Sensitive fields (e.g. SSN) may be masked or encrypted at rest per product rules; see `repo/README.md` and hiring/compliance handlers.
