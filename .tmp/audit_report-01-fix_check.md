# Audit Report 01 - Fix Check

## Issues and Fix Commentary

1) **High - Hiring CSV import scope/object bypass**  
**Fix status:** Resolved  
**Brief commentary:** Route-level and object-level scope enforcement were tightened for CSV import, so out-of-scope imports are denied instead of being accepted. The handler now validates scope before import execution.

2) **High - Plaintext secret material at rest**  
**Fix status:** Resolved  
**Brief commentary:** Secret material handling was moved to encrypted-at-rest paths with centralized secret-store protection, bootstrap hardening, and signing/middleware alignment so sensitive values are no longer persisted as plaintext.

3) **Medium - Assigned-scope support create broader than assignment semantics**  
**Fix status:** Resolved  
**Brief commentary:** Support ticket creation was constrained to assignment-aware scope semantics, preventing broader create behavior that could exceed assigned visibility boundaries.

4) **Medium - Missing security regression coverage for CSV scope bypass**  
**Fix status:** Resolved  
**Brief commentary:** Regression tests were added in both handler-level tests and API test flows to explicitly verify out-of-scope CSV import denial, reducing risk of silent reintroduction.

5) **Low - Test runner auto-start side effect**  
**Fix status:** Resolved  
**Brief commentary:** Test-runner behavior was made explicit/documented so environment startup side effects are controlled and predictable during scripted execution.
