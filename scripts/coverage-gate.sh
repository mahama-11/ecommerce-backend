#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT_DIR"

BASELINE_FILE=${COVERAGE_BASELINE_FILE:-scripts/coverage-baseline.json}
REPORT_DIR=${COVERAGE_REPORT_DIR:-reports/quality/coverage}
mkdir -p "$REPORT_DIR"
NORMAL_PROFILE="$REPORT_DIR/cover.out"
COVERPKG_PROFILE="$REPORT_DIR/cover-allpkg.out"
NORMAL_LOG="$REPORT_DIR/go-test-cover.log"
COVERPKG_LOG="$REPORT_DIR/go-test-cover-allpkg.log"
REPORT_JSON="$REPORT_DIR/latest.json"

export GOTOOLCHAIN=${GOTOOLCHAIN:-auto}

normal_status=0
go test ./... -count=1 -covermode=atomic -coverprofile="$NORMAL_PROFILE" 2>&1 | tee "$NORMAL_LOG" || normal_status=${PIPESTATUS[0]}
coverpkg_status=0
go test ./... -count=1 -covermode=atomic -coverpkg=./... -coverprofile="$COVERPKG_PROFILE" 2>&1 | tee "$COVERPKG_LOG" || coverpkg_status=${PIPESTATUS[0]}

python3 - "$BASELINE_FILE" "$NORMAL_PROFILE" "$COVERPKG_PROFILE" "$NORMAL_LOG" "$REPORT_JSON" "$normal_status" "$coverpkg_status" <<'PY'
import json, re, subprocess, sys, time
from pathlib import Path

baseline_path, normal_profile, coverpkg_profile, normal_log, report_path, normal_status, coverpkg_status = sys.argv[1:]
baseline = json.loads(Path(baseline_path).read_text())
normal_status = int(normal_status)
coverpkg_status = int(coverpkg_status)

def cover_total(profile: str) -> float:
    out = subprocess.check_output(["go", "tool", "cover", "-func", profile], text=True, stderr=subprocess.STDOUT)
    m = re.search(r"total:\s+\(statements\)\s+([0-9.]+)%", out)
    if not m:
        raise SystemExit(f"cannot parse total coverage from {profile}")
    return float(m.group(1))

def package_coverages(profile_path: str):
    result = {}
    by_pkg = {}
    path = Path(profile_path)
    if not path.exists():
        return result
    for line in path.read_text(errors="replace").splitlines()[1:]:
        parts = line.split()
        if len(parts) != 3:
            continue
        file_range, stmts_s, count_s = parts
        file_path = file_range.split(':', 1)[0]
        pkg = '/'.join(file_path.split('/')[:-1])
        try:
            stmts = int(stmts_s); count = int(count_s)
        except ValueError:
            continue
        covered = stmts if count > 0 else 0
        current = by_pkg.setdefault(pkg, [0, 0])
        current[0] += covered; current[1] += stmts
    for pkg, (covered, total) in by_pkg.items():
        if total:
            result[pkg] = round(covered * 100.0 / total, 1)
    return result

def stable_package_coverages(profile_path: str, required_packages: list[str]):
    # `go test -coverprofile` can leave the aggregate profile briefly truncated
    # on this Go 1.25 auto-toolchain lane when no-test packages emit covdata
    # diagnostics. Wait for the profile to settle before declaring package
    # floors missing; otherwise the gate false-fails while the final profile
    # already contains the required packages.
    packages = {}
    missing = set(required_packages)
    last_size = -1
    stable_reads = 0
    path = Path(profile_path)
    for _ in range(30):
        packages = package_coverages(profile_path)
        missing = {pkg for pkg in required_packages if pkg not in packages}
        size = path.stat().st_size if path.exists() else -1
        if not missing:
            return packages
        if size == last_size:
            stable_reads += 1
            if stable_reads >= 3:
                return packages
        else:
            stable_reads = 0
            last_size = size
        time.sleep(0.1)
    return packages

def has_real_test_failure(log_path: str) -> bool:
    text = Path(log_path).read_text(errors="replace") if Path(log_path).exists() else ""
    return bool(re.search(r"(?m)^--- FAIL:|^FAIL\s+\S+", text))

def go_packages_without_tests():
    out = subprocess.check_output(["go", "list", "-json", "./..."], text=True)
    dec = json.JSONDecoder(); i = 0; missing = []
    while i < len(out):
        while i < len(out) and out[i].isspace():
            i += 1
        if i >= len(out):
            break
        obj, j = dec.raw_decode(out, i); i = j
        if not obj.get("TestGoFiles") and not obj.get("XTestGoFiles"):
            missing.append(obj["ImportPath"])
    return missing

normal_total = cover_total(normal_profile) if Path(normal_profile).exists() else 0.0
coverpkg_total = cover_total(coverpkg_profile) if Path(coverpkg_profile).exists() else 0.0
packages = stable_package_coverages(normal_profile, list(baseline.get("packages", {}).keys()))
failures = []

if normal_status != 0 and (not Path(normal_profile).exists() or has_real_test_failure(normal_log)):
    failures.append({"type": "go_test", "target": "normal", "status": normal_status})
if coverpkg_status != 0 and (not Path(coverpkg_profile).exists() or has_real_test_failure(str(Path(coverpkg_profile).with_name('go-test-cover-allpkg.log')))):
    failures.append({"type": "go_test", "target": "coverpkg", "status": coverpkg_status})
if normal_total < float(baseline.get("total_min_percent", 0)):
    failures.append({"type": "coverage_total", "actual": normal_total, "min": baseline.get("total_min_percent")})
if coverpkg_total < float(baseline.get("coverpkg_total_min_percent", 0)):
    failures.append({"type": "coverage_coverpkg_total", "actual": coverpkg_total, "min": baseline.get("coverpkg_total_min_percent")})

for pkg, floor in baseline.get("packages", {}).items():
    actual = packages.get(pkg)
    if actual is None:
        failures.append({"type": "package_coverage_missing", "package": pkg, "min": floor})
    elif actual < float(floor):
        failures.append({"type": "package_coverage", "package": pkg, "actual": actual, "min": floor})

missing_tests = set(go_packages_without_tests())
required_missing = sorted(pkg for pkg in baseline.get("required_test_packages", []) if pkg in missing_tests)
# Required packages start as advisory until phases B/E have landed tests.
advisories = [{"type": "required_test_package_missing", "package": pkg} for pkg in required_missing]

report = {
    "generated_at_unix": int(time.time()),
    "status": "PASS" if not failures else "FAIL",
    "normal_total_percent": normal_total,
    "coverpkg_total_percent": coverpkg_total,
    "package_coverage_percent": packages,
    "baseline": baseline,
    "failures": failures,
    "advisories": advisories,
    "profiles": {"normal": normal_profile, "coverpkg": coverpkg_profile},
    "logs": {"normal": normal_log, "coverpkg": str(Path(coverpkg_profile).with_name('go-test-cover-allpkg.log'))},
}
Path(report_path).write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n")
print(json.dumps({"status": report["status"], "normal_total_percent": normal_total, "coverpkg_total_percent": coverpkg_total, "failures": failures, "advisory_count": len(advisories)}, ensure_ascii=False))
if failures:
    raise SystemExit(1)
PY
