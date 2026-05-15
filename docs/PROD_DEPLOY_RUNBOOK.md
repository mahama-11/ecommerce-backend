# Agent Ecommerce Prod Deploy Runbook

This runbook standardizes Agent Ecommerce backend production deploys, drift checks, and Visual Workflow smoke evidence. It intentionally keeps Platform as the shared truth source and verifies only Ecommerce topology/config, product workflow routes, and the Platform integration contract.

## Safety defaults

- The deploy flow preserves remote `/root/gk/ecommerce-backend/config.prod.yaml` by default.
- Repository `config.prod.yaml` may contain placeholders; it is uploaded only when `UPLOAD_PROD_CONFIG=1` or `--upload-prod-config` is explicitly set and placeholder scan passes.
- Scripts must not print secrets, JWTs, provider keys, DB passwords, or internal service secrets. Secret comparisons use booleans/hash prefixes only.
- Smoke generates a short-lived JWT inside the remote process from remote Ecommerce `platform.jwt_secret`; it is never printed and is never passed through `curl` command-line headers. Drift-check fails critical when this prod config key is missing or placeholder-shaped.
- Deploy is phased/resumable and safe to dry-run before any mutation.

## Topology manifest

Non-secret prod topology lives in:

```text
ops/topology/prod.env
```

It defines prod/dev container names, ports, Docker network, remote paths, DB container, Platform URLs, and expected Ecommerce product/runtime table names.

## Common commands

Read-only dry run:

```bash
./tools/prod/ecommerce-drift-check.sh --env prod --dry-run --fail-on-critical
./tools/prod/ecommerce-visual-workflow-smoke.sh --env prod --dry-run --fail-on-critical
./tools/prod/ecommerce-deploy.sh all --env prod --dry-run
```

Read-only prod drift gate:

```bash
./tools/prod/ecommerce-drift-check.sh --env prod --fail-on-critical
```

Visual Workflow prod smoke:

```bash
./tools/prod/ecommerce-visual-workflow-smoke.sh --env prod --fail-on-critical
```

Full deploy after merge:

```bash
git checkout main
git pull --ff-only origin main
./tools/prod/ecommerce-deploy.sh all --env prod
```

## Phased/resumable deploy

```bash
./tools/prod/ecommerce-deploy.sh build --env prod
./tools/prod/ecommerce-deploy.sh upload --env prod
./tools/prod/ecommerce-deploy.sh restart --env prod
./tools/prod/ecommerce-deploy.sh drift-check --env prod
./tools/prod/ecommerce-deploy.sh smoke --env prod
./tools/prod/ecommerce-deploy.sh evidence --env prod
```

`upload` copies the image artifact and `docker-compose.yml`. It does **not** upload prod config unless `--upload-prod-config` is explicitly provided.

## Drift layers

`ecommerce-drift-check.sh` checks:

- prod Ecommerce and Platform containers exist and are healthy;
- Ecommerce `/healthz` and `/readyz` return HTTP 200 and API `code=0` where present;
- Ecommerce inbound callback secret `security.service_secret_key` is present and not placeholder-shaped;
- Ecommerce outbound `platform.internal_service_secret` hash matches Platform `security.internal_service_secret` hash;
- Ecommerce `platform.base_url` points at the prod Platform internal URL, not dev container/port;
- required product, image runtime, and Visual Workflow tables exist;
- Platform `runtime_product_endpoints.ecommerce` points at the prod Ecommerce internal URL, is active, has non-placeholder secret, and the endpoint secret hash matches Ecommerce inbound callback secret.

## Smoke layers

`ecommerce-visual-workflow-smoke.sh` performs an authenticated synthetic workflow on prod port `8296`:

1. Generates a short-lived HS256 JWT from remote Ecommerce `platform.jwt_secret` inside the remote Python process; it is used in memory for HTTP requests and never printed.
2. Creates a synthetic product through `POST /api/v1/ecommerce/products`.
3. Creates a Visual Workflow V2 session through `POST /api/v1/ecommerce/v2/visual-workflows/sessions`.
4. Verifies `GET /api/v1/ecommerce/v2/visual-workflows/:session_id/stage-view` returns HTTP 200 and `code=0`.
5. Calls `POST /api/v1/ecommerce/v2/visual-workflows/:session_id/intent-planner-jobs`.

Intent-planner boundary result handling:

- If Ecommerce returns a `runtime_job_id`, the script polls Platform internal runtime job detail when the Platform internal API is reachable and configured.
- If no runtime can be created because the workflow has no confirmed deconstruction selection, `DECONSTRUCTION_SELECTION_REQUIRED` / honest `contract_needed` is treated as `PASS_WITH_NOTES` for the boundary.
- Other missing-runtime or non-`code=0` responses are failures.

The smoke intentionally creates synthetic prod records and does not delete them, preserving auditability.

## Evidence

The deploy `drift-check` and `smoke` phases capture redacted local logs under:

```text
artifacts/prod/evidence/ecommerce-drift-<tag>.log
artifacts/prod/evidence/ecommerce-visual-workflow-smoke-<tag>.log
```

The deploy `evidence` phase writes a local Markdown file under:

```text
artifacts/prod/evidence/
```

The file records image tag, build method, remote target, and follow-up commands. It does not include secrets.

## Remaining risks / operator notes

- Prod smoke mutates product-owned tables by creating synthetic product/workflow rows. Use the `prod-smoke` SKU/tag prefix to filter or archive later.
- Drift checks validate table existence and integration secrets by hash prefix, not full semantic migrations or provider execution quality.
- Platform runtime completion depends on Platform worker/provider health. A valid boundary with `DECONSTRUCTION_SELECTION_REQUIRED` is a pass-with-notes because full intent planning requires selected deconstruction elements.
- If drift reports secret mismatch, remediate through the Platform endpoint/callback-secret workflow rather than hand-editing secrets in logs or shell history.
