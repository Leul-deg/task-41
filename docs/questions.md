# Business Logic Questions Log

This document records open business questions identified while understanding `docs/prompt.md`, along with current assumptions and proposed implementation decisions.

## 1) Role Visibility and Access Boundaries
**Question:** The prompt says each role only sees permitted modules, but does not define exact module-to-role mapping or whether field-level restrictions are required inside a module.  
**My Understanding/Hypothesis:** Module-level access is mandatory; some sensitive actions (refund approval, exports, permission changes) likely require action-level permissions in addition to role visibility.  
**Solution:** Define an RBAC matrix with module permissions and action permissions (`view`, `create`, `update`, `approve`, `export`, `delete`) plus optional field masking rules.

## 2) Cross-Role Data Ownership Scope
**Question:** Can users act on all records in a module, or only records they created / records assigned to their site or warehouse?  
**My Understanding/Hypothesis:** Operational roles should be scoped by assignment (warehouse/site/team), while System Administrators and Compliance Officers can access broader scopes.  
**Solution:** Add data-scope rules (`global`, `site`, `warehouse`, `self`, `assigned`) and enforce at query layer.

## 3) Candidate Deduplication Identity Rules
**Question:** "Duplicate identity matches" is mentioned, but identity keys are not defined (email, phone, government ID, full name + DOB, etc.).  
**My Understanding/Hypothesis:** Dedup should be deterministic using configurable identity fields with weighted matching.  
**Solution:** Introduce dedup policy config with exact-match keys and optional fuzzy fields; generate a duplicate risk score and block/flag based on threshold.

## 4) Blocklist Evaluation Priority
**Question:** If multiple blocklist rules apply (email domain, duplicate identity, keyword), which one determines final outcome: flag, stop progression, or both?  
**My Understanding/Hypothesis:** "Stop progression" rules should take precedence over "flag only" rules.  
**Solution:** Implement ordered rule evaluation with severity levels (`info`, `warn`, `block`) and persist triggered rule IDs for auditability.

## 5) Pipeline Stage Config Constraints
**Question:** Employer-defined stages are allowed, but constraints are unclear (max stages, unique names, mandatory terminal states like Hire/Reject).  
**My Understanding/Hypothesis:** Pipeline must preserve reporting integrity and should require at least one successful terminal state and one unsuccessful terminal state.  
**Solution:** Validate stage configuration: unique stage codes, min/max stage count, and required terminal outcomes before activation.

## 6) Ticket Priority and SLA Calendar Definition
**Question:** Ticket escalation uses "4 business hours" and "1 business day," but business hours, timezone, weekends, and holiday rules are not specified.  
**My Understanding/Hypothesis:** SLA should use a configurable local business calendar with explicit timezone and holiday table.  
**Solution:** Add per-site SLA calendar settings and compute deadlines using business-time arithmetic.

## 7) Offline Draft Sync Conflict Resolution
**Question:** Offline drafts and optimistic updates can conflict with server state, but conflict resolution policy is unspecified.  
**My Understanding/Hypothesis:** User should be prompted with server-vs-local diff and choose merge, overwrite, or discard local change.  
**Solution:** Implement versioned records (ETag/revision), server-side conflict detection, and a conflict-resolution UI flow.

## 8) Attachment Governance for After-Sales Tickets
**Question:** File size/type/count limits are defined, but storage quota per ticket/user and duplicate file handling are not defined.  
**My Understanding/Hypothesis:** Limit enforcement should exist at upload time; duplicate uploads should be detected by checksum to reduce storage waste.  
**Solution:** Add checksum-based dedup, per-ticket cumulative size limit, and clear upload error taxonomy.

## 9) Multi-Warehouse Reservation Allocation Strategy
**Question:** Orders reserve stock immediately, but allocation strategy across warehouses/sub-warehouses is not defined (nearest, priority list, FEFO, manual).  
**My Understanding/Hypothesis:** Deterministic priority-based allocation is needed for consistency in offline local environments.  
**Solution:** Configure warehouse priority and allocation rules; record allocation decisions in reservation events.

## 10) Reservation Expiration Boundary Conditions
**Question:** Reservation auto-releases after 2 hours without confirmation, but edge conditions are unclear (partial confirmation, retry confirmations, clock skew).  
**My Understanding/Hypothesis:** Expiration should be evaluated by server time only; partial confirmation should reduce reservation proportionally.  
**Solution:** Use server authoritative timestamps, periodic release job, and state transitions supporting partial confirm/release.

## 11) Inventory Ledger Reversal Policy
**Question:** Ledger entries are immutable and "only reversed," but reversal trigger authority and reversal granularity (full vs partial) are unspecified.  
**My Understanding/Hypothesis:** Reversals should require reason codes and privileged approval for financial-impacting movements.  
**Solution:** Add reversal transaction type referencing original entry, enforce reason code + approver, and preserve full chain audit.

## 12) Cycle Count Variance Handling
**Question:** Cycle counts require reason codes, but acceptable variance thresholds and approval workflow for large adjustments are not defined.  
**My Understanding/Hypothesis:** Small variances may auto-post; large variances should require supervisor review.  
**Solution:** Introduce configurable variance thresholds by SKU category and approval workflow for out-of-threshold adjustments.

## 13) Return Eligibility Timestamp Source
**Question:** Returns are eligible within 30 days of delivery timestamp, but source-of-truth delivery event is not defined.  
**My Understanding/Hypothesis:** Delivery timestamp should come from immutable order fulfillment events in local PostgreSQL.  
**Solution:** Define canonical delivery event table and timezone normalization policy for eligibility computation.

## 14) Mixed Returnability in One Order
**Question:** Refund-only is enforced for non-returnable categories, but behavior for orders containing both returnable and non-returnable items is unclear.  
**My Understanding/Hypothesis:** Eligibility should be decided at line-item level, not whole-order level.  
**Solution:** Split after-sales decisions by order line, allowing mixed outcomes within one ticket.

## 15) Step-Up Verification Session Scope
**Question:** Sensitive actions require re-entering password within 5 minutes, but scope is unclear (per-action, per-user session, or per-module).  
**My Understanding/Hypothesis:** Step-up validity should be short-lived and action-class scoped to reduce friction while preserving security.  
**Solution:** Issue a step-up token bound to user, action class, and 5-minute TTL; invalidate on logout/password change.

## 16) Request Signing and Key Lifecycle
**Question:** All API requests require signing, but key issuance/rotation/revocation procedures are not specified for isolated local deployment.  
**My Understanding/Hypothesis:** Keys are locally managed with periodic rotation and immediate revocation support.  
**Solution:** Implement key registry with versioned secrets, rotation schedule, overlap window, and audit logs for key operations.

## 17) Nonce Replay Protection Data Store
**Question:** Nonces are accepted in a 2-minute window, but storage backend and eviction behavior are not defined.  
**My Understanding/Hypothesis:** Nonce uniqueness should be enforced per client key within the acceptance window using fast local storage.  
**Solution:** Store nonces in PostgreSQL/Redis-compatible local cache with TTL index, reject duplicates, and monitor replay attempts.

## 18) Idempotency Key Scope and TTL
**Question:** Idempotency is required for order creation and refunds, but key scope (per endpoint, per user, per payload hash) and retention period are not defined.  
**My Understanding/Hypothesis:** Scope should include endpoint + actor + normalized payload hash to prevent accidental collisions.  
**Solution:** Persist idempotency records with response snapshot and TTL policy (for example, 24-72 hours) plus conflict error semantics.

## 19) Data Deletion Requests vs Immutable Records
**Question:** Deletion requests must complete within 30 days, but immutable ledgers/audit requirements may conflict with hard deletion.  
**My Understanding/Hypothesis:** Sensitive personal fields should be anonymized where legal retention obligations prevent physical deletion.  
**Solution:** Define deletion policy matrix: hard delete where allowed, cryptographic erasure/anonymization otherwise, with compliance audit record.

## 20) Retention Policy Trigger Points
**Question:** Retention defaults are given (2 years rejected candidates, 7 years financial/support), but start timestamps are not defined.  
**My Understanding/Hypothesis:** Candidate retention starts at final reject date; financial/support retention starts at ticket/order closure date.  
**Solution:** Persist explicit retention anchor timestamp per record and run scheduled purge/anonymization jobs.

## 21) Local Crawler Opt-Out and Frequency Limits
**Question:** Crawler honors opt-out markers and frequency limits, but marker format/location and limit rules are unspecified.  
**My Understanding/Hypothesis:** Approved folders should define marker conventions and per-folder scan interval policies.  
**Solution:** Standardize marker files (for example `.no_index`), enforce folder-level scan cadence, and log skipped paths with reasons.

## 22) Nightly Crawl File Cap Overflow Handling
**Question:** Crawler is capped at 5,000 files per nightly run, but overflow behavior is unclear.  
**My Understanding/Hypothesis:** Remaining files should roll into subsequent runs deterministically to avoid starvation.  
**Solution:** Implement queue-based crawl ordering with checkpointing and fairness strategy across folders.

## 23) Sensitive Data Masking Scope
**Question:** SSN masking is required for UI/logs, but exported files, error traces, and search indexes are not explicitly covered.  
**My Understanding/Hypothesis:** Masking policy should be consistent across all output channels, not just UI and application logs.  
**Solution:** Centralize masking middleware/library and apply to API responses, logs, exports, crawler indexes, and audit views.

