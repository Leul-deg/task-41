#!/usr/bin/env python3
import base64
import hashlib
import json
import os
import sys
import time
import uuid
import urllib.error
import urllib.request


BASE_URL = os.environ.get("TEST_BASE_URL", "http://localhost:8081")


class APITestRunner:
    def __init__(self):
        self.total = 0
        self.passed = 0
        self.failed = 0
        self.failures = []
        self.tokens = {}
        self.ctx = {}
        self.gaps = []

    def _request(self, method, path, body=None, token=None, step_up=None, idem_key=None):
        data = None
        if body is not None:
            data = json.dumps(body).encode("utf-8")

        req = urllib.request.Request(BASE_URL + path, data=data, method=method)
        req.add_header("Content-Type", "application/json")
        if token:
            req.add_header("Authorization", f"Bearer {token}")
        if step_up:
            req.add_header("X-Step-Up-Token", step_up)
        if idem_key:
            req.add_header("Idempotency-Key", idem_key)

        try:
            with urllib.request.urlopen(req, timeout=25) as resp:
                raw = resp.read().decode("utf-8", errors="ignore")
                try:
                    parsed = json.loads(raw) if raw else {}
                except Exception:
                    parsed = raw
                return resp.status, parsed
        except urllib.error.HTTPError as e:
            raw = e.read().decode("utf-8", errors="ignore")
            try:
                parsed = json.loads(raw) if raw else {}
            except Exception:
                parsed = raw
            return e.code, parsed

    @staticmethod
    def _snippet(value):
        text = value if isinstance(value, str) else json.dumps(value, default=str)
        text = " ".join(text.split())
        return text[:500]

    @staticmethod
    def _dict_list(payload, key):
        if not isinstance(payload, dict):
            return []
        val = payload.get(key, [])
        if not isinstance(val, list):
            return []
        return [x for x in val if isinstance(x, dict)]

    def _record(self, name, ok, reason="", snippet=""):
        self.total += 1
        status = "PASS" if ok else "FAIL"
        if ok:
            self.passed += 1
        else:
            self.failed += 1
            self.failures.append((name, reason, snippet))

        print(f"TEST={name}")
        print(f"STATUS={status}")
        if reason:
            print(f"REASON={reason}")
        if snippet:
            print(f"LOG_SNIPPET={snippet}")
        print("---")

    def _expect_status(self, name, got, expected, body):
        ok = got == expected
        self._record(name, ok, f"expected HTTP {expected}, got {got}" if not ok else "", self._snippet(body) if not ok else "")
        return ok

    def login(self, username, password):
        status, body = self._request("POST", "/rpc/login", {"username": username, "password": password})
        if status != 200 or not isinstance(body, dict) or "access_token" not in body:
            raise RuntimeError(f"login failed for {username}: status={status}, body={body}")
        self.tokens[username] = body["access_token"]
        return body["access_token"]

    def test_preflight(self):
        status, body = self._request("GET", "/")
        self._expect_status("preflight.frontend_root", status, 200, body)

    def test_auth(self):
        admin = self.login("admin", "LocalAdminPass123!")

        status, body = self._request("POST", "/rpc/login", {"username": "admin"})
        self._expect_status("auth.login_missing_password", status, 400, body)

        status, body = self._request("GET", "/rpc/api/auth/me")
        self._expect_status("auth.me_unauthorized", status, 401, body)

        status, body = self._request("GET", "/rpc/api/auth/me", token=admin)
        if self._expect_status("auth.me_success", status, 200, body) and isinstance(body, dict):
            self.ctx["admin_user_id"] = body.get("id", "")

        recruiter = self.login("recruiter1", "LocalAdminPass123!")
        status, body = self._request("POST", "/rpc/api/compliance/crawler/run", {}, token=recruiter)
        self._expect_status("auth.permission_denied_recruiter_compliance", status, 403, body)

        status, body = self._request("POST", "/rpc/logout", {})
        self._expect_status("bff.rpc_logout", status, 200, body)

        status, body = self._request("POST", "/rpc/refresh", {})
        ok = status in (400, 401)
        self._record("bff.rpc_refresh_no_cookie", ok, f"expected 400 or 401 for refresh without cookie, got {status}" if not ok else "")

    def test_hiring(self):
        token = self.tokens["admin"]
        suffix = str(uuid.uuid4())[:8]

        status, body = self._request(
            "POST",
            "/rpc/api/hiring/jobs",
            {"code": f"API-H-{suffix}", "title": "API Hiring Job", "description": "created by api tests", "site_code": "SITE-A"},
            token=token,
        )
        if self._expect_status("hiring.create_job", status, 201, body) and isinstance(body, dict):
            self.ctx["job_id"] = body.get("id")

        status, body = self._request(
            "POST",
            "/rpc/api/hiring/jobs",
            {"code": f"API-H-FOREIGN-{suffix}", "title": "Foreign Site Job", "description": "scope isolation check", "site_code": "SITE-B"},
            token=token,
        )
        if self._expect_status("hiring.create_foreign_site_job", status, 201, body) and isinstance(body, dict):
            self.ctx["foreign_job_id"] = body.get("id")

        status, body = self._request("GET", "/rpc/api/hiring/jobs", token=token)
        ok = status == 200 and isinstance(body, dict) and any(j.get("id") == self.ctx.get("job_id") for j in body.get("jobs", []))
        self._record("hiring.list_jobs_post_create_state", ok, "created job not found in jobs list" if not ok else "", self._snippet(body) if not ok else "")

        status, body = self._request("POST", "/rpc/api/hiring/jobs", {"code": 1234}, token=token)
        self._expect_status("hiring.create_job_invalid_payload_type", status, 400, body)

        recruiter = self.tokens.get("recruiter1")
        if recruiter and self.ctx.get("foreign_job_id"):
            status, body = self._request(
                "POST",
                "/rpc/api/hiring/applications/import-csv",
                {
                    "job_id": self.ctx.get("foreign_job_id"),
                    "csv": "full_name,email,phone\\nScope Test,scope@example.com,5551112222\\n",
                },
                token=recruiter,
            )
            self._expect_status("hiring.csv_import_out_of_scope_forbidden", status, 403, body)

        status, body = self._request(
            "POST",
            "/rpc/api/hiring/applications/manual",
            {
                "job_id": self.ctx.get("job_id"),
                "full_name": f"API Candidate {suffix}",
                "email": f"api.candidate.{suffix}@example.com",
                "phone": "5557771122",
                "ssn": "123-45-6789",
            },
            token=token,
        )
        if self._expect_status("hiring.create_application_manual", status, 201, body) and isinstance(body, dict):
            self.ctx["application_id"] = body.get("application_id")
            self.ctx["candidate_id"] = body.get("candidate_id")

        status, body = self._request("GET", "/rpc/api/hiring/applications", token=token)
        ok = status == 200 and isinstance(body, dict) and any(a.get("application_id") == self.ctx.get("application_id") for a in body.get("applications", []))
        self._record("hiring.list_applications_post_create_state", ok, "created application not found in application list" if not ok else "", self._snippet(body) if not ok else "")

        status, body = self._request("GET", "/rpc/api/hiring/pipelines/templates", token=token)
        self._expect_status("hiring.list_pipeline_templates", status, 200, body)
        if status == 200 and isinstance(body, dict):
            templates = self._dict_list(body, "templates")
            if templates:
                tpl_id = templates[0].get("id")
                if tpl_id:
                    s2, b2 = self._request("GET", f"/rpc/api/hiring/pipelines/templates/{tpl_id}", token=token)
                    self._expect_status("hiring.get_pipeline_template_by_id", s2, 200, b2)

        tpl_suffix = str(uuid.uuid4())[:8]
        tpl_stages = [
            {"code": f"SCREEN-{tpl_suffix}", "name": "Screening", "order_index": 1, "terminal": False},
            {"code": f"OFFER-{tpl_suffix}", "name": "Offer", "order_index": 2, "terminal": False},
            {"code": f"HIRED-{tpl_suffix}", "name": "Hired", "order_index": 3, "terminal": True, "outcome": "success"},
            {"code": f"REJECT-{tpl_suffix}", "name": "Rejected", "order_index": 4, "terminal": True, "outcome": "failure"},
        ]
        tpl_transitions = [
            {"from_stage_code": f"SCREEN-{tpl_suffix}", "to_stage_code": f"OFFER-{tpl_suffix}"},
            {"from_stage_code": f"OFFER-{tpl_suffix}", "to_stage_code": f"HIRED-{tpl_suffix}"},
            {"from_stage_code": f"OFFER-{tpl_suffix}", "to_stage_code": f"REJECT-{tpl_suffix}"},
        ]
        s3, b3 = self._request(
            "POST", "/rpc/api/hiring/pipelines/templates",
            {"code": f"API-TPL-{tpl_suffix}", "name": f"API Test Template {tpl_suffix}", "stages": tpl_stages, "transitions": tpl_transitions},
            token=token,
        )
        if self._expect_status("hiring.create_pipeline_template", s3, 201, b3) and isinstance(b3, dict):
            new_tpl_id = b3.get("id")
            if new_tpl_id:
                self.ctx["new_tpl_id"] = new_tpl_id
                s4, b4 = self._request(
                    "PUT", f"/rpc/api/hiring/pipelines/templates/{new_tpl_id}",
                    {"code": f"API-TPL-{tpl_suffix}-UPD", "name": f"Updated Template {tpl_suffix}", "stages": tpl_stages, "transitions": tpl_transitions},
                    token=token,
                )
                self._expect_status("hiring.update_pipeline_template", s4, 200, b4)

        s5, b5 = self._request("GET", "/rpc/api/hiring/jobs/for-intake", token=token)
        self._expect_status("hiring.list_jobs_for_intake", s5, 200, b5)

        if self.ctx.get("candidate_id"):
            status, body = self._request("GET", f"/rpc/api/hiring/candidates/{self.ctx.get('candidate_id')}", token=token)
            self._expect_status("hiring.get_candidate", status, 200, body)

        if self.ctx.get("application_id"):
            status, body = self._request("GET", f"/rpc/api/hiring/applications/{self.ctx.get('application_id')}/allowed-transitions", token=token)
            self._expect_status("hiring.get_allowed_transitions", status, 200, body)

            if status == 200 and isinstance(body, dict) and body.get("allowed_transitions"):
                first_trans = body["allowed_transitions"][0]
                cur_stage = body.get("current_stage", "SCREENING")
                status2, body2 = self._request(
                    "POST",
                    f"/rpc/api/hiring/applications/{self.ctx.get('application_id')}/transition",
                    {"from_stage": cur_stage, "to_stage": first_trans.get("to_stage"), "fields": {"notes": "api test transition"}},
                    token=token,
                )
                self._expect_status("hiring.transition_application", status2, 200, body2)

            status, body = self._request("GET", f"/rpc/api/hiring/applications/{self.ctx.get('application_id')}/events", token=token)
            self._expect_status("hiring.get_pipeline_events", status, 200, body)

    def test_admin(self):
        token = self.tokens["admin"]

        status, body = self._request("GET", "/rpc/api/admin/roles", token=token)
        roles_ok = status == 200 and isinstance(body, dict) and len(self._dict_list(body, "roles")) > 0
        self._record("admin.list_roles", roles_ok, "roles list unavailable" if not roles_ok else "", self._snippet(body) if not roles_ok else "")
        if not roles_ok:
            return

        recruiter_role_id = ""
        for role in self._dict_list(body, "roles"):
            if role.get("code") == "HR_RECRUITER":
                recruiter_role_id = role.get("id", "")
                break
        if not recruiter_role_id:
            self._record("admin.resolve_recruiter_role", False, "HR_RECRUITER role missing", self._snippet(body))
            return

        status, body = self._request("POST", "/rpc/api/auth/step-up", {"password": "LocalAdminPass123!", "action_class": "role_permission_change"}, token=token)
        step_ok = self._expect_status("admin.obtain_stepup", status, 200, body)
        step_token = body.get("step_up_token") if step_ok and isinstance(body, dict) else ""
        if not step_token:
            return

        status, body = self._request(
            "PUT",
            f"/rpc/api/admin/roles/{recruiter_role_id}/permissions",
            {"permissions": [{"module": "hiring", "action": "view"}, {"module": "hiring", "action": "create"}]},
            token=token,
            step_up=step_token,
        )
        self._expect_status("admin.update_role_permissions", status, 200, body)

        status, body = self._request("POST", "/rpc/api/auth/step-up", {"password": "LocalAdminPass123!", "action_class": "role_permission_change"}, token=token)
        step_ok = self._expect_status("admin.obtain_stepup_for_scopes", status, 200, body)
        step_token = body.get("step_up_token") if step_ok and isinstance(body, dict) else ""
        if not step_token:
            return

        status, body = self._request(
            "PUT",
            f"/rpc/api/admin/roles/{recruiter_role_id}/scopes",
            {"scopes": [{"module": "hiring", "scope": "site", "value": "SITE-A"}]},
            token=token,
            step_up=step_token,
        )
        self._expect_status("admin.update_role_scopes", status, 200, body)

        status, body = self._request("POST", "/rpc/api/auth/step-up", {"password": "LocalAdminPass123!", "action_class": "role_permission_change"}, token=token)
        step_ok = self._expect_status("admin.obtain_stepup_rotate", status, 200, body)
        rotate_step = body.get("step_up_token") if step_ok and isinstance(body, dict) else ""

        if rotate_step:
            key_suffix = str(uuid.uuid4())[:8]
            status, body = self._request(
                "POST", "/rpc/api/admin/client-keys/rotate",
                {"key_name": f"api-test-key-{key_suffix}", "secret": "api-test-secret"},
                token=token, step_up=rotate_step,
            )
            if self._expect_status("admin.rotate_client_key", status, 201, body) and isinstance(body, dict):
                new_key_id = body.get("key_id", "")
                status, body = self._request("POST", "/rpc/api/auth/step-up", {"password": "LocalAdminPass123!", "action_class": "delete_or_reversal"}, token=token)
                step_ok2 = self._expect_status("admin.obtain_stepup_revoke", status, 200, body)
                revoke_step = body.get("step_up_token") if step_ok2 and isinstance(body, dict) else ""
                if revoke_step and new_key_id:
                    status, body = self._request(
                        "POST", f"/rpc/api/admin/client-keys/{new_key_id}/revoke", {},
                        token=token, step_up=revoke_step,
                    )
                    self._expect_status("admin.revoke_client_key", status, 200, body)

    def test_kiosk(self):
        status, body = self._request("GET", "/rpc/kiosk/jobs")
        self._expect_status("kiosk.list_public_jobs", status, 200, body)

        status, body = self._request("POST", "/rpc/api/hiring/applications/kiosk", {"job_id": self.ctx.get("job_id")})
        self._expect_status("kiosk.authenticated_endpoint_requires_auth", status, 401, body)

        suffix = str(uuid.uuid4())[:8]
        status, body = self._request(
            "POST",
            "/rpc/kiosk/applications",
            {
                "job_id": self.ctx.get("job_id"),
                "full_name": f"Kiosk Candidate {suffix}",
                "email": f"kiosk.{suffix}@example.com",
                "phone": "5552223344",
                "ssn": "987-65-4321",
            },
        )
        self._expect_status("kiosk.public_submit_success", status, 201, body)

        status, body = self._request("POST", "/rpc/kiosk/applications", {"full_name": "No Job"})
        self._expect_status("kiosk.public_submit_missing_required", status, 400, body)

    def test_support(self):
        token = self.tokens["admin"]

        status, body = self._request(
            "POST",
            "/rpc/api/support/tickets",
            {
                "order_id": "ORD-1001",
                "ticket_type": "return_and_refund",
                "priority": "HIGH",
                "description": "API support ticket",
                "business_site": "SITE-A",
            },
            token=token,
        )
        if self._expect_status("support.create_ticket", status, 201, body) and isinstance(body, dict):
            self.ctx["ticket_id"] = body.get("id")

        status, body = self._request("GET", f"/rpc/api/support/tickets/{self.ctx.get('ticket_id')}", token=token)
        ok = status == 200 and isinstance(body, dict) and body.get("id") == self.ctx.get("ticket_id")
        self._record("support.get_ticket_post_create_state", ok, "created ticket not retrievable by id" if not ok else "", self._snippet(body) if not ok else "")

        status, body = self._request(
            "PUT",
            f"/rpc/api/support/tickets/{self.ctx.get('ticket_id')}",
            {"description": "conflict update", "record_version": 999},
            token=token,
        )
        self._expect_status("support.update_conflict_version", status, 409, body)

        status, body = self._request("GET", "/rpc/api/support/tickets/00000000-0000-0000-0000-000000000000", token=token)
        self._expect_status("support.get_ticket_not_found", status, 404, body)

        status, body = self._request("GET", "/rpc/api/support/tickets", token=token)
        self._expect_status("support.list_tickets", status, 200, body)

        status, body = self._request("GET", "/rpc/api/support/orders", token=token)
        self._expect_status("support.list_orders", status, 200, body)

        status, body = self._request("GET", "/rpc/api/support/orders/for-intake", token=token)
        self._expect_status("support.list_orders_for_intake", status, 200, body)

        if self.ctx.get("ticket_id"):
            status, body = self._request(
                "POST",
                f"/rpc/api/support/tickets/{self.ctx.get('ticket_id')}/conflict-resolve",
                {"current_version": 1, "expected_version": 999, "mode": "discard", "description": "api test discard"},
                token=token,
            )
            self._expect_status("support.conflict_resolve_discard", status, 200, body)

            _att_raw = b"api-test-attachment"
            _att_b64 = base64.b64encode(_att_raw).decode()
            _att_cksum = hashlib.sha256(_att_raw).hexdigest()
            status, body = self._request(
                "POST",
                f"/rpc/api/support/tickets/{self.ctx.get('ticket_id')}/attachments",
                {
                    "file_name": "api_test.pdf",
                    "mime_type": "application/pdf",
                    "size_mb": 1,
                    "size_bytes": len(_att_raw),
                    "checksum": _att_cksum,
                    "content_base64": _att_b64,
                },
                token=token,
            )
            self._expect_status("support.add_attachment_success", status, 201, body)

        status, body = self._request(
            "POST", "/rpc/api/support/tickets/refund-approve",
            {"ticket_id": self.ctx.get("ticket_id"), "note": "test refund"},
            token=token,
            idem_key=f"idem-refund-no-step-{self.ctx.get('ticket_id', 'missing')}",
        )
        self._expect_status("support.refund_approve_no_stepup", status, 403, body)

        status, body = self._request("POST", "/rpc/api/auth/step-up", {"password": "LocalAdminPass123!", "action_class": "refund_approval"}, token=token)
        step_ok = self._expect_status("support.obtain_stepup_refund", status, 200, body)
        refund_step = body.get("step_up_token") if step_ok and isinstance(body, dict) else ""

        if refund_step and self.ctx.get("ticket_id"):
            status, body = self._request(
                "POST", "/rpc/api/support/tickets/refund-approve",
                {"ticket_id": self.ctx.get("ticket_id"), "note": "approved via api test"},
                token=token,
                step_up=refund_step,
                idem_key=f"idem-refund-step-{self.ctx.get('ticket_id', 'missing')}",
            )
            self._expect_status("support.refund_approve_with_stepup", status, 200, body)

    def test_inventory(self):
        token = self.tokens["admin"]
        order_id = f"ORD-API-{str(uuid.uuid4())[:8]}"

        status, body = self._request("GET", "/rpc/api/inventory/reservations", token=token)
        pre_ok = status == 200 and isinstance(body, dict)
        pre_has = any(r.get("order_id") == order_id for r in self._dict_list(body, "reservations")) if pre_ok else False
        self._record("inventory.pre_state_no_order_collision", pre_ok and not pre_has, "pre-state unexpected existing reservation with test order id" if (pre_ok and pre_has) else ("unable to load pre-state reservations" if not pre_ok else ""), self._snippet(body) if not pre_ok or pre_has else "")

        status, body = self._request(
            "POST",
            "/rpc/api/inventory/reservations/order-create",
            {"order_id": order_id, "sku": "SKU-100", "quantity": 2, "site_code": "SITE-A"},
            token=token,
            idem_key=f"idem-{order_id}",
        )
        self._expect_status("inventory.create_reservation", status, 201, body)

        status, body = self._request("GET", "/rpc/api/inventory/reservations", token=token)
        post_create_ok = status == 200 and isinstance(body, dict)
        post_create_match = any(r.get("order_id") == order_id and r.get("status") in ("HELD", "PARTIAL_CONFIRMED", "RELEASED") for r in self._dict_list(body, "reservations")) if post_create_ok else False
        self._record("inventory.post_create_state", post_create_ok and post_create_match, "created reservation not found in post-create list" if not (post_create_ok and post_create_match) else "", self._snippet(body) if not (post_create_ok and post_create_match) else "")

        status, body = self._request("POST", "/rpc/api/inventory/reservations/order-create", {"order_id": order_id, "sku": "SKU-100", "quantity": "bad-type", "site_code": "SITE-A"}, token=token, idem_key=f"idem-invalid-{order_id}")
        self._expect_status("inventory.create_reservation_invalid_quantity_type", status, 400, body)

        confirm_order = f"ORD-CF-{str(uuid.uuid4())[:8]}"
        status, body = self._request(
            "POST", "/rpc/api/inventory/reservations/order-create",
            {"order_id": confirm_order, "sku": "SKU-100", "quantity": 2, "site_code": "SITE-A"},
            token=token, idem_key=f"idem-cf-{confirm_order}",
        )
        if status == 201:
            _, list_body = self._request("GET", "/rpc/api/inventory/reservations", token=token)
            confirm_res_id = next(
                (r.get("id") for r in self._dict_list(list_body, "reservations") if r.get("order_id") == confirm_order),
                None,
            )
            if confirm_res_id:
                status, body = self._request(
                    "POST", f"/rpc/api/inventory/reservations/{confirm_res_id}/confirm",
                    {"confirmed_qty": 2, "reason_code": "API_CF"},
                    token=token,
                )
                self._expect_status("inventory.confirm_reservation", status, 200, body)

        release_order = f"ORD-RL-{str(uuid.uuid4())[:8]}"
        status, body = self._request(
            "POST", "/rpc/api/inventory/reservations/order-create",
            {"order_id": release_order, "sku": "SKU-100", "quantity": 1, "site_code": "SITE-A"},
            token=token, idem_key=f"idem-rl-{release_order}",
        )
        if status == 201:
            _, list_body = self._request("GET", "/rpc/api/inventory/reservations", token=token)
            release_res_id = next(
                (r.get("id") for r in self._dict_list(list_body, "reservations") if r.get("order_id") == release_order),
                None,
            )
            if release_res_id:
                status, body = self._request(
                    "POST", f"/rpc/api/inventory/reservations/{release_res_id}/release",
                    {"reason_code": "API_RL"},
                    token=token,
                )
                self._expect_status("inventory.release_reservation", status, 200, body)

        status, body = self._request("POST", "/rpc/api/inventory/reservations/order-cancel", {"order_id": order_id}, token=token)
        self._expect_status("inventory.cancel_order_reservations", status, 200, body)

        status, body = self._request("GET", "/rpc/api/inventory/reservations", token=token)
        post_cancel_ok = status == 200 and isinstance(body, dict)
        released = any(r.get("order_id") == order_id and r.get("status") == "RELEASED" for r in self._dict_list(body, "reservations")) if post_cancel_ok else False
        self._record("inventory.post_cancel_state_released", post_cancel_ok and released, "reservation not in RELEASED state after cancellation" if not (post_cancel_ok and released) else "", self._snippet(body) if not (post_cancel_ok and released) else "")

        status, body = self._request(
            "POST",
            "/rpc/api/inventory/transfers",
            {"sku": "SKU-100", "quantity": 1, "from_warehouse": "WH-1", "to_warehouse": "WH-2", "reason_code": "API_TRANSFER"},
            token=token,
        )
        self._expect_status("inventory.transfer_between_warehouses", status, 201, body)

        status, body = self._request(
            "POST",
            "/rpc/api/inventory/inbound",
            {"sku": "SKU-100", "quantity": 5, "to_warehouse": "WH-1", "reason_code": "API_INBOUND_TEST"},
            token=token,
        )
        self._expect_status("inventory.inbound_move", status, 201, body)

        status, body = self._request("GET", "/rpc/api/inventory/orders/for-intake", token=token)
        self._expect_status("inventory.list_orders_for_intake", status, 200, body)

        status, body = self._request(
            "POST", "/rpc/api/inventory/ledger/00000000-0000-0000-0000-000000000000/reverse",
            {"reason_code": "DATA_ERROR"},
            token=token,
        )
        self._expect_status("inventory.ledger_reverse_no_stepup", status, 403, body)

        status, body = self._request("POST", "/rpc/api/auth/step-up", {"password": "LocalAdminPass123!", "action_class": "delete_or_reversal"}, token=token)
        step_ok = self._expect_status("inventory.obtain_stepup_reversal", status, 200, body)
        reversal_step = body.get("step_up_token") if step_ok and isinstance(body, dict) else ""

        if reversal_step:
            status, body = self._request(
                "POST", "/rpc/api/inventory/ledger/00000000-0000-0000-0000-000000000000/reverse",
                {"reason_code": "DATA_ERROR"},
                token=token, step_up=reversal_step,
            )
            self._expect_status("inventory.ledger_reverse_notfound", status, 404, body)

        status, body = self._request("GET", "/rpc/api/inventory/balances?site=SITE-A", token=token)
        balances_ok = status == 200 and isinstance(body, dict) and len(self._dict_list(body, "balances")) > 0
        self._record("inventory.load_balances_for_cycle_count", balances_ok, "balances unavailable for cycle-count coverage" if not balances_ok else "", self._snippet(body) if not balances_ok else "")
        if balances_ok:
            first = self._dict_list(body, "balances")[0]
            counted_qty = int(first.get("on_hand", 0))
            status, body = self._request(
                "POST",
                "/rpc/api/inventory/cycle-counts",
                {"warehouse_code": first.get("warehouse"), "sku": first.get("sku"), "counted_qty": counted_qty, "reason_code": "API_CYCLE"},
                token=token,
            )
            self._expect_status("inventory.cycle_count_happy_path", status, 201, body)

    def test_compliance(self):
        token = self.tokens["admin"]

        status, body = self._request("POST", "/rpc/api/compliance/deletion-requests", {}, token=token)
        self._expect_status("compliance.create_deletion_missing_subject", status, 400, body)

        status, body = self._request(
            "POST",
            "/rpc/api/compliance/deletion-requests",
            {"subject_ref": self.ctx.get("candidate_id") or "11111111-1111-1111-1111-111111111111"},
            token=token,
        )
        if self._expect_status("compliance.create_deletion_request", status, 201, body) and isinstance(body, dict):
            self.ctx["deletion_id"] = body.get("id")

        status, body = self._request("GET", "/rpc/api/compliance/deletion-requests", token=token)
        exists_pending = False
        if status == 200 and isinstance(body, dict):
            for req in self._dict_list(body, "requests"):
                if req.get("id") == self.ctx.get("deletion_id"):
                    exists_pending = True
                    break
        self._record("compliance.post_create_list_state", status == 200 and exists_pending, "created deletion request not found in list" if not (status == 200 and exists_pending) else "", self._snippet(body) if not (status == 200 and exists_pending) else "")

        status, body = self._request("POST", f"/rpc/api/compliance/deletion-requests/{self.ctx.get('deletion_id')}/process", {}, token=token)
        self._expect_status("compliance.process_requires_stepup", status, 403, body)

        status, body = self._request("POST", "/rpc/api/auth/step-up", {"password": "LocalAdminPass123!", "action_class": "delete_or_reversal"}, token=token)
        step_ok = self._expect_status("compliance.obtain_stepup", status, 200, body)
        step_token = body.get("step_up_token") if step_ok and isinstance(body, dict) else ""

        status, body = self._request("POST", f"/rpc/api/compliance/deletion-requests/{self.ctx.get('deletion_id')}/process", {}, token=token, step_up=step_token)
        self._expect_status("compliance.process_with_stepup", status, 200, body)

        status, body = self._request("GET", "/rpc/api/compliance/deletion-requests", token=token)
        completed = False
        if status == 200 and isinstance(body, dict):
            for req in self._dict_list(body, "requests"):
                if req.get("id") == self.ctx.get("deletion_id") and req.get("status") == "COMPLETED":
                    completed = True
                    break
        self._record("compliance.post_process_state_completed", status == 200 and completed, "deletion request not completed after processing" if not (status == 200 and completed) else "", self._snippet(body) if not (status == 200 and completed) else "")

        status, body = self._request("GET", "/rpc/api/compliance/audit-logs", token=token)
        self._expect_status("compliance.audit_logs", status, 200, body)

        status, body = self._request("GET", "/rpc/api/compliance/retention/jobs", token=token)
        self._expect_status("compliance.retention_jobs", status, 200, body)

        status, body = self._request("GET", "/rpc/api/compliance/crawler/status", token=token)
        self._expect_status("compliance.crawler_status", status, 200, body)

        status, body = self._request("POST", "/rpc/api/compliance/crawler/run", {}, token=token)
        self._expect_status("compliance.crawler_run", status, 200, body)

        status, body = self._request("GET", "/rpc/api/compliance/audit-logs/export", token=token)
        self._expect_status("compliance.audit_export_no_stepup", status, 403, body)

        status, body = self._request("POST", "/rpc/api/auth/step-up", {"password": "LocalAdminPass123!", "action_class": "export"}, token=token)
        step_ok = self._expect_status("compliance.obtain_stepup_export", status, 200, body)
        export_step = body.get("step_up_token") if step_ok and isinstance(body, dict) else ""

        if export_step:
            status, body = self._request("GET", "/rpc/api/compliance/audit-logs/export", token=token, step_up=export_step)
            self._expect_status("compliance.audit_export_with_stepup", status, 200, body)

    def run(self):
        print("[api_tests] starting")
        ordered_tests = [
            ("preflight", self.test_preflight),
            ("auth", self.test_auth),
            ("admin", self.test_admin),
            ("hiring", self.test_hiring),
            ("kiosk", self.test_kiosk),
            ("support", self.test_support),
            ("inventory", self.test_inventory),
            ("compliance", self.test_compliance),
        ]
        for name, fn in ordered_tests:
            try:
                fn()
            except Exception as exc:
                self._record(f"{name}.suite_exception", False, "test suite exception", self._snippet(str(exc)))

        print("[api_tests] summary")
        print("SUITE=API_tests")
        print(f"TOTAL={self.total}")
        print(f"PASSED={self.passed}")
        print(f"FAILED={self.failed}")
        print(f"TODO_GAPS={len(self.gaps)}")
        for gap in self.gaps:
            print(gap)

        return 1 if self.failed > 0 else 0


if __name__ == "__main__":
    sys.exit(APITestRunner().run())
