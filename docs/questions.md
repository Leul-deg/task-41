# Business Logic Questions and Current Implementation Status

This document tracks how the original business questions map to the current codebase state.

Status values:
- **Implemented**: directly supported in current routes/handlers/middleware.
- **Partially Implemented**: core behavior exists, but boundary rules are still limited or policy-heavy.
- **Open**: still requires additional business-policy or implementation work.

## 1) Role Visibility and Access Boundaries
**Status:** Implemented  
**Current alignment:** Backend enforces module/action permissions (`view/create/update/approve/export/delete`) and frontend consumes `/api/auth/me` for UI gating.

## 2) Cross-Role Data Ownership Scope
**Status:** Implemented  
**Current alignment:** Route+object scope model uses `global`, `site`, `warehouse`, and `assigned`, with scoped filtering in module handlers.

## 3) Candidate Deduplication Identity Rules
**Status:** Partially Implemented  
**Current alignment:** Deterministic identity tokenization and optional fuzzy dedup are implemented, but policy tuning (weights/threshold governance) is still configuration-sensitive.

## 4) Blocklist Evaluation Priority
**Status:** Partially Implemented  
**Current alignment:** Blocklist rule creation and enforcement paths exist; rule precedence/severity policy is not fully documented as a strict ordered policy contract.

## 5) Pipeline Stage Config Constraints
**Status:** Implemented  
**Current alignment:** Pipeline template validate/create/update flows exist, with server-side validation before persistence/usage.

## 6) Ticket Priority and SLA Calendar Definition
**Status:** Partially Implemented  
**Current alignment:** SLA handling exists in support/compliance flow; full calendar governance detail (timezone/holiday policy matrix) remains policy-driven.

## 7) Offline Draft Sync Conflict Resolution
**Status:** Implemented  
**Current alignment:** Frontend includes draft persistence, retry queue behavior, and explicit conflict-resolve flow (`merge/overwrite/discard` UX path).

## 8) Attachment Governance for After-Sales Tickets
**Status:** Implemented  
**Current alignment:** Attachment size/type/count constraints and checksum-oriented duplicate handling are enforced by support attachment path.

## 9) Multi-Warehouse Reservation Allocation Strategy
**Status:** Implemented  
**Current alignment:** Inventory allocation and reservation flow are deterministic in service logic and recorded through reservation/ledger events.

## 10) Reservation Expiration Boundary Conditions
**Status:** Partially Implemented  
**Current alignment:** Reservation lifecycle (create/confirm/release/cancel) and worker-driven expiry exist; edge-policy details for all partial states still depend on runtime policy validation.

## 11) Inventory Ledger Reversal Policy
**Status:** Implemented  
**Current alignment:** Ledger is immutable-by-design with explicit reversal endpoint, reason codes, and step-up protected authorization.

## 12) Cycle Count Variance Handling
**Status:** Partially Implemented  
**Current alignment:** Cycle count flows and reason codes exist; advanced threshold/approval stratification is not fully codified as a separate policy layer.

## 13) Return Eligibility Timestamp Source
**Status:** Implemented  
**Current alignment:** Refund/after-sales decisions are server-side and tied to persisted order/support state, not client-side timestamps.

## 14) Mixed Returnability in One Order
**Status:** Partially Implemented  
**Current alignment:** Refund logic is enforced server-side with eligibility checks; mixed line-item policy depth requires continued business-rule validation.

## 15) Step-Up Verification Session Scope
**Status:** Implemented  
**Current alignment:** Step-up tokens are short-lived and action-class scoped, then required on sensitive endpoints via `X-Step-Up-Token`.

## 16) Request Signing and Key Lifecycle
**Status:** Implemented  
**Current alignment:** Signed request middleware, key rotation/revocation admin APIs, bootstrap key seeding, and encrypted secret storage are all in place.

## 17) Nonce Replay Protection Data Store
**Status:** Implemented  
**Current alignment:** Nonce replay protection is enforced in backend signing middleware with persistence-backed uniqueness checks in the accepted time window.

## 18) Idempotency Key Scope and TTL
**Status:** Implemented  
**Current alignment:** Idempotency records key on endpoint + actor + payload hash + idem key with TTL-backed persistence for protected POST endpoints.

## 19) Data Deletion Requests vs Immutable Records
**Status:** Partially Implemented  
**Current alignment:** Deletion request workflow, approvals, and auditing are implemented; full legal-policy matrix (hard delete vs anonymization by record class) remains compliance-policy dependent.

## 20) Retention Policy Trigger Points
**Status:** Partially Implemented  
**Current alignment:** Retention job/status endpoints exist with worker processing; fine-grained anchor semantics should stay explicitly documented in compliance policy.

## 21) Local Crawler Opt-Out and Frequency Limits
**Status:** Partially Implemented  
**Current alignment:** Crawler run/status and compliance controls exist; exact marker conventions and folder-level cadence policy remain operational configuration concerns.

## 22) Nightly Crawl File Cap Overflow Handling
**Status:** Partially Implemented  
**Current alignment:** Crawler controls are present, but deterministic overflow fairness policy should remain explicitly specified in operational runbooks.

## 23) Sensitive Data Masking Scope
**Status:** Partially Implemented  
**Current alignment:** Masking/encryption protections exist for sensitive paths; exhaustive cross-channel masking governance (all exports and derived indexes) remains an ongoing policy hardening area.

