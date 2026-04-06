#!/usr/bin/env python3
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

        status, body = self._request("GET", "/rpc/api/hiring/jobs", token=token)
        ok = status == 200 and isinstance(body, dict) and any(j.get("id") == self.ctx.get("job_id") for j in body.get("jobs", []))
        self._record("hiring.list_jobs_post_create_state", ok, "created job not found in jobs list" if not ok else "", self._snippet(body) if not ok else "")

        status, body = self._request("POST", "/rpc/api/hiring/jobs", {"code": 1234}, token=token)
        self._expect_status("hiring.create_job_invalid_payload_type", status, 400, body)

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

    def test_kiosk(self):
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
