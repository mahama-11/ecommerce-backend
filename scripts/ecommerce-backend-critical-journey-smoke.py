#!/usr/bin/env python3
"""Ecommerce backend critical-journey smoke.

Default is safe dry-run (no network/no writes). Phase 2 live execution is opt-in via
--execute --fixture isolated --cleanup and runs real HTTP against an isolated local Gin
app fixture (Go httptest + temp sqlite + fake Platform httptest server).
"""
from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[1]
REPORT_DIR = ROOT / "reports" / "quality" / "business-journeys"
LATEST_REPORT = REPORT_DIR / "latest.json"

SECRET_RE = re.compile(
    r"(?i)(bearer\s+[a-z0-9._~+/=-]+|authorization\s*[:=]\s*[^\s,}]+|access[_-]?token\s*[:=]\s*[^\s,}]+|refresh[_-]?token\s*[:=]\s*[^\s,}]+|password\s*[:=]\s*[^\s,}]+|secret\s*[:=]\s*[^\s,}]+|service[_-]?secret\s*[:=]\s*[^\s,}]+|postgres://[^\s]+|mysql://[^\s]+|redis://[^\s]+|eyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+)"
)

CRITICAL_JOURNEYS: list[dict[str, Any]] = [
    {"id": "auth-session-access", "title": "Register -> Login -> Session -> Access/me", "mutates": True, "cleanup_required": True, "steps": [
        {"method": "POST", "path": "/api/v1/ecommerce/auth/register", "auth": "public", "write": True},
        {"method": "POST", "path": "/api/v1/ecommerce/auth/login", "auth": "public", "write": False},
        {"method": "GET", "path": "/api/v1/ecommerce/auth/session", "auth": "platform_jwt", "write": False},
        {"method": "GET", "path": "/api/v1/ecommerce/access/me", "auth": "platform_jwt", "write": False},
    ]},
    {"id": "product-asset-prompt", "title": "Product create -> detail -> source asset registration -> prompt preview", "mutates": True, "cleanup_required": True, "steps": [
        {"method": "POST", "path": "/api/v1/ecommerce/products", "auth": "platform_jwt", "write": True},
        {"method": "GET", "path": "/api/v1/ecommerce/products/:product_id", "auth": "platform_jwt", "write": False},
        {"method": "POST", "path": "/api/v1/ecommerce/assets/source", "auth": "platform_jwt", "write": True},
        {"method": "POST", "path": "/api/v1/ecommerce/prompts/preview", "auth": "platform_jwt", "write": True},
    ]},
    {"id": "visual-workflow-v2", "title": "Visual workflow session -> source reference -> deconstruction boundary -> generation version projection", "mutates": True, "cleanup_required": True, "steps": [
        {"method": "POST", "path": "/api/v1/ecommerce/v2/visual-workflows/sessions", "auth": "platform_jwt", "write": True},
        {"method": "POST", "path": "/api/v1/ecommerce/v2/visual-workflows/:session_id/source-references", "auth": "platform_jwt", "write": True},
        {"method": "POST", "path": "/api/v1/ecommerce/v2/visual-workflows/:session_id/deconstruction-jobs", "auth": "platform_jwt", "write": True},
        {"method": "GET", "path": "/api/v1/ecommerce/v2/visual-workflows/:session_id/generation-versions", "auth": "platform_jwt", "write": False},
    ]},
    {"id": "wallet-commercial-billing", "title": "Wallet summary/history -> offerings -> order/payment -> billing charge projection", "mutates": True, "cleanup_required": True, "steps": [
        {"method": "GET", "path": "/api/v1/ecommerce/wallet/summary", "auth": "platform_jwt", "write": False},
        {"method": "GET", "path": "/api/v1/ecommerce/wallet/history", "auth": "platform_jwt", "write": False},
        {"method": "GET", "path": "/api/v1/ecommerce/commercial/offerings", "auth": "public", "write": False},
        {"method": "POST", "path": "/api/v1/ecommerce/commercial/orders", "auth": "platform_jwt", "write": True},
        {"method": "POST", "path": "/api/v1/ecommerce/commercial/orders/:orderID/confirm-payment", "auth": "platform_jwt", "write": True},
        {"method": "GET", "path": "/api/v1/ecommerce/billing/charges", "auth": "platform_jwt", "write": False},
    ]},
    {"id": "promotion-commission", "title": "Promotion resolve/signup attribution -> commission overview/redeem projection", "mutates": True, "cleanup_required": True, "steps": []},
    {"id": "export-download", "title": "Export package/download content path", "mutates": True, "cleanup_required": True, "steps": []},
    {"id": "internal-runtime-callbacks", "title": "Internal runtime callback/result update path", "mutates": True, "cleanup_required": True, "steps": []},
]

ROUTER_FRAGMENTS = sorted({
    '"/register"', '"/login"', '"/session"', '"/access/me"', '"/wallet/summary"', '"/wallet/history"',
    '"/commercial/offerings"', '"/commercial/orders"', '"/commercial/orders/:orderID/confirm-payment"',
    '"/billing/charges"', '"/products"', '"/products/:product_id"', '"/assets/source"', '"/prompts/preview"',
})


def redact(value: Any) -> Any:
    if isinstance(value, str):
        return SECRET_RE.sub("[REDACTED]", value)
    if isinstance(value, list):
        return [redact(v) for v in value]
    if isinstance(value, dict):
        safe: dict[str, Any] = {}
        for k, v in value.items():
            if any(word in k.lower() for word in ("token", "secret", "password")):
                if isinstance(v, bool) and k.endswith("_present"):
                    safe[k] = v
                continue
            safe[k] = redact(v)
        return safe
    return value


def write_report(payload: dict[str, Any], report_path: Path = LATEST_REPORT) -> dict[str, Any]:
    REPORT_DIR.mkdir(parents=True, exist_ok=True)
    sanitized = redact(payload)
    report_path.write_text(json.dumps(sanitized, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return sanitized


def router_static_check() -> dict[str, Any]:
    router_path = ROOT / "internal" / "router" / "router.go"
    if not router_path.exists():
        return {"status": "FAIL", "missing_file": str(router_path), "missing_fragments": ROUTER_FRAGMENTS}
    text = router_path.read_text(encoding="utf-8", errors="ignore")
    missing = [fragment for fragment in ROUTER_FRAGMENTS if fragment not in text]
    return {"status": "PASS" if not missing else "FAIL", "router_path": str(router_path), "required_fragments": len(ROUTER_FRAGMENTS), "missing_fragments": missing}


def http_probe(base_url: str, path: str, method: str = "GET") -> dict[str, Any]:
    url = base_url.rstrip("/") + path
    started = time.time()
    try:
        with urllib.request.urlopen(urllib.request.Request(url, method=method), timeout=10) as resp:
            body = resp.read(2048).decode("utf-8", "replace")
            return {"path": path, "method": method, "status_code": resp.status, "elapsed_ms": int((time.time() - started) * 1000), "body_sha256": _sha256(body)}
    except urllib.error.HTTPError as exc:
        body = exc.read(2048).decode("utf-8", "replace")
        return {"path": path, "method": method, "status_code": exc.code, "elapsed_ms": int((time.time() - started) * 1000), "body_sha256": _sha256(body)}
    except Exception as exc:
        return {"path": path, "method": method, "status": "ERROR", "error_type": type(exc).__name__, "elapsed_ms": int((time.time() - started) * 1000)}


def _sha256(text: str) -> str:
    import hashlib
    return "sha256:" + hashlib.sha256(text.encode("utf-8", "replace")).hexdigest()


def build_journey_results(mode: str) -> list[dict[str, Any]]:
    status = "NOT_RUN" if mode == "dry-run" else "READ_ONLY_PARTIAL"
    return [{"id": j["id"], "title": j["title"], "status": status, "mutates": bool(j["mutates"]), "cleanup_required": bool(j["cleanup_required"]), "cleanup_evidence": "NOT_RUN_NO_MUTATION", "steps": j.get("steps", [])} for j in CRITICAL_JOURNEYS]


def run_isolated_harness() -> dict[str, Any]:
    with tempfile.NamedTemporaryFile(prefix="ecom-phase2-critical-", suffix=".json", delete=False) as tmp:
        tmp_path = Path(tmp.name)
    env = os.environ.copy()
    env["ECOM_PHASE2_SMOKE_HARNESS"] = "1"
    env["ECOM_PHASE2_SMOKE_REPORT"] = str(tmp_path)
    cmd = ["go", "test", "./internal/smoke", "-run", "TestPhase2LocalSmokeHarness", "-count=1"]
    result = subprocess.run(cmd, cwd=ROOT, env=env, text=True, capture_output=True, timeout=300)
    if result.returncode != 0:
        payload = {"status": "FAIL", "mode": "local_harness_execute", "harness_command": " ".join(cmd), "exit_code": result.returncode, "stdout_tail": result.stdout[-4000:], "stderr_tail": result.stderr[-4000:]}
        if tmp_path.exists():
            payload["partial_report"] = json.loads(tmp_path.read_text(encoding="utf-8"))
        return payload
    data = json.loads(tmp_path.read_text(encoding="utf-8"))
    try:
        tmp_path.unlink()
    except OSError:
        pass
    return data


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

    harness = run_isolated_harness()
    route_check = router_static_check()
    required = {"auth-session-access", "product-asset-prompt", "wallet-commercial-billing"}
    passed = {j.get("id") for j in harness.get("journeys", []) if j.get("status") == "PASS"}
    cleanup_pass = harness.get("cleanup", {}).get("status") == "PASS"
    status = "PASS" if harness.get("status") == "PASS" and route_check.get("status") == "PASS" and required.issubset(passed) and cleanup_pass else "FAIL"
    payload = {
        **harness,
        "status": status,
        "script": "ecommerce-backend-critical-journey-smoke.py",
        "env": args.env,
        "prod_live_smoke": "NOT_RUN",
        "prod_policy": {"status": "NOT_RUN", "reason": "prod live smoke is not executed by local isolated release-quality-gate; use the approved prod runbook separately."},
        "fixture_arg": args.fixture,
        "cleanup_arg": args.cleanup,
        "cleanup_status": harness.get("cleanup", {}).get("status"),
        "route_contract": route_check,
        "required_phase2_journeys": sorted(required),
        "required_phase2_journeys_passed": sorted(passed & required),
        "report_path": str(LATEST_REPORT),
    }
    if status != "PASS":
        payload.setdefault("failures", [])
        payload["failures"].append({"type": "phase2_acceptance", "required_passed": sorted(passed & required), "cleanup_pass": cleanup_pass, "route_contract": route_check.get("status")})
    return payload, 0 if status == "PASS" else 1


def main() -> int:
    parser = argparse.ArgumentParser(description="Ecommerce backend critical journey smoke (dry-run default; explicit local isolated execute supported).")
    parser.add_argument("--env", choices=["local", "dev", "prod"], default="local")
    parser.add_argument("--dry-run", action="store_true", help="Validate journey contract only; no network and no writes (default unless --execute/--execute-live).")
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
        print(json.dumps({"status": sanitized["status"], "report": str(LATEST_REPORT), "mode": sanitized["mode"], "journeys_passed": sanitized.get("required_phase2_journeys_passed", []), "cleanup_status": sanitized.get("cleanup", {}).get("status")}, ensure_ascii=False))
        return code

    mode = "read-only-live" if args.execute_live and args.env in {"local", "dev"} else "dry-run"
    route_check = router_static_check()
    probes: list[dict[str, Any]] = []
    if mode == "read-only-live":
        probes = [http_probe(args.base_url, p) for p in ("/healthz", "/readyz", "/api/v1/ecommerce/health", "/api/v1/ecommerce/wallet/summary")]
    failures = []
    if route_check["status"] != "PASS":
        failures.append({"type": "route_contract", "details": route_check})
    if mode == "read-only-live" and probes[-1].get("status_code") not in (401, 403):
        failures.append({"type": "protected_route_missing_token_negative", "probe": probes[-1]})
    status = "FAIL" if failures else "PASS_WITH_NOTES"
    payload = {"generated_at_unix": int(time.time()), "status": status, "mode": mode, "env": args.env, "base_url": args.base_url if mode != "dry-run" else "NOT_USED_DRY_RUN", "prod_live_smoke": "NOT_RUN", "route_contract": route_check, "journeys": build_journey_results(mode), "http_probes": probes, "fixture_policy": {"dry_run_default": True, "writes_performed": False, "cleanup_evidence": "NOT_RUN_NO_MUTATION"}, "notes": ["PASS_WITH_NOTES is the maximum status when live journey evidence is NOT_RUN."], "failures": failures, "report_path": str(LATEST_REPORT)}
    sanitized = write_report(payload)
    print(json.dumps({"status": sanitized["status"], "report": str(LATEST_REPORT), "mode": sanitized["mode"]}, ensure_ascii=False))
    return 0 if status == "PASS_WITH_NOTES" else 1


if __name__ == "__main__":
    raise SystemExit(main())
