# Meridian Operations Hub

Meridian Operations Hub is an offline-first operations system for a mid-sized US direct-to-consumer retailer. This build runs fully on a local network and includes hiring, customer support, inventory, and compliance workflows.

## Stack

- Backend: Go + Gin + PostgreSQL
- Frontend: Go HTTP server + templ components (H5 mobile-first, responsive)
- Orchestration: Docker Compose

## Repository Layout

```text
.
├── README.md
├── .env.example
├── docker-compose.yml
├── backend/
└── frontend/
```

## Features in this MVP

- RBAC with module/action permissions and scope checks
- Data masking for SSN fields
- Encryption-at-rest for sensitive fields (`candidates.ssn_raw`) with key-version envelopes
- Request signing, nonce replay defense, idempotency records
- JWT access/refresh auth, lockout rules, and step-up verification
- Hiring pipeline templates/transitions, blocklist, candidate dedup scoring
- Support tickets with return eligibility, SLA due-time, attachment checks
- Inventory reservations with deterministic warehouse allocation and auto-release
- Immutable inventory ledger with reversal entries
- Compliance crawler, retention/deletion controls, and audit trail APIs
- Role/site/warehouse-aware data visibility on support and inventory APIs

## Frontend Routes

- `/`: login shell and session bootstrap
- `/dashboard`: cross-module overview with role-aware quick actions
- `/hiring`: job posting, manual/kiosk intake, CSV import, pipeline template manager, transition actions, application list/timeline
- `/hiring/kiosk`: dedicated candidate self-intake page for kiosk/tablet mode (no staff JWT session required)
- `/hiring/kiosk/qr`: QR image endpoint for kiosk deep-link sharing (`?url=` optional)
- `/support`: ticket lifecycle, attachment UX, offline draft, optimistic update/conflict handling
- `/inventory`: inbound/outbound/transfer/cycle count forms, balances, reservation list/actions
- `/compliance`: crawler controls, retention, deletion requests, audit log viewer/export

Protected route behavior:

- `/dashboard`, `/hiring`, `/support`, `/inventory`, `/compliance` now require a valid server-verified session cookie.
- Anonymous requests to protected pages are redirected to `/` before module HTML is served.
- Public pages remain `/` and `/hiring/kiosk` (plus static assets).
- Frontend login/refresh RPC endpoints set HttpOnly cookies (`meridian_access`, `meridian_refresh`); `/rpc/logout` clears them.

## Key Hiring APIs

- `GET /api/hiring/pipelines/templates`: list active/inactive pipeline templates
- `GET /api/hiring/pipelines/templates/:id`: fetch one pipeline template definition
- `PUT /api/hiring/pipelines/templates/:id`: update a pipeline template and validation settings
- `POST /api/hiring/pipelines/validate`: validate pipeline definition only (no database writes)
- `GET /api/hiring/applications/:id/allowed-transitions`: derive allowed next states for an application
- `POST /kiosk/applications`: signed kiosk intake endpoint (no staff JWT required; kiosk token required)

## Key Inventory APIs

- `POST /api/inventory/reservations/order-create`: create reservation hold (idempotency required)
- `POST /api/inventory/reservations/order-cancel`: immediately release active holds for canceled order
- `POST /api/inventory/reservations/:id/confirm`: confirm reservation and auto-release unconfirmed quantity

## Role-Based UI Behavior

- Frontend fetches `/rpc/api/auth/me` (BFF proxy to `/api/auth/me`) after session establishment.
- Navigation tabs, module cards, and action buttons are hidden/disabled when permissions are missing.
- Backend remains authoritative for permission/scope enforcement.
- Frontend uses HttpOnly cookies (`meridian_access`, `meridian_refresh`) as the server-trusted session for protected HTML route gating and BFF API proxying; browser JS does not persist access/refresh JWTs for normal operation, and `sessionStorage` only keeps non-sensitive UX state (user display context, step-up token, local draft/retry metadata).

## Gap-Closure Hardening (latest)

- Enforced signed requests on all `/api/*` routes (including GET/HEAD).
- Added nonce replay rejection coverage for read and write API paths.
- Added encrypted-at-rest handling for sensitive candidate SSN with key version support.
- Added duplicate blocklist rule evaluation into severity resolution (`info/warn/block`).
- Updated SLA calendar engine to honor configured `weekend_days` and `holidays`.
- Fixed support conflict resolution URL usage to always use ticket ID.
- Added role-aware frontend action/module visibility using `/api/auth/me` permissions.
- Added compliance audit log export endpoint (CSV/JSON), permission-gated by `export` and step-up token.
- Added backend and frontend tests for critical security/business rules.
- Added scope-aware object filtering for support/inventory read and write handlers.
- Added step-up enforcement for compliance deletion processing.
- Added object-level scope filtering for hiring jobs/applications/candidates/transitions/events.
- Made hiring pipeline validation side-effect free.
- Added explicit kiosk public submission flow with signed request + kiosk token controls.

## Prerequisites

- Docker + Docker Compose
- 4+ GB RAM for local containers

## Local Setup

To run without Docker, see **Without Docker** below.

1. Copy and edit environment values:

```bash
cp .env.example .env
```

2. Build and start all services:

```bash
docker compose up --build
```

3. Open frontend and backend health:

- Frontend: `http://localhost:8081`
- Backend health: `http://localhost:8080/healthz`

## Default Users and Roles

All default users use password `LocalAdminPass123!`:

- `admin` -> System Administrator
- `recruiter1` -> HR Recruiter
- `manager1` -> Hiring Manager
- `clerk1` -> Warehouse Clerk
- `agent1` -> Customer Service Agent
- `compliance1` -> Compliance Officer

Signing bootstrap key:

- Key ID: `local-h5`
- Secret: `local-h5-secret-change-me`

## Security Notes

- Password minimum length: 12 characters
- Lockout: 5 failed logins -> 15 minutes
- Step-up token required for sensitive actions (5-minute validity)
- Signed headers required for all `/api/*` routes:
  - `X-Client-Key`
  - `X-Timestamp` (RFC3339)
  - `X-Nonce`
  - `X-Signature`
- Idempotency key required for reservation order creation and refund approval routes
- Sensitive export actions require step-up and `export` permission.
- Kiosk public submissions require both signed request headers and `X-Kiosk-Token` (frontend proxy injects token).
- Frontend BFF sets HttpOnly auth cookies on `/rpc/login` and uses them to gate protected HTML routes.

## New Environment Variables

- `APP_ENV`: runtime mode (`dev|local|test` allow local defaults; non-dev enforces secure secrets).
- `PII_KEY_NAME`: encryption key namespace used for sensitive field encryption.
- `PII_KEY_VALUE`: initial local key material for bootstrap/key version 1.
- `KIOSK_SUBMIT_SECRET`: backend secret required for `/kiosk/applications`.
- `H5_KIOSK_SUBMIT_SECRET`: frontend proxy secret sent as `X-Kiosk-Token` for kiosk submissions.

## Secure Configuration

- For `APP_ENV=prod` (or any non-dev value), the API/worker fail fast unless these are rotated from defaults:
  - `JWT_SECRET`
  - `BOOTSTRAP_CLIENT_SECRET`
  - `DEFAULT_ADMIN_PASSWORD`
  - `PII_KEY_VALUE`
  - `KIOSK_SUBMIT_SECRET`
- Keep these values in `.env` and never commit production secrets.

## Running Tests and Checks

Project test framework layout (root-level, mandatory):

- `unit_tests/`: unit test suite runners/resources
- `API_tests/`: API functional test suite runners/resources
- `run_tests.sh`: master one-click test runner (unit + API)

Prerequisites for one-click run:

- Python 3 (or Python) for API suite
- Go toolchain for native unit tests OR Docker (runner auto-fallback)
- Native unit tests require Go `1.23+` (older versions fail on current backend/frontend modules).
- To always run unit tests in the Go container, set `MERIDIAN_USE_DOCKER_GO=1` (Docker required).
- Running app endpoints at `http://localhost:8081` (configure via `TEST_BASE_URL` if different)

One-click command (from project root):

```bash
./run_tests.sh
```

Sample output format:

```text
TEST=auth.me_unauthorized
STATUS=PASS
---
TEST=inventory.post_cancel_state_released
STATUS=FAIL
REASON=reservation not in RELEASED state after cancellation
LOG_SNIPPET={"reservations":[...]}
---
=== Final Summary ===
TOTAL=18
PASSED=17
FAILED=1
```

Exit codes:

- `0`: all tests passed
- `1`: one or more tests failed

Coverage notes:

- API suite covers major happy paths for hiring/support/inventory/compliance plus abnormal auth/validation/permission cases.
- Summary includes `TODO` coverage markers when known gaps remain.
- Frontend server-route tests in `frontend/cmd/web/main_test.go` cover unauthenticated protected-route redirect and authenticated dashboard load.

Browser permission/auth E2E (Playwright):

```bash
cd frontend/e2e
npm ci
npx playwright install chromium
npm test
```

Prerequisites:

- Running stack reachable at `http://localhost:8081` (override with `E2E_BASE_URL`)
- Default test password override via `E2E_PASSWORD`

Covered scenarios:

- Unauthenticated `/dashboard` redirects to `/`
- Successful login reaches `/dashboard` and does not bounce back to `/`
- Missing module permission shows explicit "Access Restricted" state (recruiter -> compliance)

Build/health checks:

```bash
docker compose build
docker compose up -d
curl http://localhost:8080/healthz
```

Backend tests (containerized Go):

```bash
docker run --rm -v "$(pwd -W)/backend:/src" -w /src golang:1.23-alpine /usr/local/go/bin/go mod tidy
docker run --rm -v "$(pwd -W)/backend:/src" -w /src golang:1.23-alpine /usr/local/go/bin/go test ./...
```

Frontend tests (containerized Go):

```bash
docker run --rm -v "$(pwd -W)/frontend:/src" -w /src golang:1.23-alpine /usr/local/go/bin/go mod tidy
docker run --rm -v "$(pwd -W)/frontend:/src" -w /src golang:1.23-alpine /usr/local/go/bin/go test ./...
```

Local non-Docker checks (if Go toolchain is installed):

```bash
cd backend && go test ./...
cd ../frontend && go test ./...
```

Expected outcome: all tests pass; any scope/auth regressions should fail handler/service tests.

## Without Docker

Minimal local run path:

1. Start PostgreSQL locally and create DB/user matching `.env.example` (or set `DATABASE_URL` accordingly).
2. Copy env and customize secrets/URLs:

```bash
cp .env.example .env
```

3. Start backend API (runs migrations on boot; migrationDir resolves `./migrations` then `/app/migrations`):

```bash
cd backend
go run ./cmd/api
```

4. Start backend worker (same DB/env):

```bash
cd backend
go run ./cmd/worker
```

5. Start frontend web server:

```bash
cd frontend
go run ./cmd/web
```

6. Run unit tests:

```bash
cd backend && go test ./...
cd ../frontend && go test ./...
```

Notes:

- `run_tests.sh` and `API_tests/` require the stack to be running at `http://localhost:8081`; they are integration-style checks and optional in lightweight CI.

## CI

- No repository-managed GitHub Actions workflow is enforced.
- Run checks locally as needed:
  - `cd backend && go test ./...`
  - `cd ../frontend && go test ./...`
  - optional browser E2E in `frontend/e2e`.

Sample login (via frontend RPC proxy):

```bash
curl -X POST http://localhost:8081/rpc/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"LocalAdminPass123!"}'
```

## Important Operational Notes

- Inventory ledger rows are immutable by DB trigger; reversals are linked records.
- Reservation holds release automatically after expiry via worker.
- Retention/anonymization jobs run in worker loop.
- Crawler indexes only approved sources and honors opt-out markers.
