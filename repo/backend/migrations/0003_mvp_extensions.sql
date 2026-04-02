ALTER TABLE users
ADD COLUMN IF NOT EXISTS site_code TEXT,
ADD COLUMN IF NOT EXISTS warehouse_code TEXT;

CREATE TABLE IF NOT EXISTS user_assignments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(user_id, entity_type, entity_id)
);

CREATE TABLE IF NOT EXISTS pipeline_templates (
    id UUID PRIMARY KEY,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    min_stages INT NOT NULL DEFAULT 3,
    max_stages INT NOT NULL DEFAULT 20,
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS pipeline_stages (
    id UUID PRIMARY KEY,
    template_id UUID NOT NULL REFERENCES pipeline_templates(id),
    code TEXT NOT NULL,
    name TEXT NOT NULL,
    order_index INT NOT NULL,
    terminal BOOLEAN NOT NULL DEFAULT false,
    outcome TEXT,
    required_fields TEXT NOT NULL DEFAULT '',
    UNIQUE(template_id, code),
    UNIQUE(template_id, order_index)
);

CREATE TABLE IF NOT EXISTS pipeline_transitions (
    id UUID PRIMARY KEY,
    template_id UUID NOT NULL REFERENCES pipeline_templates(id),
    from_stage_code TEXT NOT NULL,
    to_stage_code TEXT NOT NULL,
    required_fields TEXT NOT NULL DEFAULT '',
    screening_rule TEXT,
    UNIQUE(template_id, from_stage_code, to_stage_code)
);

CREATE TABLE IF NOT EXISTS blocklist_rules (
    id UUID PRIMARY KEY,
    rule_type TEXT NOT NULL CHECK (rule_type IN ('domain','duplicate','keyword')),
    pattern TEXT NOT NULL,
    severity TEXT NOT NULL CHECK (severity IN ('info','warn','block')),
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE applications
ADD COLUMN IF NOT EXISTS pipeline_template_id UUID REFERENCES pipeline_templates(id),
ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS application_pipeline_events (
    id UUID PRIMARY KEY,
    application_id UUID NOT NULL REFERENCES applications(id),
    actor_id UUID REFERENCES users(id),
    from_stage_code TEXT,
    to_stage_code TEXT NOT NULL,
    event_type TEXT NOT NULL,
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS orders (
    id TEXT PRIMARY KEY,
    customer_ref TEXT NOT NULL,
    site_code TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS order_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id TEXT NOT NULL REFERENCES orders(id),
    sku TEXT NOT NULL,
    category TEXT NOT NULL,
    quantity INT NOT NULL,
    unit_price NUMERIC(12,2) NOT NULL,
    returnable BOOLEAN NOT NULL DEFAULT true
);

CREATE TABLE IF NOT EXISTS delivery_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id TEXT NOT NULL REFERENCES orders(id),
    event_code TEXT NOT NULL,
    event_time TIMESTAMPTZ NOT NULL,
    canonical_delivered_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS support_ticket_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id UUID NOT NULL REFERENCES support_tickets(id),
    order_line_id UUID NOT NULL REFERENCES order_lines(id),
    requested_action TEXT NOT NULL,
    approved_action TEXT,
    reason_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE support_tickets
ADD COLUMN IF NOT EXISTS sla_due_at TIMESTAMPTZ,
ADD COLUMN IF NOT EXISTS calendar_site_code TEXT,
ADD COLUMN IF NOT EXISTS conflict_note TEXT;

CREATE TABLE IF NOT EXISTS warehouses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    site_code TEXT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT true
);

CREATE TABLE IF NOT EXISTS sub_warehouses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    warehouse_code TEXT NOT NULL REFERENCES warehouses(code),
    code TEXT NOT NULL,
    name TEXT NOT NULL,
    UNIQUE(warehouse_code, code)
);

CREATE TABLE IF NOT EXISTS warehouse_priorities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    site_code TEXT NOT NULL,
    warehouse_code TEXT NOT NULL REFERENCES warehouses(code),
    priority_rank INT NOT NULL,
    UNIQUE(site_code, warehouse_code),
    UNIQUE(site_code, priority_rank)
);

CREATE TABLE IF NOT EXISTS inventory_balances (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    warehouse_code TEXT NOT NULL REFERENCES warehouses(code),
    sub_warehouse_code TEXT,
    sku TEXT NOT NULL,
    on_hand INT NOT NULL DEFAULT 0,
    reserved INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(warehouse_code, sub_warehouse_code, sku)
);

CREATE TABLE IF NOT EXISTS safety_stock_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    site_code TEXT,
    role_code TEXT,
    sku TEXT,
    threshold INT NOT NULL DEFAULT 20,
    active BOOLEAN NOT NULL DEFAULT true
);

CREATE TABLE IF NOT EXISTS cycle_count_variance_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sku TEXT,
    category TEXT,
    threshold_percent NUMERIC(5,2) NOT NULL,
    require_supervisor BOOLEAN NOT NULL DEFAULT true
);

CREATE TABLE IF NOT EXISTS cycle_count_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    warehouse_code TEXT NOT NULL,
    reason_code TEXT NOT NULL,
    status TEXT NOT NULL,
    created_by UUID REFERENCES users(id),
    approved_by UUID REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS cycle_count_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES cycle_count_sessions(id),
    sku TEXT NOT NULL,
    expected_qty INT NOT NULL,
    counted_qty INT NOT NULL,
    variance INT NOT NULL,
    variance_percent NUMERIC(8,2) NOT NULL,
    require_approval BOOLEAN NOT NULL DEFAULT false
);

CREATE TABLE IF NOT EXISTS inventory_approvals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    approval_type TEXT NOT NULL,
    ref_id TEXT NOT NULL,
    status TEXT NOT NULL,
    requested_by UUID REFERENCES users(id),
    approved_by UUID REFERENCES users(id),
    reason_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ
);

ALTER TABLE crawler_sources
ADD COLUMN IF NOT EXISTS cursor_path TEXT,
ADD COLUMN IF NOT EXISTS nightly_cap INT NOT NULL DEFAULT 5000,
ADD COLUMN IF NOT EXISTS opt_out_marker TEXT NOT NULL DEFAULT '.no_index';

CREATE TABLE IF NOT EXISTS crawler_queue (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id UUID NOT NULL REFERENCES crawler_sources(id),
    file_path TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'PENDING',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(source_id, file_path)
);

CREATE TABLE IF NOT EXISTS searchable_index_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id UUID REFERENCES crawler_sources(id),
    file_path TEXT NOT NULL,
    checksum TEXT,
    masked_excerpt TEXT,
    indexed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(file_path)
);

CREATE TABLE IF NOT EXISTS encryption_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_name TEXT NOT NULL,
    key_version INT NOT NULL,
    key_value TEXT NOT NULL,
    status TEXT NOT NULL,
    valid_from TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(key_name, key_version)
);

CREATE TABLE IF NOT EXISTS key_rotation_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_name TEXT NOT NULL,
    from_version INT,
    to_version INT NOT NULL,
    actor_id UUID REFERENCES users(id),
    event_type TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO warehouses(code, name, site_code)
VALUES
    ('WH-1', 'Primary Warehouse', 'SITE-A'),
    ('WH-2', 'Overflow Warehouse', 'SITE-A')
ON CONFLICT (code) DO NOTHING;

INSERT INTO sub_warehouses(warehouse_code, code, name)
VALUES
    ('WH-1', 'A1', 'Inbound Zone'),
    ('WH-1', 'A2', 'Pick Zone'),
    ('WH-2', 'B1', 'Overflow Zone')
ON CONFLICT (warehouse_code, code) DO NOTHING;

INSERT INTO warehouse_priorities(site_code, warehouse_code, priority_rank)
VALUES
    ('SITE-A', 'WH-1', 1),
    ('SITE-A', 'WH-2', 2)
ON CONFLICT (site_code, warehouse_code) DO NOTHING;

INSERT INTO business_calendars(site_code, timezone, business_start, business_end, weekend_days, holidays)
VALUES
    ('SITE-A', 'America/New_York', '08:00', '18:00', '{0,6}', '{}')
ON CONFLICT (site_code) DO NOTHING;

INSERT INTO safety_stock_rules(site_code, threshold)
VALUES ('SITE-A', 20)
ON CONFLICT DO NOTHING;

INSERT INTO orders(id, customer_ref, site_code)
VALUES ('ORD-1001', 'CUST-1', 'SITE-A')
ON CONFLICT (id) DO NOTHING;

INSERT INTO order_lines(order_id, sku, category, quantity, unit_price, returnable)
VALUES
    ('ORD-1001', 'SKU-100', 'APPAREL', 2, 19.99, true),
    ('ORD-1001', 'SKU-200', 'FINAL_SALE', 1, 12.99, false)
ON CONFLICT DO NOTHING;

INSERT INTO delivery_events(order_id, event_code, event_time, canonical_delivered_at)
VALUES ('ORD-1001', 'DELIVERED', now() - interval '3 days', now() - interval '3 days')
ON CONFLICT DO NOTHING;

INSERT INTO inventory_balances(warehouse_code, sub_warehouse_code, sku, on_hand, reserved)
VALUES
    ('WH-1', 'A2', 'SKU-100', 200, 0),
    ('WH-1', 'A2', 'SKU-200', 40, 0),
    ('WH-2', 'B1', 'SKU-100', 30, 0)
ON CONFLICT (warehouse_code, sub_warehouse_code, sku) DO NOTHING;
