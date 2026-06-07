#!/usr/bin/env python3
"""Safe Ecommerce backend critical-journey smoke scaffold.

Default mode is dry-run: validate the critical journey contract and route surface without
starting network calls or mutating data. Active HTTP execution is intentionally opt-in
and remains read-only unless a future fixture runner adds cleanup-backed writes.

Prod is hard blocked unless ECOM_PROD_SMOKE_APPROVED=1 is present. This script never
prints tokens/secrets and report fields are redacted defensively.
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
LATEST_REPORT = REPORT_DIR / "latest.json"

SECRET_RE = re.compile(
    r"(?i)(bearer\s+[a-z0-9._~+/=-]+|authorization\s*[:=]\s*[^\s,}]+|access[_-]?token\s*[:=]\s*[^\s,}]+|refresh[_-]?token\s*[:=]\s*[^\s,}]+|password\s*[:=]\s*[^\s,}]+|secret\s*[:=]\s*[^\s,}]+|service[_-]?secret\s*[:=]\s*[^\s,}]+|postgres://[^\s]+|mysql://[^\s]+|redis://[^\s]+)"
)

CRITICAL_JOURNEYS: list[dict[str, Any]] = [
    {
        "id": "auth-session-access",
        "title": "Register -> Login -> Session -> Access/me",
        "mutates": True,
        "cleanup_required": True,
        "steps": [
            {"method": "POST", "path": "/api/v1/ecommerce/auth/register", "auth": "public", "write": True},
            {"method": "POST", "path": "/api/v1/ecommerce/auth/login", "auth": "public", "write": False},
            {"method": "GET", "path": "/api/v1/ecommerce/auth/session", "auth": "platform_jwt", "write": False},
            {"method": "GET", "path": "/api/v1/ecommerce/access/me", "auth": "platform_jwt", "write": False},
        ],
    },
    {
        "id": "product-asset-prompt",
        "title": "Product create -> detail -> source asset registration -> prompt preview",
        "mutates": True,
        "cleanup_required": True,
        "steps": [
            {"method": "POST", "path": "/api/v1/ecommerce/products", "auth": "platform_jwt", "write": True},
            {"method": "GET", "path": "/api/v1/ecommerce/products/:product_id", "auth": "platform_jwt", "write": False},
            {"method": "POST", "path": "/api/v1/ecommerce/assets/source", "auth": "platform_jwt", "write": True},
            {"method": "POST", "path": "/api/v1/ecommerce/prompts/preview", "auth": "platform_jwt", "write": False},
        ],
    },
    {
        "id": "visual-workflow-v2",
        "title": "Visual workflow session -> source reference -> deconstruction boundary -> generation version projection",
        "mutates": True,
        "cleanup_required": True,
        "steps": [
            {"method": "POST", "path": "/api/v1/ecommerce/v2/visual-workflows/sessions", "auth": "platform_jwt", "write": True},
            {"method": "POST", "path": "/api/v1/ecommerce/v2/visual-workflows/:session_id/source-references", "auth": "platform_jwt", "write": True},
            {"method": "POST", "path": "/api/v1/ecommerce/v2/visual-workflows/:session_id/deconstruction-jobs", "auth": "platform_jwt", "write": True},
            {"method": "GET", "path": "/api/v1/ecommerce/v2/visual-workflows/:session_id/generation-versions", "auth": "platform_jwt", "write": False},
        ],
    },
    {
        "id": "wallet-commercial-billing",
        "title": "Wallet summary/history -> offerings -> order/payment -> billing charge projection",
        "mutates": True,
        "cleanup_required": True,
        "steps": [
            {"method": "GET", "path": "/api/v1/ecommerce/wallet/summary", "auth": "platform_jwt", "write": False},
            {"method": "GET", "path": "/api/v1/ecommerce/wallet/history", "auth": "platform_jwt", "write": False},
            {"method": "GET", "path": "/api/v1/ecommerce/commercial/offerings", "auth": "public", "write": False},
            {"method": "POST", "path": "/api/v1/ecommerce/commercial/orders", "auth": "platform_jwt", "write": True},
            {"method": "POST", "path": "/api/v1/ecommerce/commercial/orders/:orderID/confirm-payment", "auth": "platform_jwt", "write": True},
            {"method": "GET", "path": "/api/v1/ecommerce/billing/charges", "auth": "platform_jwt", "write": False},
        ],
    },
    {
        "id": "promotion-commission",
        "title": "Promotion resolve/signup attribution -> commission overview/redeem projection",
        "mutates": True,
        "cleanup_required": True,
        "steps": [
            {"method": "GET", "path": "/api/v1/ecommerce/promotions/codes/:code/resolve", "auth": "public", "write": False},
            {"method": "POST", "path": "/api/v1/ecommerce/promotions/me/codes/ensure", "auth": "platform_jwt", "write": True},
            {"method": "GET", "path": "/api/v1/ecommerce/commissions/me/overview", "auth": "platform_jwt", "write": False},
            {"method": "POST", "path": "/api/v1/ecommerce/commissions/me/referrals/redeem", "auth": "platform_jwt", "write": True},
        ],
    },
    {
        "id": "export-download",
        "title": "Export package/download content path",
        "mutates": True,
        "cleanup_required": True,
        "steps": [
            {"method": "POST", "path": "/api/v1/ecommerce/export-packages", "auth": "platform_jwt", "write": True},
            {"method": "GET", "path": "/api/v1/ecommerce/export-packages/:package_id", "auth": "platform_jwt", "write": False},
            {"method": "GET", "path": "/api/v1/ecommerce/downloads/:download_id/content", "auth": "platform_jwt", "write": False},
        ],
    },
    {
        "id": "internal-runtime-callbacks",
        "title": "Internal runtime callback/result update path",
        "mutates": True,
        "cleanup_required": True,
        "steps": [
            {"method": "POST", "path": "/internal/v1/ecommerce/jobs/:jobID/runtime", "auth": "internal_service", "write": True},
            {"method": "POST", "path": "/internal/v1/ecommerce/jobs/:jobID/results", "auth": "internal_service", "write": True},
        ],
    },
]

ROUTER_FRAGMENTS = sorted({
    '"/register"', '"/login"', '"/session"', '"/access/me"', '"/wallet/summary"', '"/wallet/history"',
    '"/commercial/offerings"', '"/commercial/orders"', '"/commercial/orders/:orderID/confirm-payment"',
    '"/billing/charges"', '"/promotions/codes/:code/resolve"', '"/promotions/me/codes/ensure"',
    '"/commissions/me/overview"', '"/commissions/me/referrals/redeem"', '"/products"', '"/products/:product_id"',
    '"/assets/source"', '"/prompts/preview"', '"/v2/visual-workflows/sessions"',
    '"/v2/visual-workflows/:session_id/source-references"', '"/v2/visual-workflows/:session_id/deconstruction-jobs"',
    '"/v2/visual-workflows/:session_id/generation-versions"', '"/export-packages"', '"/export-packages/:package_id"',
    '"/downloads/:download_id/content"', '"/jobs/:jobID/runtime"', '"/jobs/:jobID/results"',
})


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


def router_static_check() -> dict[str, Any]:
    router_path = ROOT / "internal" / "router" / "router.go"
    if not router_path.exists():
        return {"status": "FAIL", "missing_file": str(router_path), "missing_fragments": ROUTER_FRAGMENTS}
    text = router_path.read_text(encoding="utf-8", errors="ignore")
    missing = [fragment for fragment in ROUTER_FRAGMENTS if fragment not in text]
    return {
        "status": "PASS" if not missing else "FAIL",
        "router_path": str(router_path),
        "required_fragments": len(ROUTER_FRAGMENTS),
        "missing_fragments": missing,
    }


def http_probe(base_url: str, path: str, headers: dict[str, str] | None = None, method: str = "GET") -> dict[str, Any]:
    url = base_url.rstrip("/") + path
    req = urllib.request.Request(url, method=method, headers=headers or {})
    started = time.time()
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            body = resp.read(2048).decode("utf-8", "replace")
            return {"path": path, "method": method, "status_code": resp.status, "elapsed_ms": int((time.time() - started) * 1000), "body_sample": body[:200]}
    except urllib.error.HTTPError as exc:
        body = exc.read(2048).decode("utf-8", "replace")
        return {"path": path, "method": method, "status_code": exc.code, "elapsed_ms": int((time.time() - started) * 1000), "body_sample": body[:200]}
    except Exception as exc:  # network errors are evidence, not stack traces with env
        return {"path": path, "method": method, "status": "ERROR", "error_type": type(exc).__name__, "elapsed_ms": int((time.time() - started) * 1000)}


def build_journey_results(mode: str) -> list[dict[str, Any]]:
    results = []
    for journey in CRITICAL_JOURNEYS:
        results.append({
            "id": journey["id"],
            "title": journey["title"],
            "status": "NOT_RUN" if mode == "dry-run" else "READ_ONLY_PARTIAL",
            "mutates": bool(journey["mutates"]),
            "cleanup_required": bool(journey["cleanup_required"]),
            "cleanup_evidence": "NOT_RUN_NO_MUTATION",
            "steps": journey["steps"],
        })
    return results


def main() -> int:
    parser = argparse.ArgumentParser(description="Ecommerce backend critical journey smoke scaffold (safe dry-run default).")
    parser.add_argument("--env", choices=["local", "dev", "prod"], default="local")
    parser.add_argument("--dry-run", action="store_true", default=True, help="Validate journey contract only; no network and no writes (default).")
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
    route_check = router_static_check()
    probes: list[dict[str, Any]] = []
    if mode == "read-only-live":
        probes.append(http_probe(args.base_url, "/healthz"))
        probes.append(http_probe(args.base_url, "/readyz"))
        probes.append(http_probe(args.base_url, "/api/v1/ecommerce/health"))
        probes.append(http_probe(args.base_url, "/api/v1/ecommerce/wallet/summary"))  # should fail closed without token

    failures = []
    if route_check["status"] != "PASS":
        failures.append({"type": "route_contract", "details": route_check})
    if mode == "read-only-live":
        health_failures = [p for p in probes[:3] if p.get("status_code") not in (200, 204)]
        auth_probe = probes[-1] if probes else {}
        if health_failures:
            failures.append({"type": "read_only_health_probe", "probes": health_failures})
        if auth_probe.get("status_code") not in (401, 403):
            failures.append({"type": "protected_route_missing_token_negative", "probe": auth_probe})

    status = "FAIL" if failures else "PASS_WITH_NOTES"
    payload = {
        "generated_at_unix": int(time.time()),
        "status": status,
        "mode": mode,
        "env": args.env,
        "base_url": args.base_url if mode != "dry-run" else "NOT_USED_DRY_RUN",
        "prod_live_smoke": "NOT_RUN" if mode != "read-only-live" or args.env == "prod" else "READ_ONLY_LOCAL_OR_DEV",
        "route_contract": route_check,
        "journeys": build_journey_results(mode),
        "http_probes": probes,
        "fixture_policy": {
            "dry_run_default": True,
            "writes_performed": False,
            "write_steps_require_isolated_fixture_and_cleanup_evidence": True,
            "synthetic_record_ids": [],
            "cleanup_evidence": "NOT_RUN_NO_MUTATION",
        },
        "notes": [
            "Dry-run validates the critical journey route/step contract but intentionally does not claim full live business closure.",
            "PASS_WITH_NOTES is the maximum status when live journey evidence is NOT_RUN.",
        ],
        "failures": failures,
        "report_path": str(LATEST_REPORT),
    }
    write_report(payload)
    return 0 if status == "PASS_WITH_NOTES" else 1


if __name__ == "__main__":
    raise SystemExit(main())
