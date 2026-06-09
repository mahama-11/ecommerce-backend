#!/usr/bin/env python3
"""Ecommerce backend API-contract smoke.

Default is dry-run. Phase 2 live execution is opt-in via --execute --fixture isolated
--cleanup and reuses the isolated local HTTP harness, then validates the API-contract
route set against the executed evidence.
"""
from __future__ import annotations

import argparse
import importlib.util
import json
import os
import time
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[1]
REPORT_DIR = ROOT / "reports" / "quality" / "business-journeys"
LATEST_REPORT = REPORT_DIR / "api-contract-latest.json"

_CRITICAL_PATH = ROOT / "scripts" / "ecommerce-backend-critical-journey-smoke.py"
_SPEC = importlib.util.spec_from_file_location("critical_smoke", _CRITICAL_PATH)
critical_smoke = importlib.util.module_from_spec(_SPEC)  # type: ignore[arg-type]
assert _SPEC and _SPEC.loader
_SPEC.loader.exec_module(critical_smoke)  # type: ignore[union-attr]

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


def write_report(payload: dict[str, Any]) -> dict[str, Any]:
    REPORT_DIR.mkdir(parents=True, exist_ok=True)
    sanitized = critical_smoke.redact(payload)
    LATEST_REPORT.write_text(json.dumps(sanitized, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return sanitized


def route_contract_check() -> dict[str, Any]:
    router_path = ROOT / "internal" / "router" / "router.go"
    if not router_path.exists():
        return {"status": "FAIL", "missing_file": str(router_path), "missing_routes": CONTRACT_ROUTES}
    text = router_path.read_text(encoding="utf-8", errors="ignore")
    missing = [route for route in CONTRACT_ROUTES if route["literal"] not in text]
    auth_guards = {"platform_jwt_middleware": "PlatformJWTAuth" in text, "internal_service_middleware": "RequireInternalService" in text, "optional_platform_jwt_middleware": "OptionalPlatformJWTAuth" in text}
    return {"status": "PASS" if not missing and all(auth_guards.values()) else "FAIL", "router_path": str(router_path), "routes_checked": len(CONTRACT_ROUTES), "missing_routes": missing, "auth_guards": auth_guards}


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
    candidates = [ROOT / "docs" / "swagger.json", ROOT / "docs" / "swagger.yaml", ROOT / "docs" / "openapi.json", ROOT / "docs" / "openapi.yaml", ROOT / "docs" / "openapi" / "openapi.json", ROOT / "docs" / "openapi" / "swagger.json"]
    existing = [str(p) for p in candidates if p.exists()]
    return {"status": "PASS_WITH_NOTES" if script.exists() else "NOT_RUN", "generator": str(script) if script.exists() else None, "generated_specs_found": existing, "dry_run_note": "Swagger generation/drift is not run by this smoke."}


def flatten_executed_paths(harness: dict[str, Any]) -> set[tuple[str, str]]:
    paths: set[tuple[str, str]] = set()
    for journey in harness.get("journeys", []):
        for step in journey.get("steps", []):
            if step.get("status") == "PASS":
                paths.add((str(step.get("method")), str(step.get("path"))))
    for route in harness.get("http_routes", []):
        if route.get("status") == "PASS":
            paths.add((str(route.get("method")), str(route.get("path"))))
    return paths


def normalize_contract_path(path: str) -> str:
    return path.replace(":product_id", "").replace(":orderID", "")


def live_contract_evidence(harness: dict[str, Any]) -> dict[str, Any]:
    executed = flatten_executed_paths(harness)
    required_live = [
        ("POST", "/api/v1/ecommerce/auth/register"),
        ("POST", "/api/v1/ecommerce/auth/login"),
        ("GET", "/api/v1/ecommerce/auth/session"),
        ("GET", "/api/v1/ecommerce/access/me"),
        ("POST", "/api/v1/ecommerce/products"),
        ("GET", "/api/v1/ecommerce/products/"),
        ("POST", "/api/v1/ecommerce/assets/source"),
        ("POST", "/api/v1/ecommerce/prompts/preview"),
        ("GET", "/api/v1/ecommerce/wallet/summary"),
        ("GET", "/api/v1/ecommerce/wallet/history"),
        ("GET", "/api/v1/ecommerce/commercial/offerings"),
        ("POST", "/api/v1/ecommerce/commercial/orders"),
        ("POST", "/api/v1/ecommerce/commercial/orders/"),
        ("GET", "/api/v1/ecommerce/billing/charges"),
    ]
    matched: list[str] = []
    missing: list[str] = []
    for method, prefix in required_live:
        ok = any(m == method and p.startswith(prefix) for m, p in executed)
        (matched if ok else missing).append(f"{method} {prefix}")
    return {"status": "PASS" if not missing else "FAIL", "fixture": "isolated/local_harness", "executed_route_count": len(executed), "matched_required_live_routes": matched, "missing_required_live_routes": missing}


def execute_local(args: argparse.Namespace) -> tuple[dict[str, Any], int]:
    failures: list[dict[str, Any]] = []
    if args.env != "local":
        failures.append({"type": "execute_env", "message": "--execute is currently restricted to --env local for the isolated fixture"})
    if args.fixture != "isolated":
        failures.append({"type": "fixture", "message": "--execute requires --fixture isolated"})
    if not args.cleanup:
        failures.append({"type": "cleanup", "message": "--execute requires explicit --cleanup; no PASS without cleanup evidence"})
    if failures:
        return {"generated_at_unix": int(time.time()), "status": "BLOCKED", "mode": "execute-precondition-blocked", "env": args.env, "failures": failures, "report_path": str(LATEST_REPORT)}, 2

    harness = critical_smoke.run_isolated_harness()
    route_contract = route_contract_check()
    live_evidence = live_contract_evidence(harness)
    cleanup_pass = harness.get("cleanup", {}).get("status") == "PASS"
    status = "PASS" if harness.get("status") == "PASS" and route_contract.get("status") == "PASS" and live_evidence.get("status") == "PASS" and cleanup_pass else "FAIL"
    payload = {**harness, "status": status, "script": "ecommerce-backend-api-contract-smoke.py", "env": args.env, "prod_live_smoke": "NOT_RUN", "prod_policy": {"status": "NOT_RUN", "reason": "prod live API contract smoke is not executed by local isolated release-quality-gate; use the approved prod runbook separately."}, "fixture_arg": args.fixture, "cleanup_arg": args.cleanup, "cleanup_status": harness.get("cleanup", {}).get("status"), "contract_routes": CONTRACT_ROUTES, "route_contract": route_contract, "route_inventory_evidence": load_quality_routes_report(), "swagger_evidence": swagger_check(), "live_contract_evidence": live_evidence, "report_path": str(LATEST_REPORT)}
    if status != "PASS":
        payload.setdefault("failures", [])
        payload["failures"].append({"type": "api_contract_acceptance", "cleanup_pass": cleanup_pass, "route_contract": route_contract.get("status"), "live_contract": live_evidence.get("status")})
    return payload, 0 if status == "PASS" else 1


def main() -> int:
    parser = argparse.ArgumentParser(description="Ecommerce backend API contract smoke (dry-run default; explicit local isolated execute supported).")
    parser.add_argument("--env", choices=["local", "dev", "prod"], default="local")
    parser.add_argument("--dry-run", action="store_true", help="Validate static contract only; no network and no writes (default unless --execute/--execute-live).")
    parser.add_argument("--execute", action="store_true", help="Run live local isolated fixture over real HTTP. Requires --fixture isolated --cleanup.")
    parser.add_argument("--execute-live", action="store_true", help="Backward-compatible read-only HTTP probes for local/dev. Does not write.")
    parser.add_argument("--fixture", choices=["isolated"], default="isolated")
    parser.add_argument("--cleanup", action="store_true", help="Required with --execute; verifies temp fixture cleanup evidence before PASS.")
    parser.add_argument("--base-url", default=os.environ.get("ECOM_BACKEND_BASE_URL", "http://127.0.0.1:8080"))
    args = parser.parse_args()

    if args.env == "prod":
        payload = {"generated_at_unix": int(time.time()), "status": "BLOCKED", "mode": "prod-refusal", "env": args.env, "prod_live_smoke": "NOT_RUN", "reason": "prod smoke is hard-refused by this script; use a separate approved prod runbook", "report_path": str(LATEST_REPORT)}
        sanitized = write_report(payload)
        print(json.dumps({"status": sanitized["status"], "report": str(LATEST_REPORT), "mode": sanitized["mode"], "reason": sanitized["reason"]}, ensure_ascii=False))
        return 3

    if args.execute:
        payload, code = execute_local(args)
        sanitized = write_report(payload)
        print(json.dumps({"status": sanitized["status"], "report": str(LATEST_REPORT), "mode": sanitized["mode"], "live_contract": sanitized.get("live_contract_evidence", {}).get("status"), "cleanup_status": sanitized.get("cleanup", {}).get("status")}, ensure_ascii=False))
        return code

    mode = "read-only-live" if args.execute_live and args.env in {"local", "dev"} else "dry-run"
    route_contract = route_contract_check()
    route_inventory = load_quality_routes_report()
    swagger = swagger_check()
    probes: list[dict[str, Any]] = []
    if mode == "read-only-live":
        probes = [critical_smoke.http_probe(args.base_url, p) for p in ("/healthz", "/api/v1/ecommerce/health", "/api/v1/ecommerce/commercial/offerings", "/api/v1/ecommerce/access/me", "/internal/v1/ecommerce/health")]
    failures: list[dict[str, Any]] = []
    if route_contract["status"] != "PASS":
        failures.append({"type": "route_contract", "details": route_contract})
    if route_inventory.get("status") not in {"PASS", "NOT_RUN"}:
        failures.append({"type": "route_inventory_report", "details": route_inventory})
    if mode == "read-only-live" and len(probes) >= 5:
        if any(p.get("status_code") not in (200, 204) for p in probes[:3]):
            failures.append({"type": "public_read_only_probe", "probes": probes[:3]})
        if probes[3].get("status_code") not in (401, 403):
            failures.append({"type": "access_missing_token_negative", "probe": probes[3]})
        if probes[4].get("status_code") not in (401, 403):
            failures.append({"type": "internal_missing_secret_negative", "probe": probes[4]})
    status = "FAIL" if failures else "PASS_WITH_NOTES"
    payload = {"generated_at_unix": int(time.time()), "status": status, "mode": mode, "env": args.env, "base_url": args.base_url if mode != "dry-run" else "NOT_USED_DRY_RUN", "prod_live_smoke": "NOT_RUN", "contract_routes": CONTRACT_ROUTES, "route_contract": route_contract, "route_inventory_evidence": route_inventory, "swagger_evidence": swagger, "http_probes": probes, "fixture_policy": {"dry_run_default": True, "writes_performed": False, "cleanup_evidence": "NOT_RUN_NO_MUTATION"}, "notes": ["PASS_WITH_NOTES is the maximum status when live API evidence is NOT_RUN."], "failures": failures, "report_path": str(LATEST_REPORT)}
    sanitized = write_report(payload)
    print(json.dumps({"status": sanitized["status"], "report": str(LATEST_REPORT), "mode": sanitized["mode"]}, ensure_ascii=False))
    return 0 if status == "PASS_WITH_NOTES" else 1


if __name__ == "__main__":
    raise SystemExit(main())
