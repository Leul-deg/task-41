package templates

import (
	"fmt"
	"strings"

	"github.com/a-h/templ"
	templruntime "github.com/a-h/templ/runtime"
)

func LoginPage() templ.Component {
	body := `<main class="auth-layout"><section class="auth-hero"><h1>Meridian Operations Hub</h1><p>Run hiring, support, inventory, and compliance operations from a secured local platform.</p><div class="trust-grid"><div class="trust-item">Signed requests + nonce replay defense</div><div class="trust-item">Role-based module and action controls</div><div class="trust-item">Step-up protections for sensitive actions</div></div></section><section class="card auth-card"><h2 class="title">Sign in</h2><p class="subtitle">Use your operations account credentials.</p><form id="login-form" class="stack"><label class="label" for="username">Username</label><input id="username" class="input" required autocomplete="username"><label class="label" for="password">Password</label><input id="password" type="password" class="input" required autocomplete="current-password"><button class="btn" type="submit">Sign In</button><p id="login-error" class="error-text" aria-live="polite"></p></form></section></main>`
	return shell("Meridian Operations Hub", "login", body)
}

func DashboardPage() templ.Component {
	body := `<main class="page-wrap"><section class="card span-all"><div class="panel-head"><h1 class="page-title">Operations Dashboard</h1><div class="chip-row"><span class="chip">Offline-first</span><span class="chip">Auditable</span><span class="chip">Role-aware</span></div></div><p class="page-subtitle">Track session health and jump directly into module-specific actions.</p><div class="stats-strip"><div class="stat-tile"><div class="stat-label">User</div><div class="stat-value" id="session-user">Unknown</div></div><div class="stat-tile"><div class="stat-label">Network</div><div class="stat-value" id="network-state">Online</div></div><div class="stat-tile"><div class="stat-label">Retry Queue</div><div class="stat-value" id="queue-state">0</div></div><div class="stat-tile"><div class="stat-label">Mode</div><div class="stat-value">Operator</div></div></div></section><section class="dashboard-grid">` +
		card("Hiring", `<p class="muted">Post jobs, capture candidates, and advance pipeline stages.</p><div class="actions-row"><button class="btn" id="create-job-btn" type="button">Quick: sample job</button><button class="ghost-btn" id="create-kiosk-app-btn" type="button">Kiosk intake</button></div>`, `id="card-hiring"`) +
		card("Support", `<p class="muted">Create service tickets and resolve conflicts with SLA context.</p><div class="actions-row"><button class="btn" id="create-ticket-btn" type="button">Quick: sample ticket</button><button class="ghost-btn" id="load-ticket-btn" type="button">Load ticket + SLA</button></div>`, `id="card-support"`) +
		card("Inventory", `<p class="muted">Manage reservations and monitor stock movement outcomes.</p><div class="actions-row"><button class="btn" id="create-reservation-btn" type="button">Create reservation</button><button class="ghost-btn" id="inventory-balance-btn" type="button">Check low stock</button></div>`, `id="card-inventory"`) +
		card("Compliance", `<p class="muted">Run crawler tasks and review retention/deletion controls.</p><div class="actions-row"><button class="btn" id="run-crawler-btn" type="button">Run crawler</button><button class="ghost-btn" id="retention-status-btn" type="button">Retention status</button></div>`, `id="card-compliance"`) +
		card("Activity", `<p class="muted">System feedback and sampled action outputs.</p><pre class="log" id="activity-log"></pre>`, `class="card span-all"`) +
		`</section></main>`
	return appShell("Dashboard", "dashboard", body)
}

func HiringPage() templ.Component {
	body := `<main class="page-wrap"><section class="card span-all"><div class="panel-head"><h1 class="page-title">Hiring Operations</h1><div class="chip-row"><span class="chip">Intake</span><span class="chip">Transitions</span><span class="chip">Pipeline Admin</span></div></div><p class="page-subtitle">Capture candidate data quickly, then manage transitions and templates with confidence.</p></section><section class="module-grid">` +
		card("Job Posting", field("job-code", "Code")+field("job-title", "Title")+fieldArea("job-description", "Description", 3)+`<div class="actions-row"><button class="btn" id="hiring-create-job" type="button">Create Job</button><button class="ghost-btn" id="hiring-load-jobs" type="button">Load Jobs</button></div><pre class="log" id="hiring-jobs-log"></pre><div id="hiring-job-msg" class="muted" role="status" aria-live="polite"></div>`, ``) +
		card("Candidate Intake", jobSelectField("manual-job-id", "Job")+field("manual-name", "Full Name")+field("manual-email", "Email")+field("manual-phone", "Phone")+field("manual-ssn", "SSN")+`<div class="actions-row"><button class="btn" id="hiring-manual-submit" type="button">Submit Manual Intake</button><button id="kiosk-submit-btn" class="ghost-btn" type="button">Submit Kiosk Intake</button></div><div id="hiring-intake-msg" class="muted" role="status" aria-live="polite"></div><span class="status-badge warn" id="hiring-rule-status">No rule feedback yet</span>`, ``) +
		card("Kiosk QR", `<p class="muted">Share kiosk mode for supervised walk-up candidate capture.</p><img id="hiring-kiosk-qr" alt="Kiosk QR" class="qr-image"><p><a id="hiring-kiosk-url" href="/hiring/kiosk" class="nav-link">Open kiosk page</a></p>`, ``) +
		card("Pipeline Transitions", idSelectField("transition-app-id", "Application ID", "Select an application")+field("transition-from", "From Stage")+field("transition-to", "To Stage")+fieldArea("transition-required-json", "Required Fields JSON", 4)+`<p class="muted">Use <strong>Refresh Applications</strong> to view and select recently created applications.</p><div class="actions-row"><button id="pipeline-transition-btn" class="btn" type="button">Transition Stage</button><button id="load-allowed-transitions-btn" class="ghost-btn" type="button">Load Allowed Transitions</button><button id="load-applications-btn" class="ghost-btn" type="button">Refresh Applications</button></div><pre class="log" id="allowed-transitions-log"></pre><pre class="log" id="hiring-applications-log"></pre>`, `class="card span-all"`) +
		card("Application Timeline", idSelectField("timeline-app-id", "Application ID", "Select an application")+`<div class="actions-row"><button id="load-timeline-btn" class="ghost-btn" type="button">Load Timeline</button></div><pre class="log" id="hiring-timeline-log"></pre>`, `class="card span-all"`) +
		card("CSV Import", jobSelectField("csv-job-id", "Job")+fieldArea("csv-text", "CSV Data (name,email,phone)", 6)+`<p class="muted">Expected format: <code>full_name,email,phone</code> with one candidate per line.</p><div class="actions-row"><button id="csv-validate-btn" class="ghost-btn" type="button">Validate CSV</button><button id="csv-import-btn" class="btn" type="button">Import CSV</button></div><pre class="log" id="csv-validation-log"></pre>`, `class="card span-all"`) +
		card("Advanced Pipeline Config", field("pipeline-code", "Pipeline Code")+field("pipeline-name", "Pipeline Name")+fieldArea("pipeline-stages-json", "Stages JSON", 6)+fieldArea("pipeline-transitions-json", "Transitions JSON", 6)+`<p class="muted">Stages should include success and failure terminal states. Transition objects require <code>from_stage_code</code> and <code>to_stage_code</code>.</p><div class="actions-row"><button id="pipeline-validate-btn" class="ghost-btn" type="button">Validate Pipeline</button><button id="pipeline-save-btn" class="btn" type="button">Save Pipeline</button><button id="pipeline-load-btn" class="ghost-btn" type="button">Load Templates</button></div><pre class="log" id="pipeline-config-log"></pre>`, `class="card span-all"`) +
		`</section></main>`
	return appShell("Hiring", "hiring", body)
}

func HiringKioskPage() templ.Component {
	body := `<main class="page-wrap"><section class="card auth-card kiosk-hero"><h1 class="title">Hiring Kiosk</h1><p class="subtitle">Quick candidate intake for on-site recruitment desks.</p><p class="kiosk-note">This kiosk submits through a dedicated signed flow and never requires a staff login session.</p>` +
		jobSelectField("kiosk-job-id", "Job") + field("kiosk-name", "Full Name") + field("kiosk-email", "Email") + field("kiosk-phone", "Phone") + field("kiosk-ssn", "SSN") +
		`<div class="actions-row"><button class="btn" id="kiosk-page-submit" type="button">Submit Application</button></div><p id="kiosk-page-msg" class="muted" aria-live="polite"></p></section></main>`
	return shell("Meridian Hiring Kiosk", "hiring-kiosk", body)
}

func SupportPage() templ.Component {
	body := `<main class="page-wrap"><section class="card span-all"><div class="panel-head"><h1 class="page-title">Support Operations</h1><div class="chip-row"><span class="chip">Draft Safe</span><span class="chip">Optimistic Update</span><span class="chip">Conflict Ready</span></div></div><p class="page-subtitle">Create tickets quickly while preserving reliable retry and conflict handling.</p></section><section class="module-grid">` +
		card("Create Ticket", selectField("support-ticket-type", "Type", []opt{{"return_and_refund", "Return + Refund"}, {"refund_only", "Refund Only"}})+idSelectField("support-order-id", "Order", "Select an order")+`<div class="actions-row"><button id="support-load-orders" class="ghost-btn" type="button">Load Orders</button></div>`+selectField("support-priority", "Priority", []opt{{"STANDARD", "Standard"}, {"HIGH", "High"}})+fieldArea("support-description", "Description", 3)+`<div class="actions-row"><button class="btn" id="support-create-ticket" type="button">Create Ticket</button></div><pre class="log" id="support-orders-log"></pre><div id="support-ticket-msg" class="muted" role="status" aria-live="polite"></div><span id="support-draft-status" class="status-badge info">Draft not restored</span>`, ``) +
		card("Attachments", `<input id="support-attach-file" type="file" class="input" accept="image/jpeg,image/png,application/pdf"><div class="actions-row"><button id="support-add-attachment" class="btn" type="button">Attach to Last Ticket</button></div><pre class="log" id="support-attach-log"></pre>`, ``) +
		card("List + Retry Queue", `<div class="actions-row"><button id="support-refresh-list" class="ghost-btn" type="button">Refresh</button><button id="support-save-draft" class="ghost-btn" type="button">Save Draft</button><button id="support-restore-draft" class="ghost-btn" type="button">Restore Draft</button></div><div id="support-retry-state" class="muted">Retry queue: 0</div><pre class="log" id="support-ticket-list-log"></pre>`, `class="card span-all"`) +
		card("Conflict Resolution", field("support-update-ticket-id", "Ticket ID")+field("support-update-version", "Version")+fieldArea("support-update-description", "Description", 3)+`<div class="actions-row"><button id="support-update-submit" class="btn" type="button">Optimistic Update</button></div><p class="muted">On conflicts, choose merge/overwrite/discard from the prompt.</p><pre class="log" id="support-conflict-log"></pre>`, `class="card span-all"`) +
		`</section></main>`
	return appShell("Support", "support", body)
}

func InventoryPage() templ.Component {
	body := `<main class="page-wrap"><section class="card span-all"><div class="panel-head"><h1 class="page-title">Inventory Control</h1><div class="chip-row"><span class="chip">Deterministic Allocation</span><span class="chip">Low Stock Alerts</span><span class="chip">Auditable Ledger</span></div></div><p class="page-subtitle">Execute stock movements with guardrails and monitor reservation health in real time.</p></section><section class="module-grid">` +
		card("Stock Operations", selectField("inventory-op-type", "Operation", []opt{{"inbound", "Inbound"}, {"outbound", "Outbound"}, {"transfers", "Transfer"}})+field("inventory-sku", "SKU")+field("inventory-qty", "Quantity")+field("inventory-from-wh", "From Warehouse")+field("inventory-to-wh", "To Warehouse")+field("inventory-reason", "Reason Code")+`<div class="actions-row"><button class="btn" id="inventory-op-submit" type="button">Submit Operation</button></div><div class="log log-rich" id="inventory-op-log" role="region" aria-label="Operation result"></div>`, ``) +
		card("Cycle Count", field("cycle-wh", "Warehouse")+field("cycle-sku", "SKU")+field("cycle-counted", "Counted Qty")+field("cycle-reason", "Reason")+`<div class="actions-row"><button class="btn" id="inventory-cycle-submit" type="button">Submit Cycle Count</button></div><div class="log log-rich" id="inventory-cycle-log" role="region" aria-label="Cycle count log"></div>`, ``) +
		card("Balances + Reservations", idSelectField("inventory-res-order-id", "Order", "Select an order")+field("inventory-res-sku", "SKU")+field("inventory-res-qty", "Quantity")+field("inventory-res-site", "Site Code")+field("inventory-balance-site", "Balance Site Filter")+`<p class="muted">Site code is the business site (e.g. <code>SITE-A</code>), not a pick-zone or sub-warehouse label.</p><div class="actions-row"><button id="inventory-load-orders" class="ghost-btn" type="button">Load Orders</button><button id="inventory-refresh-balances" class="ghost-btn" type="button">Refresh Balances</button><button id="inventory-create-reservation" class="btn" type="button">Create Reservation</button><button id="inventory-refresh-reservations" class="ghost-btn" type="button">Refresh Reservations</button></div><div class="log log-rich" id="inventory-balances-log" role="region" aria-label="Balances"></div><div class="log log-rich" id="inventory-reservations-log" role="region" aria-label="Reservations"></div>`, `class="card span-all"`) +
		`</section></main>`
	return appShell("Inventory", "inventory", body)
}

func CompliancePage() templ.Component {
	body := `<main class="page-wrap"><section class="card span-all"><div class="panel-head"><h1 class="page-title">Compliance Center</h1><div class="chip-row"><span class="chip">Crawler Guardrails</span><span class="chip">Retention Controls</span><span class="chip">Step-up Protected</span></div></div><p class="page-subtitle">Separate routine monitoring from sensitive processing and export actions.</p></section><section class="module-grid">` +
		card("Crawler", `<div class="actions-row"><button id="compliance-crawler-run" class="btn" type="button">Run Crawler</button><button id="compliance-crawler-status" class="ghost-btn" type="button">Check Status</button></div><div class="log log-rich" id="compliance-crawler-log" role="region" aria-label="Crawler output"></div>`, ``) +
		card("Retention", `<div class="actions-row"><button id="compliance-retention-status" class="btn" type="button">Load Retention</button></div><div class="log log-rich" id="compliance-retention-log" role="region" aria-label="Retention summary"></div>`, ``) +
		card("Deletion Requests (Sensitive)", field("compliance-subject-ref", "Subject Ref")+`<div class="actions-row"><button id="compliance-delete-create" class="btn" type="button">Create Request</button><button id="compliance-list-delete" class="ghost-btn" type="button">List Requests</button></div>`+field("compliance-process-id", "Request ID")+`<div class="actions-row"><button id="compliance-delete-process" class="btn" type="button">Process Request</button></div><div class="log log-rich" id="compliance-delete-log" role="region" aria-label="Deletion requests"></div>`, `class="card span-all"`) +
		card("Audit Logs", field("compliance-audit-filter", "Action filter")+field("compliance-audit-page", "Page")+field("compliance-audit-limit", "Limit")+`<div class="actions-row"><button id="compliance-audit-load" class="ghost-btn" type="button">Load Audit Logs</button><button id="compliance-audit-export" class="btn" type="button">Export Audit Logs</button></div><div class="log log-rich" id="compliance-audit-log" role="region" aria-label="Audit logs"></div>`, `class="card span-all"`) +
		`</section></main>`
	return appShell("Compliance", "compliance", body)
}

func appShell(title, pageKey, body string) templ.Component {
	nav := `<header class="topbar"><nav class="topbar-left" aria-label="Primary"><a href="/dashboard" class="nav-link">Dashboard</a><a href="/hiring" class="nav-link" id="nav-hiring">Hiring</a><a href="/support" class="nav-link" id="nav-support">Support</a><a href="/inventory" class="nav-link" id="nav-inventory">Inventory</a><a href="/compliance" class="nav-link" id="nav-compliance">Compliance</a></nav><div class="topbar-actions"><button class="ghost-btn" id="logout-btn" type="button">Logout</button></div></header>`
	return shell("Meridian "+title, pageKey, nav+body)
}

func shell(title, pageKey, body string) templ.Component {
	assetVersion := "20260402g"
	head := fmt.Sprintf(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>%s</title><link rel="stylesheet" href="/static/css/theme.css?v=%s"><script defer src="/static/js/app.js?v=%s"></script></head><body class="bg-app text-body" data-page="%s">`, title, assetVersion, assetVersion, pageKey)
	return raw(head + body + `</body></html>`)
}

func card(title, content, attrs string) string {
	if strings.TrimSpace(attrs) == "" {
		attrs = `class="card"`
	}
	if !strings.Contains(attrs, "class=") {
		attrs = `class="card" ` + attrs
	}
	return fmt.Sprintf(`<section %s role="region" aria-label="%s"><h2>%s</h2>%s</section>`, attrs, title, title, content)
}

func field(id, label string) string {
	return fmt.Sprintf(`<label class="label" for="%s">%s</label><input id="%s" class="input">`, id, label, id)
}

func fieldArea(id, label string, rows int) string {
	return fmt.Sprintf(`<label class="label" for="%s">%s</label><textarea id="%s" class="input" rows="%d"></textarea>`, id, label, id, rows)
}

func jobSelectField(id, label string) string {
	return fmt.Sprintf(`<label class="label" for="%s">%s</label><select id="%s" class="input"><option value="">Select a job</option></select>`, id, label, id)
}

func idSelectField(id, label, placeholder string) string {
	return fmt.Sprintf(`<label class="label" for="%s">%s</label><select id="%s" class="input"><option value="">%s</option></select>`, id, label, id, placeholder)
}

type opt struct {
	Value string
	Label string
}

func selectField(id, label string, options []opt) string {
	b := strings.Builder{}
	b.WriteString(fmt.Sprintf(`<label class="label" for="%s">%s</label><select id="%s" class="input">`, id, label, id))
	for _, o := range options {
		b.WriteString(fmt.Sprintf(`<option value="%s">%s</option>`, o.Value, o.Label))
	}
	b.WriteString(`</select>`)
	return b.String()
}

func raw(html string) templ.Component {
	return templruntime.GeneratedTemplate(func(in templruntime.GeneratedComponentInput) error {
		_, err := in.Writer.Write([]byte(html))
		return err
	})
}
