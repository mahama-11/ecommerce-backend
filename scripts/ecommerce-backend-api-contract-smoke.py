#!/usr/bin/env python3
"""Safe Ecommerce backend API-contract smoke scaffold.

Default dry-run validates route contracts and existing quality evidence without network
or mutations. Active HTTP mode is local/dev read-only only. Prod is hard blocked unless
ECOM_PROD_SMOKE_APPROVED=1 is set; even then this scaffold performs no writes.
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[1]
REPORT_DIR = ROOT / "reports" / "quality" / "business-journeys"
LATEST_REPORT = REPORT_DIR / "api-contract-latest.json"

SECRET_RE = re.compile(
    r"(?i)(bearer\s+[a-z0-9._~+/=-]+|authorization\s*[:=]\s*[^\s,}]+|access[_-]?token\s*[:=]\s*[^\s,}]+|refresh[_-]?token\s*[:=]\s*[^\s,}]+|password\s*[:=]\s*[^\s,}]+|secret\s*[:=]\s*[^\s,}]+|service[_-]?secret\s*[:=]\s*[^\s,}]+|postgres://[^\s]+|mysql://[^\s]+|redis://[^\s]+)"
)

CONTRACT_ROUTES: list[dict[str, str]] = [
    {"method": "GET", "path": "/healthz", "auth": "public", "literal": '"/healthz"'},
    {"method": "GET", "path": "/readyz", "auth": "public", "literal": '"/readyz"'},
    {"method": "POST", "path": "/api/v1/ecommerce/auth/register", "auth": "public", "literal": '"/register"'},
    {"method": "POST", "path": "/api/v1/ecommerce/auth/login", "auth": "public", "literal": '"/login"'},
    {"method": "GET", "path": "/api/v1/ecommerce/auth/session", "auth": "platform_jwt", "literal": '"/session"'},
    {"method": "GET", "path": "/api/v1/ecommerce/access/me", "auth": "platform_jwt", "literal": '"/access/me"'},
    {"method": "GET", "path": "/api/v1/ecommerce/wallet/summary", "auth": "platform_jwt", "literal": '"/wallet/summary"'},
    {"method": "GET", "path": "/api/v1/ecommerce/wallet/history", "auth": "platform_jwt", "literal": '"/wallet/history"'},
    {"method": "GET", "path": "/api/v1/ecommerce/commercial/offerings", "auth": "public", "literal": '"/commercial/offerings"'},
    {"method": "POST", "path": "/api/v1/ecommerce/commercial/orders", "auth": "platform_jwt", "literal": '"/commercial/orders"'},
    {"method": "POST", "path": "/api/v1/ecommerce/commercial/orders/:orderID/confirm-payment", "auth": "platform_jwt", "literal": '"/commercial/orders/:orderID/confirm-payment"'},
    {"method": "GET", "path": "/api/v1/ecommerce/billing/summary", "auth": "platform_jwt", "literal": '"/billing/summary"'},
    {"method": "GET", "path": "/api/v1/ecommerce/billing/charges", "auth": "platform_jwt", "literal": '"/billing/charges"'},
    {"method": "GET", "path": "/api/v1/ecommerce/promotions/codes/:code/resolve", "auth": "public", "literal": '"/promotions/codes/:code/resolve"'},
    {"method": "GET", "path": "/api/v1/ecommerce/promotions/me/overview", "auth": "platform_jwt", "literal": '"/promotions/me/overview"'},
    {"method": "GET", "path": "/api/v1/ecommerce/commissions/me/overview", "auth": "platform_jwt", "literal": '"/commissions/me/overview"'},
    {"method": "POST", "path": "/api/v1/ecommerce/assets/source", "auth": "platform_jwt", "literal": '"/assets/source"'},
    {"method": "POST", "path": "/api/v1/ecommerce/prompts/preview", "auth": "platform_jwt", "literal": '"/prompts/preview"'},
    {"method": "POST", "path": "/api/v1/ecommerce/products", "auth": "platform_jwt", "literal": '"/products"'},
    {"method": "GET", "path": "/api/v1/ecommerce/products/:product_id", "auth": "platform_jwt", "literal": '"/products/:product_id"'},
    {"method": "POST", "path": "/api/v1/ecommerce/export-packages", "auth": "platform_jwt", "literal": '"/export-packages"'},
    {"method": "GET", "path": "/api/v1/ecommerce/downloads/:download_id/content", "auth": "platform_jwt", "literal": '"/downloads/:download_id/content"'},
    {"method": "POST", "path": "/api/v1/ecommerce/v2/visual-workflows/sessions", "auth": "platform_jwt", "literal": '"/v2/visual-workflows/sessions"'},
    {"method": "POST", "path": "/api/v1/ecommerce/v2/visual-workflows/:session_id/source-references", "auth": "platform_jwt", "literal": '"/v2/visual-workflows/:session_id/source-references"'},
    {"method": "POST", "path": "/internal/v1/ecommerce/jobs/:jobID/runtime", "auth": "internal_service", "literal": '"/jobs/:jobID/runtime"'},
    {"method": "POST", "path": "/internal/v1/ecommerce/jobs/:jobID/results", "auth": "internal_service", "literal": '"/jobs/:jobID/results"'},
]


def redact(value: Any) -> Any:
    if isinstance(value, str):
        return SECRET_RE.sub("[REDACTED]", value)
    if isinstance(value, list):
        return [redact(v) for v in value]
    if isinstance(value, dict):
        return {k: redact(v) for k, v in value.items() if "token" not in k.lower() and "secret" not in k.lower() and "password" not in k.lower()}
    return value


def write_report(payload: dict[str, Any]) -> None:
    REPORT_DIR.mkdir(parents=True, exist_ok=True)
    sanitized = redact(payload)
    text = json.dumps(sanitized, ensure_ascii=False, indent=2) + "\n"
    LATEST_REPORT.write_text(text, encoding="utf-8")
    print(json.dumps({"status": sanitized["status"], "report": str(LATEST_REPORT), "mode": sanitized["mode"]}, ensure_ascii=False))


def route_contract_check() -> dict[str, Any]:
    router_path = ROOT / "internal" / "router" / "router.go"
    if not router_path.exists():
        return {"status": "FAIL", "missing_file": str(router_path), "missing_routes": CONTRACT_ROUTES}
    text = router_path.read_text(encoding="utf-8", errors="ignore")
    missing = [route for route in CONTRACT_ROUTES if route["literal"] not in text]
    auth_guards = {
        "platform_jwt_middleware": "PlatformJWTAuth" in text,
        "internal_service_middleware": "RequireInternalService" in text,
        "optional_platform_jwt_middleware": "OptionalPlatformJWTAuth" in text,
    }
    return {
        "status": "PASS" if not missing and all(auth_guards.values()) else "FAIL",
        "router_path": str(router_path),
        "routes_checked": len(CONTRACT_ROUTES),
        "missing_routes": missing,
        "auth_guards": auth_guards,
    }


def load_quality_routes_report() -> dict[str, Any]:
    path = ROOT / "reports" / "quality" / "routes" / "latest.json"
    if not path.exists():
        return {"status": "NOT_RUN", "path": str(path), "note": "route inventory report missing; run scripts/route-inventory-gate.sh"}
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except Exception as exc:
        return {"status": "FAIL", "path": str(path), "error_type": type(exc).__name__}
    return {"status": data.get("status", "UNKNOWN"), "path": str(path), "generated_at_unix": data.get("generated_at_unix")}


def swagger_check() -> dict[str, Any]:
    script = ROOT / "scripts" / "gen-swagger.sh"
    candidates = [ROOT / "docs" / "swagger.json", ROOT / "docs" / "swagger.yaml", ROOT / "docs" / "openapi.json", ROOT / "docs" / "openapi.yaml"]
    existing = [str(p) for p in candidates if p.exists()]
    return {
        "status": "PASS_WITH_NOTES" if script.exists() else "NOT_RUN",
        "generator": str(script) if script.exists() else None,
        "generated_specs_found": existing,
        "dry_run_note": "Swagger generation/drift is not run by this smoke scaffold; generator presence is recorded.",
    }


def http_probe(base_url: str, path: str, method: str = "GET") -> dict[str, Any]:
    url = base_url.rstrip("/") + path
    started = time.time()
    try:
        with urllib.request.urlopen(urllib.request.Request(url, method=method), timeout=10) as resp:
            body = resp.read(2048).decode("utf-8", "replace")
            return {"method": method, "path": path, "status_code": resp.status, "elapsed_ms": int((time.time() - started) * 1000), "body_sample": body[:200]}
    except urllib.error.HTTPError as exc:
        body = exc.read(2048).decode("utf-8", "replace")
        return {"method": method, "path": path, "status_code": exc.code, "elapsed_ms": int((time.time() - started) * 1000), "body_sample": body[:200]}
    except Exception as exc:
        return {"method": method, "path": path, "status": "ERROR", "error_type": type(exc).__name__, "elapsed_ms": int((time.time() - started) * 1000)}


def main() -> int:
    parser = argparse.ArgumentParser(description="Ecommerce backend API contract smoke scaffold (safe dry-run default).")
    parser.add_argument("--env", choices=["local", "dev", "prod"], default="local")
    parser.add_argument("--dry-run", action="store_true", default=True, help="Validate static contract only; no network and no writes (default).")
    parser.add_argument("--execute-live", action="store_true", help="Opt into read-only HTTP probes for local/dev. Never enables writes.")
    parser.add_argument("--base-url", default=os.environ.get("ECOM_BACKEND_BASE_URL", "http://127.0.0.1:8080"))
    args = parser.parse_args()

    approved = os.environ.get("ECOM_PROD_SMOKE_APPROVED") == "1"
    if args.env == "prod" and not approved:
        payload = {
            "generated_at_unix": int(time.time()),
            "status": "BLOCKED",
            "mode": "prod-refusal",
            "env": args.env,
            "prod_live_smoke": "NOT_RUN",
            "reason": "prod smoke requires ECOM_PROD_SMOKE_APPROVED=1; no network calls or mutations attempted",
            "report_path": str(LATEST_REPORT),
        }
        write_report(payload)
        return 3

    if args.env == "prod" and args.execute_live:
        payload = {
            "generated_at_unix": int(time.time()),
            "status": "BLOCKED",
            "mode": "prod-live-not-implemented",
            "env": args.env,
            "prod_live_smoke": "NOT_RUN",
            "reason": "prod read-only/live probes are intentionally not implemented in this safe scaffold; do not imply approved prod execution without real probes",
            "report_path": str(LATEST_REPORT),
        }
        write_report(payload)
        return 3

    mode = "read-only-live" if args.execute_live and args.env in {"local", "dev"} else "dry-run"
    route_contract = route_contract_check()
    route_inventory = load_quality_routes_report()
    swagger = swagger_check()
    probes: list[dict[str, Any]] = []
    if mode == "read-only-live":
        probes = [
            http_probe(args.base_url, "/healthz"),
            http_probe(args.base_url, "/api/v1/ecommerce/health"),
            http_probe(args.base_url, "/api/v1/ecommerce/commercial/offerings"),
            http_probe(args.base_url, "/api/v1/ecommerce/access/me"),
            http_probe(args.base_url, "/internal/v1/ecommerce/health"),
        ]

    failures: list[dict[str, Any]] = []
    if route_contract["status"] != "PASS":
        failures.append({"type": "route_contract", "details": route_contract})
    if route_inventory.get("status") not in {"PASS", "NOT_RUN"}:
        failures.append({"type": "route_inventory_report", "details": route_inventory})
    if mode == "read-only-live":
        public_failures = [p for p in probes[:3] if p.get("status_code") not in (200, 204)]
        access_negative = probes[3]
        internal_negative = probes[4]
        if public_failures:
            failures.append({"type": "public_read_only_probe", "probes": public_failures})
        if access_negative.get("status_code") not in (401, 403):
            failures.append({"type": "access_missing_token_negative", "probe": access_negative})
        if internal_negative.get("status_code") not in (401, 403):
            failures.append({"type": "internal_missing_secret_negative", "probe": internal_negative})

    status = "FAIL" if failures else "PASS_WITH_NOTES"
    payload = {
        "generated_at_unix": int(time.time()),
        "status": status,
        "mode": mode,
        "env": args.env,
        "base_url": args.base_url if mode != "dry-run" else "NOT_USED_DRY_RUN",
        "prod_live_smoke": "NOT_RUN" if mode != "read-only-live" or args.env == "prod" else "READ_ONLY_LOCAL_OR_DEV",
        "contract_routes": CONTRACT_ROUTES,
        "route_contract": route_contract,
        "route_inventory_evidence": route_inventory,
        "swagger_evidence": swagger,
        "http_probes": probes,
        "fixture_policy": {"writes_performed": False, "cleanup_evidence": "NOT_RUN_NO_MUTATION"},
        "notes": [
            "Dry-run validates route/API contract scaffolding only and cannot claim full live API closure.",
            "PASS_WITH_NOTES is the maximum status when live API evidence is NOT_RUN.",
        ],
        "failures": failures,
        "report_path": str(LATEST_REPORT),
    }
    write_report(payload)
    return 0 if status == "PASS_WITH_NOTES" else 1


if __name__ == "__main__":
    raise SystemExit(main())
