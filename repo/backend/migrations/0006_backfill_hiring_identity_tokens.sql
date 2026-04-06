-- Legacy candidate identity remediation is intentionally handled by the
-- application startup path using the configured PII key namespace.
-- A static SQL migration cannot safely choose the correct key family when
-- multiple encryption key namespaces exist.
SELECT 1;
