(function () {
  const path = window.location.pathname;
  const routePath = path.replace(/\/+$/, "") || "/";
  const RETRY_KEY_BASE = "offline:retry_queue";
  const SUPPORT_DRAFT_KEY_BASE = "support:draft";
  const LAST_TICKET_KEY_BASE = "support:last_ticket_id";
  const AUTH_KEYS = {
    stepUp: "auth:step_up_token",
    userID: "auth:user_id",
    userName: "auth:user_name",
  };

  const session = {
    get(key) {
      try {
        return window.sessionStorage.getItem(key) || "";
      } catch {
        return "";
      }
    },
    set(key, val) {
      try {
        if (typeof val === "string" && val !== "")
          window.sessionStorage.setItem(key, val);
      } catch {}
    },
    remove(key) {
      try {
        window.sessionStorage.removeItem(key);
      } catch {}
    },
    clear() {
      try {
        window.sessionStorage.clear();
      } catch {}
    },
    stepUpToken() {
      return session.get(AUTH_KEYS.stepUp);
    },
    userID() {
      return session.get(AUTH_KEYS.userID);
    },
    userName() {
      return session.get(AUTH_KEYS.userName);
    },
    setUserContext(userID, userName) {
      session.set(AUTH_KEYS.userID, userID || "");
      session.set(AUTH_KEYS.userName, userName || "");
    },
    clearAuth() {
      session.remove(AUTH_KEYS.stepUp);
      session.remove(AUTH_KEYS.userID);
      session.remove(AUTH_KEYS.userName);
    },
  };

  function scopedKey(base, explicitUserID) {
    const uid = (explicitUserID || session.userID() || "anon").trim() || "anon";
    return `${base}:${uid}`;
  }

  function clearUserScopedCache(userID) {
    if (!userID) return;
    localStorage.removeItem(scopedKey(RETRY_KEY_BASE, userID));
    localStorage.removeItem(scopedKey(SUPPORT_DRAFT_KEY_BASE, userID));
    localStorage.removeItem(scopedKey(LAST_TICKET_KEY_BASE, userID));
  }

  function byId(id) {
    return document.getElementById(id);
  }

  function toast(msg, kind) {
    let el = byId("global-toast");
    if (!el) {
      el = document.createElement("div");
      el.id = "global-toast";
      el.className = "toast";
      document.body.appendChild(el);
    }
    el.className = `toast ${kind || "info"}`;
    el.textContent = msg;
    setTimeout(() => {
      if (el) el.textContent = "";
    }, 2600);
  }

  function setStatusBadge(id, text, kind) {
    const el = byId(id);
    if (!el) return;
    el.textContent = text;
    el.className = `status-badge ${kind || "info"}`;
  }

  function setVisibility(el, allowed, messageIfDenied) {
    if (!el) return;
    if (allowed) {
      el.removeAttribute("hidden");
      el.classList.remove("disabled");
      if ("disabled" in el) el.disabled = false;
      if (messageIfDenied && el.dataset.deniedMessage)
        el.dataset.deniedMessage = "";
    } else {
      el.setAttribute("hidden", "hidden");
      el.classList.add("disabled");
      if ("disabled" in el) el.disabled = true;
      if (messageIfDenied) el.dataset.deniedMessage = messageIfDenied;
    }
  }

  function can(perms, module, action) {
    return !!(perms && perms[module] && perms[module][action]);
  }

  function updateSessionLabel() {
    const el = byId("session-user");
    if (!el) return;
    const username = session.userName();
    if (!username) {
      el.textContent = "Unknown";
      return;
    }
    el.textContent = username;
  }

  function updateNetworkState() {
    const el = byId("network-state");
    if (el) el.textContent = navigator.onLine ? "Online" : "Offline";
  }

  function loadQueue() {
    try {
      return JSON.parse(
        localStorage.getItem(scopedKey(RETRY_KEY_BASE)) || "[]",
      );
    } catch {
      return [];
    }
  }

  function saveQueue(queue) {
    localStorage.setItem(scopedKey(RETRY_KEY_BASE), JSON.stringify(queue));
    const q1 = byId("queue-state");
    const q2 = byId("support-retry-state");
    if (q1) q1.textContent = String(queue.length);
    if (q2) q2.textContent = `Retry queue: ${queue.length}`;
  }

  function enqueueRetry(url, options) {
    const q = loadQueue();
    q.push({ url, options });
    saveQueue(q);
  }

  async function refreshAccessToken() {
    const payload = {};
    const res = await fetch("/rpc/refresh", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    return res.ok;
  }

  async function apiFetch(url, opts) {
    const options = opts || {};
    const method = options.method || "GET";
    const headers = { "Content-Type": "application/json" };
    if (options.idempotency) headers["Idempotency-Key"] = crypto.randomUUID();
    if (options.stepUp && session.stepUpToken()) {
      headers["X-Step-Up-Token"] = session.stepUpToken();
    }

    const req = {
      method,
      headers,
      body: method === "GET" ? undefined : JSON.stringify(options.body || {}),
    };

    let res = await fetch(url, req);
    if (res.status === 401 && options.auth !== false) {
      if (await refreshAccessToken()) {
        res = await fetch(url, req);
      }
    }

    const contentType = res.headers.get("Content-Type") || "";
    const body = contentType.includes("application/json")
      ? await res.json().catch(() => ({}))
      : await res.text().catch(() => "");

    if (options.stepUp && res.ok) {
      session.remove(AUTH_KEYS.stepUp);
    }
    if (!res.ok)
      throw {
        status: res.status,
        ...(typeof body === "object" ? body : { error: body }),
      };
    return body;
  }

  const apiGet = (url) => apiFetch(url, { method: "GET", auth: true });
  const apiPost = (url, body, extra) =>
    apiFetch(url, { method: "POST", body, auth: true, ...(extra || {}) });
  const apiPut = (url, body) =>
    apiFetch(url, { method: "PUT", body, auth: true });

  async function flushRetryQueue() {
    if (!navigator.onLine) return;
    const queue = loadQueue();
    if (!queue.length) return;
    const remaining = [];
    for (const item of queue) {
      try {
        await apiFetch(item.url, item.options || {});
      } catch {
        remaining.push(item);
      }
    }
    saveQueue(remaining);
    if (!remaining.length) toast("Retry queue flushed", "ok");
  }

  async function fetchUserContext() {
    try {
      const ctx = await apiGet("/rpc/api/auth/me");
      if (ctx && ctx.id) {
        session.setUserContext(String(ctx.id), String(ctx.username || ""));
        updateSessionLabel();
      }
      return ctx;
    } catch {
      return null;
    }
  }

  function applyModuleVisibility(ctx) {
    const perms = (ctx && ctx.permissions) || {};
    const moduleVisible = {
      hiring: can(perms, "hiring", "view") || can(perms, "hiring", "create"),
      support: can(perms, "support", "view") || can(perms, "support", "create"),
      inventory:
        can(perms, "inventory", "view") || can(perms, "inventory", "create"),
      compliance:
        can(perms, "compliance", "view") || can(perms, "compliance", "create"),
    };
    setVisibility(byId("nav-hiring"), moduleVisible.hiring);
    setVisibility(byId("nav-support"), moduleVisible.support);
    setVisibility(byId("nav-inventory"), moduleVisible.inventory);
    setVisibility(byId("nav-compliance"), moduleVisible.compliance);
    return { perms, moduleVisible };
  }

  function applyActionGates(perms, mapping) {
    mapping.forEach((m) => {
      const el = byId(m.id);
      const allowed = can(perms, m.module, m.action);
      setVisibility(el, allowed, `${m.module}:${m.action} required`);
    });
  }

  function logTo(id, msg) {
    const el = byId(id);
    if (!el) return;
    el.textContent = `[${new Date().toISOString()}] ${msg}\n` + el.textContent;
  }

  function escapeHtml(s) {
    return String(s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  function humanizeKey(k) {
    return String(k || "")
      .replace(/_/g, " ")
      .replace(/\b\w/g, (c) => c.toUpperCase());
  }

  function fmtTime(iso) {
    if (iso == null || iso === "") return "—";
    try {
      return new Date(iso).toLocaleString();
    } catch {
      return String(iso);
    }
  }

  function renderDetailJson(obj) {
    if (obj == null) return "";
    return `<details class="detail-json"><summary>Technical (JSON)</summary><pre class="detail-json-pre">${escapeHtml(JSON.stringify(obj, null, 2))}</pre></details>`;
  }

  function renderDl(obj) {
    const pairs = Object.entries(obj || {}).filter(
      ([, v]) => v !== undefined && v !== null && String(v) !== "",
    );
    if (!pairs.length)
      return `<p class="panel-empty">No details returned.</p>`;
    const rows = pairs
      .map(
        ([k, v]) =>
          `<dt>${escapeHtml(humanizeKey(k))}</dt><dd>${escapeHtml(typeof v === "object" ? JSON.stringify(v) : String(v))}</dd>`,
      )
      .join("");
    return `<dl class="result-kv">${rows}</dl>`;
  }

  function renderDataTable(columns, rows) {
    if (!rows.length)
      return `<p class="panel-empty">No rows.</p>`;
    const th = columns
      .map((c) => `<th>${escapeHtml(c.label)}</th>`)
      .join("");
    const tr = rows
      .map(
        (row) =>
          `<tr>${columns.map((c) => `<td>${escapeHtml(String(row[c.key] ?? "—"))}</td>`).join("")}</tr>`,
      )
      .join("");
    return `<div class="table-wrap"><table class="data-table"><thead><tr>${th}</tr></thead><tbody>${tr}</tbody></table></div>`;
  }

  function setRichPanel(id, innerHtml) {
    const el = byId(id);
    if (!el) return;
    el.innerHTML = innerHtml || "";
  }

  function populateJobDropdown(selectId, jobs, placeholder) {
    const el = byId(selectId);
    if (!el || el.tagName !== "SELECT") return;
    const prior = el.value;
    const first = placeholder || "Select a job";
    const opts = [`<option value="">${first}</option>`].concat(
      (jobs || []).map(
        (j) => `<option value="${j.id}">${j.code} - ${j.title}</option>`,
      ),
    );
    el.innerHTML = opts.join("");
    if (prior && (jobs || []).some((j) => j.id === prior)) {
      el.value = prior;
    } else if ((jobs || []).length > 0) {
      el.value = jobs[0].id;
    }
  }

  function populateOrderDropdown(selectId, orders, placeholder) {
    const el = byId(selectId);
    if (!el || el.tagName !== "SELECT") return;
    const prior = el.value;
    const first = placeholder || "Select an order";
    const rows = orders || [];
    const opts = [`<option value="">${first}</option>`].concat(
      rows.map(
        (o) =>
          `<option value="${o.id}">${o.id} | ${o.site_code || "SITE-A"} | ${o.customer_ref || "customer"}</option>`,
      ),
    );
    el.innerHTML = opts.join("");
    if (prior && rows.some((o) => o.id === prior)) {
      el.value = prior;
    } else if (rows.length > 0) {
      el.value = rows[0].id;
    }
  }

  function setSelectValue(selectId, value, labelFallback) {
    const el = byId(selectId);
    if (!el || el.tagName !== "SELECT") return;
    const next = (value || "").trim();
    if (!next) {
      el.value = "";
      return;
    }
    const exists = Array.from(el.options || []).some(
      (opt) => opt.value === next,
    );
    if (!exists) {
      const opt = document.createElement("option");
      opt.value = next;
      opt.textContent = labelFallback || next;
      el.appendChild(opt);
    }
    el.value = next;
  }

  function setJobDropdownMessage(stateId, message, kind) {
    const el = byId(stateId);
    if (!el) return;
    el.textContent = message;
    el.className = kind === "error" ? "error-text" : "muted";
  }

  function setJobActionEnabled(enabled) {
    const manualSubmit = byId("hiring-manual-submit");
    const csvImport = byId("csv-import-btn");
    if (manualSubmit) manualSubmit.disabled = !enabled;
    if (csvImport) csvImport.disabled = !enabled;
  }

  async function fetchJobsForDropdown(allowPublicFallback) {
    try {
      const res = await apiGet("/rpc/api/hiring/jobs");
      return { jobs: res.jobs || [], state: "ok", error: "" };
    } catch (err) {
      if (err.status === 401) {
        return {
          jobs: [],
          state: "unauthorized",
          error: err.error || "Session expired",
        };
      }
      if (err.status === 403) {
        try {
          const res = await apiGet("/rpc/api/hiring/jobs/for-intake");
          return { jobs: res.jobs || [], state: "ok", error: "" };
        } catch (createErr) {
          if (createErr.status === 401) {
            return {
              jobs: [],
              state: "unauthorized",
              error: createErr.error || "Session expired",
            };
          }
          return {
            jobs: [],
            state: "forbidden",
            error: createErr.error || err.error || "Access denied",
          };
        }
      }
      if (allowPublicFallback) {
        try {
          const res = await apiFetch("/rpc/kiosk/jobs", {
            method: "GET",
            auth: false,
          });
          return { jobs: res.jobs || [], state: "ok", error: "" };
        } catch (kioskErr) {
          return {
            jobs: [],
            state: "error",
            error: kioskErr.error || `HTTP ${kioskErr.status || 500}`,
          };
        }
      }
      return {
        jobs: [],
        state: "error",
        error: err.error || `HTTP ${err.status || 500}`,
      };
    }
  }

  function showModuleUnauthorized(moduleName) {
    const root = document.querySelector(
      "main.page-wrap, main.module-grid, main.dashboard-grid",
    );
    if (!root) return;
    root.className = "page-wrap";
    root.innerHTML = `<section class="card unauthorized-card"><h1 class="page-title">Access Restricted</h1><p class="page-subtitle">You do not have permission to open the ${moduleName} module.</p><div class="actions-row" style="justify-content:center"><a class="nav-link" href="/dashboard">Return to Dashboard</a></div></section>`;
  }

  function requestStepUpPassword(actionClass) {
    return new Promise((resolve, reject) => {
      const backdrop = document.createElement("div");
      backdrop.className = "stepup-backdrop";
      backdrop.innerHTML = `<section class="card stepup-modal" role="dialog" aria-modal="true" aria-labelledby="stepup-title"><h2 id="stepup-title">Step-up verification</h2><p class="muted">Re-enter your password to continue with <strong>${actionClass}</strong>.</p><label class="label" for="stepup-password">Password</label><input id="stepup-password" class="input" type="password" autocomplete="current-password"><p id="stepup-error" class="stepup-error" aria-live="polite"></p><div class="actions-row"><button type="button" class="ghost-btn" id="stepup-cancel">Cancel</button><button type="button" class="btn" id="stepup-submit">Verify</button></div></section>`;
      document.body.appendChild(backdrop);

      const pass = backdrop.querySelector("#stepup-password");
      const err = backdrop.querySelector("#stepup-error");
      const cleanup = () => {
        backdrop.remove();
        document.removeEventListener("keydown", onEsc);
      };
      const onEsc = (e) => {
        if (e.key === "Escape") {
          cleanup();
          reject(new Error("step-up cancelled"));
        }
      };
      document.addEventListener("keydown", onEsc);

      backdrop.querySelector("#stepup-cancel").onclick = () => {
        cleanup();
        reject(new Error("step-up cancelled"));
      };

      backdrop.querySelector("#stepup-submit").onclick = () => {
        const value = (pass.value || "").trim();
        if (!value) {
          err.textContent = "Password is required.";
          pass.focus();
          return;
        }
        cleanup();
        resolve(value);
      };

      pass.addEventListener("keydown", (e) => {
        if (e.key === "Enter") backdrop.querySelector("#stepup-submit").click();
      });
      pass.focus();
    });
  }

  async function loginFlow(e) {
    e.preventDefault();
    const username = byId("username").value.trim();
    const password = byId("password").value;
    const err = byId("login-error");
    err.textContent = "";
    try {
      await apiFetch("/rpc/login", {
        method: "POST",
        auth: false,
        body: { username, password },
      });
      const oldUser = session.userID();
      const ctx = await fetchUserContext();
      const newUser = ctx && ctx.id ? String(ctx.id) : "";
      if (oldUser && newUser && oldUser !== newUser)
        clearUserScopedCache(oldUser);
      window.location.href = "/dashboard";
    } catch (ex) {
      err.textContent = ex.error || "Login failed";
    }
  }

  async function stepUp(actionClass) {
    const pw = await requestStepUpPassword(actionClass);
    const res = await apiPost("/rpc/api/auth/step-up", {
      password: pw,
      action_class: actionClass,
    });
    session.set(AUTH_KEYS.stepUp, res.step_up_token || "");
    return res.step_up_token;
  }

  function wireGlobal() {
    updateSessionLabel();
    updateNetworkState();
    saveQueue(loadQueue());

    const logout = byId("logout-btn");
    if (logout) {
      logout.addEventListener("click", async () => {
        try {
          await apiFetch("/rpc/logout", {
            method: "POST",
            auth: false,
            body: {},
          });
        } catch {}
        clearUserScopedCache(session.userID());
        session.clearAuth();
        window.location.href = "/";
      });
    }

    window.addEventListener("online", async () => {
      updateNetworkState();
      await flushRetryQueue();
    });
    window.addEventListener("offline", updateNetworkState);
    setInterval(flushRetryQueue, 15000);
  }

  async function initDashboard(ctx) {
    const { perms, moduleVisible } = applyModuleVisibility(ctx);
    setVisibility(byId("card-hiring"), moduleVisible.hiring);
    setVisibility(byId("card-support"), moduleVisible.support);
    setVisibility(byId("card-inventory"), moduleVisible.inventory);
    setVisibility(byId("card-compliance"), moduleVisible.compliance);

    applyActionGates(perms, [
      { id: "create-job-btn", module: "hiring", action: "create" },
      { id: "create-kiosk-app-btn", module: "hiring", action: "create" },
      { id: "create-ticket-btn", module: "support", action: "create" },
      { id: "load-ticket-btn", module: "support", action: "view" },
      { id: "create-reservation-btn", module: "inventory", action: "create" },
      { id: "inventory-balance-btn", module: "inventory", action: "view" },
      { id: "run-crawler-btn", module: "compliance", action: "create" },
      { id: "retention-status-btn", module: "compliance", action: "view" },
    ]);
  }

  function validateCsv(csv) {
    const rows = csv.split(/\r?\n/).filter((r) => r.trim() !== "");
    const errors = [];
    for (let i = 1; i < rows.length; i++) {
      const cols = rows[i].split(",");
      if (cols.length < 3) errors.push(`row ${i + 1}: expected 3 cols`);
      if (cols[1] && !cols[1].includes("@"))
        errors.push(`row ${i + 1}: bad email`);
    }
    return errors;
  }

  async function initHiring(ctx) {
    const { perms, moduleVisible } = applyModuleVisibility(ctx);
    if (!moduleVisible.hiring) {
      showModuleUnauthorized("Hiring");
      return;
    }

    applyActionGates(perms, [
      { id: "hiring-create-job", module: "hiring", action: "create" },
      { id: "hiring-manual-submit", module: "hiring", action: "create" },
      { id: "kiosk-submit-btn", module: "hiring", action: "create" },
      { id: "csv-import-btn", module: "hiring", action: "create" },
      { id: "pipeline-save-btn", module: "hiring", action: "update" },
      { id: "pipeline-validate-btn", module: "hiring", action: "update" },
      { id: "pipeline-transition-btn", module: "hiring", action: "update" },
      { id: "load-applications-btn", module: "hiring", action: "view" },
      { id: "load-timeline-btn", module: "hiring", action: "view" },
    ]);

    const kioskURL = `${window.location.origin}/hiring/kiosk`;
    const qr = byId("hiring-kiosk-qr");
    if (qr) qr.src = `/hiring/kiosk/qr?url=${encodeURIComponent(kioskURL)}`;
    const kioskLink = byId("hiring-kiosk-url");
    if (kioskLink) kioskLink.href = kioskURL;

    async function refreshJobDropdowns() {
      const hasManual = !!byId("manual-job-id");
      const hasCSV = !!byId("csv-job-id");
      if (!hasManual && !hasCSV) return;
      const result = await fetchJobsForDropdown(false);
      if (result.state === "ok") {
        const jobs = result.jobs || [];
        const placeholder = jobs.length ? "Select a job" : "No jobs available";
        if (hasManual) populateJobDropdown("manual-job-id", jobs, placeholder);
        if (hasCSV) populateJobDropdown("csv-job-id", jobs, placeholder);
        setJobDropdownMessage(
          "manual-job-state",
          jobs.length ? `${jobs.length} job(s) available` : "No jobs available",
          jobs.length ? "info" : "error",
        );
        setJobActionEnabled(jobs.length > 0);
        setVisibility(byId("manual-job-retry"), false);
        return;
      }
      let placeholder = "Unable to load jobs";
      let stateMessage = result.error || "Unable to load jobs";
      let allowRetry = false;
      if (result.state === "unauthorized") {
        placeholder = "Session expired while loading jobs";
        stateMessage = "Session expired while loading jobs";
      } else if (result.state === "forbidden") {
        placeholder = "Access denied to job list";
        stateMessage = "Access denied to job list";
      } else {
        placeholder = "Failed to load jobs (Retry)";
        stateMessage = "Failed to load jobs";
        allowRetry = true;
      }
      if (hasManual) populateJobDropdown("manual-job-id", [], placeholder);
      if (hasCSV) populateJobDropdown("csv-job-id", [], placeholder);
      setJobDropdownMessage("manual-job-state", stateMessage, "error");
      setJobActionEnabled(false);
      setVisibility(byId("manual-job-retry"), allowRetry);
      logTo(
        "hiring-jobs-log",
        `Job dropdown load failed: ${result.error || result.state}`,
      );
    }

    function populateApplicationDropdown(selectId, apps, placeholder) {
      const el = byId(selectId);
      if (!el || el.tagName !== "SELECT") return;
      const prior = el.value;
      const first = placeholder || "Select an application";
      const rows = apps || [];
      const opts = [`<option value="">${first}</option>`].concat(
        rows.map(
          (a) =>
            `<option value="${a.application_id}">${a.application_id} - ${a.candidate?.full_name || "Candidate"}</option>`,
        ),
      );
      el.innerHTML = opts.join("");
      if (prior && rows.some((a) => a.application_id === prior)) {
        el.value = prior;
      }
    }

    async function refreshApplicationDropdowns() {
      const hasTransition = !!byId("transition-app-id");
      const hasTimeline = !!byId("timeline-app-id");
      if (!hasTransition && !hasTimeline) return;
      try {
        const res = await apiGet("/rpc/api/hiring/applications");
        const apps = res.applications || [];
        const placeholder = apps.length
          ? "Select an application"
          : "No applications available";
        if (hasTransition)
          populateApplicationDropdown("transition-app-id", apps, placeholder);
        if (hasTimeline)
          populateApplicationDropdown("timeline-app-id", apps, placeholder);
      } catch (err) {
        if (hasTransition)
          populateApplicationDropdown(
            "transition-app-id",
            [],
            "Unable to load applications",
          );
        if (hasTimeline)
          populateApplicationDropdown(
            "timeline-app-id",
            [],
            "Unable to load applications",
          );
        logTo(
          "hiring-applications-log",
          `Application dropdown load failed: ${err.error || err.status}`,
        );
      }
    }

    await refreshJobDropdowns();
    await refreshApplicationDropdowns();

    byId("hiring-create-job").onclick = async () => {
      const payload = {
        code: byId("job-code").value.trim(),
        title: byId("job-title").value.trim(),
        description: byId("job-description").value.trim(),
        site_code: "SITE-A",
      };
      try {
        const res = await apiPost("/rpc/api/hiring/jobs", payload);
        byId("hiring-job-msg").textContent = `Created job ${res.id}`;
        await refreshJobDropdowns();
      } catch (err) {
        byId("hiring-job-msg").textContent = err.error || "Create failed";
      }
    };

    byId("hiring-load-jobs").onclick = async () => {
      try {
        const res = await apiGet("/rpc/api/hiring/jobs");
        const lines = (res.jobs || []).map(
          (j) => `${j.id} | ${j.code} | ${j.title}`,
        );
        byId("hiring-jobs-log").textContent = lines.join("\n") || "No jobs";
        populateJobDropdown("manual-job-id", res.jobs || []);
        populateJobDropdown("csv-job-id", res.jobs || []);
      } catch (err) {
        byId("hiring-jobs-log").textContent = err.error || "Load jobs failed";
      }
    };
    const retryJobs = byId("manual-job-retry");
    if (retryJobs) retryJobs.onclick = refreshJobDropdowns;

    async function submitIntake(endpoint) {
      const payload = {
        job_id: byId("manual-job-id").value.trim(),
        full_name: byId("manual-name").value.trim(),
        email: byId("manual-email").value.trim(),
        phone: byId("manual-phone").value.trim(),
        ssn: byId("manual-ssn").value.trim(),
      };
      try {
        const res = await apiPost(endpoint, payload);
        byId("hiring-intake-msg").textContent =
          `Application ${res.application_id}, risk=${res.risk_score}`;
        const severity = (res.severity || "info").toLowerCase();
        setStatusBadge(
          "hiring-rule-status",
          `Rule outcome: ${severity}; triggers=${(res.rule_triggers || []).join(",")}`,
          severity === "block"
            ? "error"
            : severity === "warn"
              ? "warn"
              : "info",
        );
        await refreshApplicationDropdowns();
      } catch (err) {
        byId("hiring-intake-msg").textContent = err.error || "Intake failed";
        setStatusBadge(
          "hiring-rule-status",
          `Rule failure: ${err.error || err.status}`,
          "error",
        );
      }
    }
    byId("hiring-manual-submit").onclick = () =>
      submitIntake("/rpc/api/hiring/applications/manual");
    byId("kiosk-submit-btn").onclick = () =>
      submitIntake("/rpc/api/hiring/applications/kiosk");

    byId("csv-validate-btn").onclick = () => {
      const errs = validateCsv(byId("csv-text").value);
      byId("csv-validation-log").textContent = errs.length
        ? errs.join("\n")
        : "CSV validation passed";
    };
    byId("csv-import-btn").onclick = async () => {
      const csv = byId("csv-text").value;
      const errs = validateCsv(csv);
      if (errs.length) {
        byId("csv-validation-log").textContent = errs.join("\n");
        return;
      }
      try {
        const res = await apiPost("/rpc/api/hiring/applications/import-csv", {
          job_id: byId("csv-job-id").value.trim(),
          csv,
        });
        byId("csv-validation-log").textContent = `Imported ${res.created} rows`;
        await refreshApplicationDropdowns();
      } catch (err) {
        byId("csv-validation-log").textContent =
          err.error || "CSV import failed";
      }
    };

    byId("pipeline-validate-btn").onclick = async () => {
      try {
        const stages = JSON.parse(byId("pipeline-stages-json").value || "[]");
        const transitions = JSON.parse(
          byId("pipeline-transitions-json").value || "[]",
        );
        await apiPost("/rpc/api/hiring/pipelines/validate", {
          code: byId("pipeline-code").value.trim(),
          name: byId("pipeline-name").value.trim(),
          stages,
          transitions,
        });
        logTo("pipeline-config-log", "Pipeline definition valid");
      } catch (err) {
        logTo(
          "pipeline-config-log",
          `Validation failed: ${err.error || err.status}`,
        );
      }
    };

    byId("pipeline-save-btn").onclick = async () => {
      try {
        const stages = JSON.parse(byId("pipeline-stages-json").value || "[]");
        const transitions = JSON.parse(
          byId("pipeline-transitions-json").value || "[]",
        );
        const res = await apiPost("/rpc/api/hiring/pipelines/templates", {
          code: byId("pipeline-code").value.trim(),
          name: byId("pipeline-name").value.trim(),
          stages,
          transitions,
        });
        logTo("pipeline-config-log", `Saved template ${res.id}`);
      } catch (err) {
        logTo("pipeline-config-log", `Save failed: ${err.error || err.status}`);
      }
    };

    byId("pipeline-load-btn").onclick = async () => {
      try {
        const res = await apiGet("/rpc/api/hiring/pipelines/templates");
        const lines = (res.templates || []).map(
          (t) => `${t.id} | ${t.code} | ${t.name} | active=${t.active}`,
        );
        logTo("pipeline-config-log", lines.join("\n") || "No templates");
      } catch (err) {
        logTo(
          "pipeline-config-log",
          `Load templates failed: ${err.error || err.status}`,
        );
      }
    };

    byId("load-allowed-transitions-btn").onclick = async () => {
      const appID = byId("transition-app-id").value.trim();
      if (!appID) return;
      try {
        const res = await apiGet(
          `/rpc/api/hiring/applications/${appID}/allowed-transitions`,
        );
        const lines = (res.allowed_transitions || []).map(
          (t) =>
            `${res.current_stage} -> ${t.to_stage} | req=${t.required_fields || "-"} | rule=${t.screening_rule || "-"}`,
        );
        byId("allowed-transitions-log").textContent =
          lines.join("\n") || "No transitions";
      } catch (err) {
        byId("allowed-transitions-log").textContent =
          err.error || "Failed to load allowed transitions";
      }
    };

    byId("pipeline-transition-btn").onclick = async () => {
      const appID = byId("transition-app-id").value.trim();
      let fields;
      try {
        fields = JSON.parse(byId("transition-required-json").value || "{}");
      } catch {
        toast("Required fields JSON is invalid", "error");
        return;
      }
      try {
        await apiPost(`/rpc/api/hiring/applications/${appID}/transition`, {
          from_stage: byId("transition-from").value.trim(),
          to_stage: byId("transition-to").value.trim(),
          fields,
        });
        logTo("hiring-applications-log", `Transitioned application ${appID}`);
        await refreshApplicationDropdowns();
      } catch (err) {
        logTo(
          "hiring-applications-log",
          `Transition failed: ${err.error || err.status}`,
        );
      }
    };

    byId("load-applications-btn").onclick = async () => {
      try {
        const res = await apiGet("/rpc/api/hiring/applications");
        const lines = (res.applications || []).map(
          (a) =>
            `${a.application_id} | stage=${a.stage_code} | ${a.candidate.full_name} | risk=${a.risk_score}`,
        );
        byId("hiring-applications-log").textContent =
          lines.join("\n") || "No applications";
        const placeholder = (res.applications || []).length
          ? "Select an application"
          : "No applications available";
        populateApplicationDropdown(
          "transition-app-id",
          res.applications || [],
          placeholder,
        );
        populateApplicationDropdown(
          "timeline-app-id",
          res.applications || [],
          placeholder,
        );
      } catch (err) {
        byId("hiring-applications-log").textContent =
          err.error || "Failed to load applications";
      }
    };

    byId("load-timeline-btn").onclick = async () => {
      const appID = byId("timeline-app-id").value.trim();
      if (!appID) return;
      try {
        const res = await apiGet(
          `/rpc/api/hiring/applications/${appID}/events`,
        );
        const lines = (res.events || []).map(
          (e) =>
            `${e.at} | ${e.event_type} | ${e.from || "-"} -> ${e.to || "-"}\n${e.details}`,
        );
        byId("hiring-timeline-log").textContent =
          lines.join("\n\n") || "No events";
      } catch (err) {
        byId("hiring-timeline-log").textContent =
          err.error || "Failed to load timeline";
      }
    };
  }

  async function initKiosk() {
    const submit = byId("kiosk-page-submit");
    if (!submit) return;
    const msg = byId("kiosk-page-msg");

    const result = await fetchJobsForDropdown(true);
    if (result.state === "ok") {
      const jobs = result.jobs || [];
      populateJobDropdown(
        "kiosk-job-id",
        jobs,
        jobs.length ? "Select a job" : "No jobs available",
      );
      submit.disabled = jobs.length === 0;
      if (!jobs.length && msg)
        msg.textContent = "No kiosk jobs are currently available.";
    } else {
      populateJobDropdown("kiosk-job-id", [], "Unable to load jobs");
      submit.disabled = true;
      if (msg) msg.textContent = result.error || "Unable to load kiosk jobs.";
    }

    submit.onclick = async () => {
      const payload = {
        job_id: byId("kiosk-job-id").value.trim(),
        full_name: byId("kiosk-name").value.trim(),
        email: byId("kiosk-email").value.trim(),
        phone: byId("kiosk-phone").value.trim(),
        ssn: byId("kiosk-ssn").value.trim(),
      };
      try {
        const res = await apiFetch("/rpc/kiosk/applications", {
          method: "POST",
          auth: false,
          body: payload,
        });
        msg.textContent = `Application captured: ${res.application_id}`;
        toast("Kiosk submission saved", "ok");
      } catch (err) {
        msg.textContent = err.error || "Kiosk submission failed";
      }
    };
  }

  async function initSupport(ctx) {
    const { perms, moduleVisible } = applyModuleVisibility(ctx);
    if (!moduleVisible.support) {
      showModuleUnauthorized("Support");
      return;
    }
    applyActionGates(perms, [
      { id: "support-create-ticket", module: "support", action: "create" },
      { id: "support-load-orders", module: "support", action: "view" },
      { id: "support-add-attachment", module: "support", action: "update" },
      { id: "support-refresh-list", module: "support", action: "view" },
      { id: "support-update-submit", module: "support", action: "update" },
    ]);

    async function refreshOrders() {
      try {
        let res;
        try {
          res = await apiGet("/rpc/api/support/orders");
        } catch (err) {
          if (err.status === 403) {
            res = await apiGet("/rpc/api/support/orders/for-intake");
          } else {
            throw err;
          }
        }
        const orders = res.orders || [];
        const placeholder = orders.length
          ? "Select an order"
          : "No orders available";
        populateOrderDropdown("support-order-id", orders, placeholder);
        const lines = orders.map(
          (o) =>
            `${o.id} | ${o.site_code || "SITE-A"} | ${o.customer_ref || "customer"}`,
        );
        byId("support-orders-log").textContent =
          lines.join("\n") || "No orders available";
      } catch (err) {
        populateOrderDropdown("support-order-id", [], "Unable to load orders");
        byId("support-orders-log").textContent =
          err.error || "Failed to load orders";
      }
    }

    byId("support-load-orders").onclick = refreshOrders;
    await refreshOrders();

    byId("support-create-ticket").onclick = async () => {
      const payload = {
        order_id: byId("support-order-id").value.trim(),
        ticket_type: byId("support-ticket-type").value,
        priority: byId("support-priority").value,
        description: byId("support-description").value,
        business_site: "SITE-A",
      };
      if (!payload.order_id) {
        byId("support-ticket-msg").textContent =
          "Select an order before creating a ticket.";
        return;
      }
      const optimisticID = "optimistic-" + Date.now();
      logTo("support-ticket-list-log", `Optimistic ${optimisticID} created`);
      try {
        const res = await apiPost("/rpc/api/support/tickets", payload);
        localStorage.setItem(scopedKey(LAST_TICKET_KEY_BASE), res.id);
        byId("support-ticket-msg").textContent =
          `Created ticket ${res.id}; SLA due ${res.sla_due_at}; escalated=${res.eligibility ? "no" : "unknown"}`;
      } catch (err) {
        logTo(
          "support-ticket-list-log",
          `Rollback ${optimisticID}: ${err.error || err.status}`,
        );
        enqueueRetry("/rpc/api/support/tickets", {
          method: "POST",
          body: payload,
          auth: true,
        });
      }
    };

    byId("support-save-draft").onclick = () => {
      localStorage.setItem(
        scopedKey(SUPPORT_DRAFT_KEY_BASE),
        JSON.stringify({
          order_id: byId("support-order-id").value,
          ticket_type: byId("support-ticket-type").value,
          priority: byId("support-priority").value,
          description: byId("support-description").value,
        }),
      );
      setStatusBadge("support-draft-status", "Draft saved", "info");
      toast("Draft saved", "ok");
    };

    byId("support-restore-draft").onclick = () => {
      try {
        const d = JSON.parse(
          localStorage.getItem(scopedKey(SUPPORT_DRAFT_KEY_BASE)) || "{}",
        );
        setSelectValue("support-order-id", d.order_id || "", d.order_id || "");
        byId("support-ticket-type").value =
          d.ticket_type || "return_and_refund";
        byId("support-priority").value = d.priority || "STANDARD";
        byId("support-description").value = d.description || "";
        setStatusBadge("support-draft-status", "Draft restored", "ok");
      } catch {
        setStatusBadge("support-draft-status", "No valid draft", "warn");
      }
    };

    byId("support-refresh-list").onclick = async () => {
      try {
        const list = await apiGet("/rpc/api/support/tickets");
        const lines = (list.tickets || []).map(
          (t) =>
            `${t.id} | ${t.status} | SLA=${t.sla_seconds}s | escalated=${t.escalated}`,
        );
        byId("support-ticket-list-log").textContent =
          lines.join("\n") || "No tickets";
      } catch (err) {
        byId("support-ticket-list-log").textContent =
          err.error || "Failed to load tickets";
      }
    };

    byId("support-add-attachment").onclick = async () => {
      const ticketID =
        localStorage.getItem(scopedKey(LAST_TICKET_KEY_BASE)) ||
        prompt("Ticket ID");
      if (!ticketID) return;
      const input = byId("support-attach-file");
      const file = input.files && input.files[0];
      if (!file) return logTo("support-attach-log", "Select a file first");
      const allowed = ["image/jpeg", "image/png", "application/pdf"];
      if (!allowed.includes(file.type))
        return logTo("support-attach-log", "Only JPG/PNG/PDF accepted");
      if (file.size > 10 * 1024 * 1024)
        return logTo("support-attach-log", "File exceeds 10MB");
      const hash = await crypto.subtle.digest(
        "SHA-256",
        await file.arrayBuffer(),
      );
      const checksum = Array.from(new Uint8Array(hash))
        .map((b) => b.toString(16).padStart(2, "0"))
        .join("");
      const bytes = new Uint8Array(await file.arrayBuffer());
      let binary = "";
      const chunkSize = 0x8000;
      for (let i = 0; i < bytes.length; i += chunkSize) {
        binary += String.fromCharCode(...bytes.subarray(i, i + chunkSize));
      }
      const contentBase64 = btoa(binary);
      try {
        await apiPost(`/rpc/api/support/tickets/${ticketID}/attachments`, {
          file_name: file.name,
          mime_type: file.type,
          size_mb: Math.max(1, Math.ceil(file.size / (1024 * 1024))),
          size_bytes: file.size,
          checksum,
          content_base64: contentBase64,
        });
        logTo("support-attach-log", `Attachment accepted ${file.name}`);
      } catch (err) {
        logTo(
          "support-attach-log",
          `Attachment failed ${err.error || err.status}`,
        );
      }
    };

    byId("support-update-submit").onclick = async () => {
      const ticketID = byId("support-update-ticket-id").value.trim();
      const version = Number(byId("support-update-version").value || "1");
      const description = byId("support-update-description").value;
      try {
        const res = await apiPut(`/rpc/api/support/tickets/${ticketID}`, {
          description,
          record_version: version,
        });
        logTo(
          "support-conflict-log",
          `Update succeeded version=${res.record_version}`,
        );
      } catch (err) {
        if (err.status !== 409) {
          logTo(
            "support-conflict-log",
            `Update failed: ${err.error || err.status}`,
          );
          return;
        }
        const choice = (
          prompt(
            "Version conflict. Choose merge, overwrite, or discard.",
            "merge",
          ) || "discard"
        ).toLowerCase();
        if (choice === "discard") {
          logTo(
            "support-conflict-log",
            "Conflict discarded. Latest server version kept.",
          );
          return;
        }
        try {
          const r = await apiPost(
            `/rpc/api/support/tickets/${ticketID}/conflict-resolve`,
            {
              mode: choice,
              current_version: err.current_version || version,
              expected_version: version,
              description,
            },
          );
          logTo(
            "support-conflict-log",
            `Conflict resolved (${choice}): ${JSON.stringify(r)}`,
          );
        } catch (e2) {
          logTo(
            "support-conflict-log",
            `Conflict resolution failed: ${e2.error || e2.status}`,
          );
        }
      }
    };
  }

  async function initInventory(ctx) {
    const { perms, moduleVisible } = applyModuleVisibility(ctx);
    if (!moduleVisible.inventory) {
      showModuleUnauthorized("Inventory");
      return;
    }
    applyActionGates(perms, [
      { id: "inventory-op-submit", module: "inventory", action: "create" },
      { id: "inventory-cycle-submit", module: "inventory", action: "create" },
      { id: "inventory-load-orders", module: "inventory", action: "view" },
      { id: "inventory-refresh-balances", module: "inventory", action: "view" },
      {
        id: "inventory-create-reservation",
        module: "inventory",
        action: "create",
      },
      {
        id: "inventory-refresh-reservations",
        module: "inventory",
        action: "view",
      },
    ]);

    async function refreshInventoryOrders() {
      try {
        let res;
        try {
          res = await apiGet("/rpc/api/inventory/orders");
        } catch (err) {
          if (err.status === 403) {
            res = await apiGet("/rpc/api/inventory/orders/for-intake");
          } else {
            throw err;
          }
        }
        const orders = res.orders || [];
        const placeholder = orders.length
          ? "Select an order"
          : "No orders available";
        populateOrderDropdown("inventory-res-order-id", orders, placeholder);
      } catch (err) {
        populateOrderDropdown(
          "inventory-res-order-id",
          [],
          "Unable to load orders",
        );
        setRichPanel(
          "inventory-reservations-log",
          `<p class="panel-error">Could not load orders: ${escapeHtml(err.error || err.status || "unknown")}</p>`,
        );
      }
    }

    byId("inventory-load-orders").onclick = refreshInventoryOrders;
    await refreshInventoryOrders();

    byId("inventory-op-submit").onclick = async () => {
      const op = byId("inventory-op-type").value;
      const payload = {
        sku: byId("inventory-sku").value.trim(),
        quantity: Number(byId("inventory-qty").value || "0"),
        from_warehouse: byId("inventory-from-wh").value.trim(),
        to_warehouse: byId("inventory-to-wh").value.trim(),
        reason_code: byId("inventory-reason").value.trim(),
      };
      if (!confirm(`Confirm ${op} for ${payload.sku}?`)) return;
      try {
        const res = await apiPost(`/rpc/api/inventory/${op}`, payload);
        const summary =
          res && typeof res === "object"
            ? renderDl({
                Movement: res.movement,
                Ok: res.ok,
              }) + renderDetailJson(res)
            : renderDetailJson(res);
        setRichPanel(
          "inventory-op-log",
          `<p class="panel-headline">Stock operation succeeded</p>${summary}`,
        );
        await byId("inventory-refresh-balances").onclick();
      } catch (err) {
        setRichPanel(
          "inventory-op-log",
          `<p class="panel-error">${escapeHtml(err.error || `failed ${err.status}`)}</p>`,
        );
      }
    };

    byId("inventory-cycle-submit").onclick = async () => {
      const payload = {
        warehouse_code: byId("cycle-wh").value.trim(),
        sku: byId("cycle-sku").value.trim(),
        counted_qty: Number(byId("cycle-counted").value || "0"),
        reason_code: byId("cycle-reason").value.trim(),
      };
      if (!confirm("Confirm cycle count?")) return;
      try {
        const res = await apiPost("/rpc/api/inventory/cycle-counts", payload);
        const pct =
          res.variance_percent != null
            ? Number(res.variance_percent).toFixed(2) + "%"
            : "—";
        setRichPanel(
          "inventory-cycle-log",
          `<p class="panel-headline">Cycle count recorded</p>${renderDl({
            "Session id": res.session_id,
            "Needs approval": res.requires_approval ? "Yes" : "No",
            "Variance": pct,
          })}${renderDetailJson(res)}`,
        );
      } catch (err) {
        setRichPanel(
          "inventory-cycle-log",
          `<p class="panel-error">${escapeHtml(err.error || "cycle failed")}</p>`,
        );
      }
    };

    byId("inventory-refresh-balances").onclick = async () => {
      try {
        const site = (
          byId("inventory-balance-site")?.value ||
          byId("inventory-res-site")?.value ||
          "SITE-A"
        ).trim();
        const res = await apiGet(
          `/rpc/api/inventory/balances?site=${encodeURIComponent(site)}`,
        );
        const balances = res.balances || [];
        const rows = balances.map((b) => ({
          warehouse: b.warehouse,
          zone: b.sub_warehouse || "—",
          sku: b.sku,
          available: b.available,
          on_hand: b.on_hand,
          reserved: b.reserved,
          safety: b.safety_stock,
          low: b.low_stock_alert ? "Yes" : "No",
        }));
        const table = renderDataTable(
          [
            { key: "warehouse", label: "Warehouse" },
            { key: "zone", label: "Zone" },
            { key: "sku", label: "SKU" },
            { key: "available", label: "Available" },
            { key: "on_hand", label: "On hand" },
            { key: "reserved", label: "Reserved" },
            { key: "safety", label: "Safety target" },
            { key: "low", label: "Low?" },
          ],
          rows,
        );
        setRichPanel(
          "inventory-balances-log",
          `<p class="panel-headline">Balances for ${escapeHtml(site)} (${balances.length})</p>${table}${renderDetailJson(res)}`,
        );
      } catch (err) {
        setRichPanel(
          "inventory-balances-log",
          `<p class="panel-error">${escapeHtml(err.error || "Load balances failed")}</p>`,
        );
      }
    };

    byId("inventory-create-reservation").onclick = async () => {
      const payload = {
        order_id: byId("inventory-res-order-id").value.trim(),
        sku: byId("inventory-res-sku").value.trim(),
        quantity: Number(byId("inventory-res-qty").value || "0"),
        site_code: byId("inventory-res-site").value.trim() || "SITE-A",
      };
      if (!payload.order_id || !payload.sku || payload.quantity <= 0) {
        setRichPanel(
          "inventory-reservations-log",
          `<p class="panel-error">Choose an order, enter a SKU, and a quantity greater than zero.</p>`,
        );
        return;
      }
      if (
        !confirm(
          `Create reservation for ${payload.order_id} (${payload.sku} x ${payload.quantity})?`,
        )
      )
        return;
      try {
        const res = await apiPost(
          "/rpc/api/inventory/reservations/order-create",
          payload,
          { idempotency: true },
        );
        setRichPanel(
          "inventory-reservations-log",
          `<p class="panel-headline">Reservation created</p>${renderDl({
            Id: res.id,
            Status: res.status,
            Warehouse: res.warehouse,
            Deterministic: res.deterministic,
          })}${renderDetailJson(res)}`,
        );
        await byId("inventory-refresh-reservations").onclick();
      } catch (err) {
        setRichPanel(
          "inventory-reservations-log",
          `<p class="panel-error">${escapeHtml(err.error || "reservation failed")}</p>`,
        );
      }
    };

    byId("inventory-refresh-reservations").onclick = async () => {
      try {
        const res = await apiGet("/rpc/api/inventory/reservations");
        const list = res.reservations || [];
        const rows = list.map((r) => ({
          id: r.id || "—",
          order: r.order_id,
          sku: r.sku,
          wh: r.warehouse_code,
          qty: r.reserved_qty,
          status: r.status,
          hold: fmtTime(r.hold_expires_at),
        }));
        const table = renderDataTable(
          [
            { key: "id", label: "Reservation" },
            { key: "order", label: "Order" },
            { key: "sku", label: "SKU" },
            { key: "wh", label: "WH" },
            { key: "qty", label: "Qty" },
            { key: "status", label: "Status" },
            { key: "hold", label: "Hold until" },
          ],
          rows,
        );
        setRichPanel(
          "inventory-reservations-log",
          `<p class="panel-headline">Reservations (${list.length})</p>${table}${renderDetailJson(res)}`,
        );
      } catch (err) {
        setRichPanel(
          "inventory-reservations-log",
          `<p class="panel-error">${escapeHtml(err.error || "Load reservations failed")}</p>`,
        );
      }
    };
  }

  async function initCompliance(ctx) {
    const { perms, moduleVisible } = applyModuleVisibility(ctx);
    if (!moduleVisible.compliance) {
      showModuleUnauthorized("Compliance");
      return;
    }

    applyActionGates(perms, [
      { id: "compliance-crawler-run", module: "compliance", action: "create" },
      { id: "compliance-crawler-status", module: "compliance", action: "view" },
      {
        id: "compliance-retention-status",
        module: "compliance",
        action: "view",
      },
      {
        id: "compliance-delete-create",
        module: "compliance",
        action: "create",
      },
      { id: "compliance-list-delete", module: "compliance", action: "view" },
      {
        id: "compliance-delete-process",
        module: "compliance",
        action: "approve",
      },
      { id: "compliance-audit-load", module: "compliance", action: "view" },
      { id: "compliance-audit-export", module: "compliance", action: "export" },
    ]);

    byId("compliance-crawler-run").onclick = async () => {
      try {
        const r = await apiPost("/rpc/api/compliance/crawler/run", {});
        setRichPanel(
          "compliance-crawler-log",
          `<p class="panel-headline">Crawler run</p>${renderDl({
            Indexed: r.indexed,
            Queued: r.queued,
          })}${renderDetailJson(r)}`,
        );
      } catch (err) {
        setRichPanel(
          "compliance-crawler-log",
          `<p class="panel-error">${escapeHtml(err.error || "crawler failed")}</p>`,
        );
      }
    };
    byId("compliance-crawler-status").onclick = async () => {
      try {
        const r = await apiGet("/rpc/api/compliance/crawler/status");
        setRichPanel(
          "compliance-crawler-log",
          `<p class="panel-headline">Crawler status</p>${renderDl(r)}${renderDetailJson(r)}`,
        );
      } catch (err) {
        setRichPanel(
          "compliance-crawler-log",
          `<p class="panel-error">${escapeHtml(err.error || "status failed")}</p>`,
        );
      }
    };
    byId("compliance-retention-status").onclick = async () => {
      try {
        const r = await apiGet("/rpc/api/compliance/retention/jobs");
        const summary = { ...r };
        if (summary.checked_at) summary.checked_at = fmtTime(summary.checked_at);
        setRichPanel(
          "compliance-retention-log",
          `<p class="panel-headline">Retention backlog snapshot</p>${renderDl(summary)}${renderDetailJson(r)}`,
        );
      } catch (err) {
        setRichPanel(
          "compliance-retention-log",
          `<p class="panel-error">${escapeHtml(err.error || "retention failed")}</p>`,
        );
      }
    };

    byId("compliance-delete-create").onclick = async () => {
      const ref = byId("compliance-subject-ref").value.trim();
      try {
        const r = await apiPost("/rpc/api/compliance/deletion-requests", {
          subject_ref: ref,
        });
        byId("compliance-process-id").value = r.id;
        setRichPanel(
          "compliance-delete-log",
          `<p class="panel-headline">Request created — paste this ID into Process Request if needed</p>${renderDl({
            "Request id": r.id,
          })}${renderDetailJson(r)}`,
        );
      } catch (err) {
        setRichPanel(
          "compliance-delete-log",
          `<p class="panel-error">${escapeHtml(err.error || "create failed")}</p>`,
        );
      }
    };
    byId("compliance-list-delete").onclick = async () => {
      try {
        const r = await apiGet("/rpc/api/compliance/deletion-requests");
        const rows = (r.requests || []).map((x) => ({
          id: x.id || "—",
          subject: x.subject_ref,
          status: x.status,
          due: fmtTime(x.due_at),
        }));
        const table = renderDataTable(
          [
            { key: "id", label: "Request" },
            { key: "subject", label: "Subject" },
            { key: "status", label: "Status" },
            { key: "due", label: "Due" },
          ],
          rows,
        );
        setRichPanel(
          "compliance-delete-log",
          `<p class="panel-headline">Deletion requests (${(r.requests || []).length})</p>${table}${renderDetailJson(r)}`,
        );
      } catch (err) {
        setRichPanel(
          "compliance-delete-log",
          `<p class="panel-error">${escapeHtml(err.error || "list failed")}</p>`,
        );
      }
    };
    byId("compliance-delete-process").onclick = async () => {
      const id = byId("compliance-process-id").value.trim();
      try {
        await stepUp("delete_or_reversal");
        const r = await apiPost(
          `/rpc/api/compliance/deletion-requests/${id}/process`,
          {},
          { stepUp: true },
        );
        setRichPanel(
          "compliance-delete-log",
          `<p class="panel-headline">Processed ${escapeHtml(id)}</p>${renderDl({
            Policy: r.policy,
          })}${renderDetailJson(r)}`,
        );
      } catch (err) {
        setRichPanel(
          "compliance-delete-log",
          `<p class="panel-error">${escapeHtml(err.error || "process failed")}</p>`,
        );
      }
    };

    byId("compliance-audit-load").onclick = async () => {
      const action = encodeURIComponent(
        byId("compliance-audit-filter").value.trim(),
      );
      const page = encodeURIComponent(
        byId("compliance-audit-page").value.trim() || "1",
      );
      const limit = encodeURIComponent(
        byId("compliance-audit-limit").value.trim() || "50",
      );
      try {
        const r = await apiGet(
          `/rpc/api/compliance/audit-logs?page=${page}&limit=${limit}&action=${action}`,
        );
        const logs = r.logs || [];
        const rows = logs.map((l) => ({
          when: fmtTime(l.at),
          actor: l.actor,
          action: l.action,
          entity: `${l.entity_type || "—"} ${l.entity_id || ""}`.trim(),
        }));
        const table = renderDataTable(
          [
            { key: "when", label: "When" },
            { key: "actor", label: "Actor" },
            { key: "action", label: "Action" },
            { key: "entity", label: "Entity" },
          ],
          rows,
        );
        setRichPanel(
          "compliance-audit-log",
          `<p class="panel-headline">Audit log entries (${logs.length})</p>${table}${renderDetailJson(r)}`,
        );
      } catch (err) {
        setRichPanel(
          "compliance-audit-log",
          `<p class="panel-error">${escapeHtml(err.error || "audit failed")}</p>`,
        );
      }
    };

    byId("compliance-audit-export").onclick = async () => {
      try {
        await stepUp("export");
        const text = await apiFetch(
          "/rpc/api/compliance/audit-logs/export?format=csv&limit=500",
          { method: "GET", auth: true, stepUp: true },
        );
        byId("compliance-audit-log").textContent = String(text).slice(0, 3000);
      } catch (err) {
        toast(err.error || "Export failed", "error");
      }
    };
  }

  function wireQuickButtons() {
    const j = byId("create-job-btn");
    if (j)
      j.onclick = async () => {
        try {
          const r = await apiPost("/rpc/api/hiring/jobs", {
            code: `JOB-${Date.now()}`,
            title: "Warehouse Associate",
            description: "Inbound ops",
            site_code: "SITE-A",
          });
          logTo("activity-log", `Created job ${r.id}`);
        } catch (err) {
          logTo("activity-log", err.error || "job failed");
        }
      };

    const t = byId("create-ticket-btn");
    if (t)
      t.onclick = async () => {
        try {
          const r = await apiPost("/rpc/api/support/tickets", {
            order_id: "ORD-1001",
            ticket_type: "return_and_refund",
            priority: "HIGH",
            description: "Quick ticket",
            business_site: "SITE-A",
          });
          localStorage.setItem(scopedKey(LAST_TICKET_KEY_BASE), r.id);
          logTo("activity-log", `Created ticket ${r.id}`);
        } catch (err) {
          logTo("activity-log", err.error || "ticket failed");
        }
      };

    const rb = byId("create-reservation-btn");
    if (rb)
      rb.onclick = async () => {
        try {
          const r = await apiPost(
            "/rpc/api/inventory/reservations/order-create",
            {
              order_id: `ORD-${Date.now()}`,
              sku: "SKU-100",
              quantity: 2,
              site_code: "SITE-A",
            },
            { idempotency: true },
          );
          logTo("activity-log", `Reservation ${r.id}`);
        } catch (err) {
          logTo("activity-log", err.error || "reservation failed");
        }
      };
  }

  async function initRoute() {
    const publicPaths = new Set(["/", "/hiring/kiosk"]);
    if (routePath === "/") {
      session.clearAuth();
      wireGlobal();
      const form = byId("login-form");
      if (form) form.addEventListener("submit", loginFlow);
      return;
    }

    if (publicPaths.has(routePath)) {
      wireGlobal();
      if (routePath === "/hiring/kiosk") return initKiosk();
      return;
    }

    wireGlobal();

    const ctx = await fetchUserContext();
    if (!ctx) {
      clearUserScopedCache(session.userID());
      session.clearAuth();
      window.location.replace("/");
      return;
    }

    if (routePath === "/dashboard") {
      await initDashboard(ctx);
      wireQuickButtons();
      return;
    }
    if (routePath === "/hiring") return initHiring(ctx);
    if (routePath === "/support") return initSupport(ctx);
    if (routePath === "/inventory") return initInventory(ctx);
    if (routePath === "/compliance") return initCompliance(ctx);
  }

  initRoute();
})();
