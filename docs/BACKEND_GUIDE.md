# Agent Ecommerce Backend Guide

## 1. Purpose

This guide defines the current product scope and engineering baseline of `v-ecommerce-backend`.

## 2. Intended Capability Scope

Should belong here:

- `/api/v1/ecommerce/auth/*`
- `/api/v1/ecommerce/access/*`
- `/api/v1/ecommerce/template-center/*`
- `/api/v1/ecommerce/templates/*`
- `/api/v1/ecommerce/workflow/*`
- `/api/v1/ecommerce/assets/*`
- `/api/v1/ecommerce/jobs/*`
- `/api/v1/ecommerce/deliveries/*`
- future `/api/v1/ecommerce/jobs/*`, `/reports/*`, `/catalog/*`, `/designer/*` product workflows

Should not belong here:

- shared login/register/user/org truth
- shared membership/RBAC truth
- shared wallet, subscription, payment, refund, or metering truth
- shared channel settlement infrastructure

## 3. Current Implementation Goal

The current backend slice intentionally focuses on replacing the most fragile frontend mock state while also wiring the correct shared platform dependencies:

- saved templates
- template center preset catalog, favorites, copy-to-my-templates, and use-route handoff
- workflow feed events
- linked design assets
- linked delivery bundles
- design-to-agent template bridges
- product-owned product center CRUD, status machine, listing, profit, export, and activity flows
- product-owned download-center aggregation sourced from export tasks
- product-facing register/login/session orchestration
- product-facing access resolution backed by platform identity and permission context
- product-facing wallet summary projection for current org credits
- product-facing wallet history projection with product-scoped reward, commission, wallet, and billing aggregation
- product-facing commerce ordering and payment orchestration built on shared platform offerings
- product-facing billing charge projection, refund projection, and channel-report replay orchestration
- product-facing promotion program, invite code, conversion, and signup attribution orchestration
- product-facing commission overview, redeem flow, and channel commission / settlement projection
- product-owned image-generation callback projection from platform runtime into ecommerce jobs and assets
- product-facing source asset registration and image job creation/query routes for the first `image_to_image` slice

This creates a stable product contract before moving visual tools, async jobs, or deeper billing-heavy flows.

## 4. Integration Principle

When Agent Ecommerce needs identity, org, permission, or billing truth, call `v-platform-backend` through stable APIs.
Do not duplicate shared tables as a second source of truth.

When reading shared wallet, reward, commission, discount, settlement, or usage data from platform, always pass the Agent Ecommerce `product_code` and keep the product scope explicit in product aggregation code.
Do not rely on subject-level platform reads that omit `product_code`, and do not use any cross-product query mode in normal product flows.

For product image generation, use the platform runtime as the execution layer and keep product-facing job / asset ownership inside `v-ecommerce-backend`. See [Image Generation Runtime Integration](architecture/IMAGE_GENERATION_RUNTIME_INTEGRATION.md).

## 5. Engineering Status

- Go service runtime is active as a runnable product backend
- platform register/login/session remain sourced from `v-platform-backend`, then projected into product-facing auth responses
- `gorm` storage is active with `sqlite` default and `postgres`-ready configuration
- `redis` cache wiring is active for workspace list queries when enabled
- API envelopes are normalized through `pkg/response`
- workspace state is persisted in product-owned tables instead of frontend-only local storage
- request-scoped `request_id` and `trace_id` are now propagated through middleware and API responses
- structured JSON access logs, Prometheus metrics, and OTel tracing bootstrap are now part of the backend baseline
- internal service authentication is available under `/internal/v1/ecommerce/*`
- template center API success-path tests now cover catalog, detail, favorite, copy, and use flows
- first image runtime integration slice is active with product-owned `ecommerce_image_jobs` and `ecommerce_assets`
- first Prompt Center vertical slice is active with product-owned `ecommerce_prompt_runs`, immutable preview snapshots, source maps, content hashes, schema versioning, compiled final prompts, and prompt-bound image job metadata/runtime manifests
- first public Prompt Center routes are available under:
  - `POST /api/v1/ecommerce/prompts/preview`
  - `GET /api/v1/ecommerce/prompts/:promptId`
- first public image runtime routes are available under:
  - `POST /api/v1/ecommerce/assets/source`
  - `GET /api/v1/ecommerce/image-jobs`
  - `POST /api/v1/ecommerce/image-jobs`
  - `GET /api/v1/ecommerce/image-jobs/:jobID`
- first Visual Workflow V2 S1+S2 foundation is available under:
  - `POST /api/v1/ecommerce/products/:product_id/v2/visual-sessions`
  - `POST /api/v1/ecommerce/v2/visual-workflows/sessions`
  - `GET/PATCH /api/v1/ecommerce/v2/visual-workflows/:session_id`
  - `GET /api/v1/ecommerce/v2/visual-workflows/:session_id/stage-view`
  - `POST/GET/PATCH /api/v1/ecommerce/v2/visual-workflows/:session_id/generation-versions[...]`, `POST /api/v1/ecommerce/v2/visual-workflows/:session_id/generation-versions/:version_id/select`, and `POST /api/v1/ecommerce/v2/visual-workflows/:session_id/generation-versions/:version_id/writeback-selected-asset`
  - `POST/GET/PATCH /api/v1/ecommerce/v2/visual-workflows/:session_id/source-references[...]`
  - `POST/GET /api/v1/ecommerce/v2/visual-workflows/:session_id/deconstruction-jobs[...]`
  - `GET/PATCH /api/v1/ecommerce/v2/visual-workflows/:session_id/deconstruction-elements[...]`
  - `POST /api/v1/ecommerce/v2/visual-workflows/:session_id/deconstruction-elements:confirm`
  - visual sessions validate `product_id` + `sku_code` against Product Center; source references persist `product_asset` / `platform_source_ref` metadata, while URL/video analysis stays `contract_needed` until a source-analysis runtime contract exists. Deconstruction now uses a capability-gated Platform runtime path: Ecommerce checks the `ecommerce` / `image_understanding` capability matrix, creates a Platform runtime job only when capability and contract state are ready, and otherwise records a sanitized `contract_needed` / capability blocker without making provider calls.
  - stage-view returns the shared V2 vocabulary (`session_id`, `product_id`, `sku_code`, `current_stage`, `status`, `readiness`, `source_reference`, `deconstruction_job`, `deconstruction_elements`, `intent_spec`, `prompt_plan`, `generation_versions`) plus sanitized Platform runtime capability readiness (`runtime_capabilities`, `runtime_capability_error`) without provider/storage-key leakage.
  - public deconstruction create/get/stage-view responses use a sanitized DTO (status/stage/progress/runtime reference only) and never expose raw manifests, stored metadata, client idempotency keys, provider payloads, storage keys, or billing truth; request metadata is allowlist-scrubbed before persistence.
  - deconstruction element selection/confirmation is persisted on product-owned rows.
  - source asset registration now requires `product_id` and `sku_code`
  - visual workflow generation versions persist typed product-owned version/refinement state in the session JSON projection via session-scoped create/list/get/update/select routes; create is idempotency-key aware, snapshots prompt/intent references, rejects provider/runtime artifacts, and keeps stage-view generation readiness blocked with `CONTRACT_NEEDED` until the real Platform runtime execution contract exists
  - selected generation asset writeback is available through `POST /api/v1/ecommerce/v2/visual-workflows/:session_id/generation-versions/:version_id/writeback-selected-asset`; it validates the current org/session/product/SKU, requires the selected asset to exist in `ecommerce_assets` and belong to that generation version's `result_assets`, then creates or updates the Product Center `ecom_asset_relation` with `relation_type=result`, default `asset_role=hero`, optional primary clearing, idempotent replay by key or product+asset, and a sanitized generation-version `metadata.writeback` projection. It does not call providers, Platform runtime, Prompt Center execution, billing, listing/export mutation, or expose storage keys.
  - image job creation now requires `product_id` and `sku_code`; the formal path accepts `prompt_id` and uses the persisted Prompt Center snapshot before legacy prompt/template fields
  - source assets are validated against the bound product before job creation
  - generated result assets are automatically archived into the bound product asset set
  - `GET /api/v1/ecommerce/image-jobs` can be filtered by `productID` for product-scoped tool history
  - Asset Library governance routes are available under `GET /api/v1/ecommerce/assets/library`, `GET /api/v1/ecommerce/assets/library/stats`, `GET /api/v1/ecommerce/assets/library/:relationId/lineage`, `PATCH /api/v1/ecommerce/assets/library/:relationId/governance`, and `PATCH /api/v1/ecommerce/assets/library/batch-governance`
  - Asset Library list supports server-side filters for `source_type`, `product_id`, `sku_code`, `asset_role`/`role`, `visibility`, `status`, `tag`, and `q`; list responses expose asset content proxy/reference URLs instead of internal storage keys
- commerce routes are available under:
  - `GET /api/v1/ecommerce/commercial/offerings`
  - `POST /api/v1/ecommerce/commercial/orders`
  - `GET /api/v1/ecommerce/commercial/orders`
  - `GET /api/v1/ecommerce/commercial/orders/:orderID`
  - `POST /api/v1/ecommerce/commercial/orders/:orderID/confirm-payment`
- wallet routes are available under:
  - `GET /api/v1/ecommerce/wallet/summary`
  - `GET /api/v1/ecommerce/wallet/history`
- billing routes are available under:
  - `GET /api/v1/ecommerce/billing/summary`
  - `GET /api/v1/ecommerce/billing/charges`
- promotion routes are available under:
  - `GET /api/v1/ecommerce/promotions/codes/:code/resolve`
  - `GET /api/v1/ecommerce/promotions/programs`
  - `GET /api/v1/ecommerce/promotions/me/overview`
  - `GET /api/v1/ecommerce/promotions/me/codes`
  - `POST /api/v1/ecommerce/promotions/me/codes/ensure`
  - `POST /api/v1/ecommerce/promotions/me/codes`
  - `GET /api/v1/ecommerce/promotions/me/conversions`
- commission routes are available under:
  - `GET /api/v1/ecommerce/commissions/me/overview`
  - `GET /api/v1/ecommerce/commissions/me/referrals`
  - `POST /api/v1/ecommerce/commissions/me/referrals/redeem`
  - `GET /api/v1/ecommerce/commissions/me/channel/overview`
  - `GET /api/v1/ecommerce/commissions/me/channel/bindings`
  - `GET /api/v1/ecommerce/commissions/me/channel/commissions`
  - `GET /api/v1/ecommerce/commissions/me/channel/settlements`
- internal runtime callbacks are reserved under:
  - `/internal/v1/ecommerce/jobs/:jobID/runtime`
  - `/internal/v1/ecommerce/jobs/:jobID/results`
- internal billing settlement hooks are available under:
  - `POST /internal/v1/ecommerce/commercial/billing/charges`
  - `POST /internal/v1/ecommerce/commercial/billing/charges/:recordID/refunds`
  - `POST /internal/v1/ecommerce/commercial/outbox/replay`
- product-owned asset content is exposed under:
  - `/api/v1/ecommerce/assets/:assetID/content`
- product-owned product center routes are available under:
  - `GET /api/v1/ecommerce/products`
  - `POST /api/v1/ecommerce/products`
  - `GET /api/v1/ecommerce/products/:product_id`
  - `PATCH /api/v1/ecommerce/products/:product_id`
  - `PATCH /api/v1/ecommerce/products/:product_id/status`
  - `DELETE /api/v1/ecommerce/products/:product_id`
  - `GET /api/v1/ecommerce/products/:product_id/assets`
  - `POST /api/v1/ecommerce/products/:product_id/assets`
  - `DELETE /api/v1/ecommerce/products/:product_id/assets/:asset_relation_id`
  - `GET /api/v1/ecommerce/products/:product_id/listing-versions`
  - `POST /api/v1/ecommerce/products/:product_id/listing-versions`
  - `POST /api/v1/ecommerce/products/listing-versions/batch`
  - `POST /api/v1/ecommerce/products/:product_id/listing-versions/adopt`
  - `POST /api/v1/ecommerce/products/listing-versions/batch-adopt`
  - `PATCH /api/v1/ecommerce/products/:product_id/listing-versions/:version_id`
  - `DELETE /api/v1/ecommerce/products/:product_id/listing-versions/:version_id`
  - `GET /api/v1/ecommerce/products/:product_id/profit-snapshots`
  - `POST /api/v1/ecommerce/products/:product_id/profit-snapshots/calculate`
  - `GET /api/v1/ecommerce/products/:product_id/export-tasks`
  - `POST /api/v1/ecommerce/products/:product_id/export-tasks`
  - `POST /api/v1/ecommerce/export-packages`
  - `PATCH /api/v1/ecommerce/products/:product_id/export-tasks/status`
- product-owned download center routes are available under:
  - `GET /api/v1/ecommerce/downloads`
  - `GET /api/v1/ecommerce/downloads/:download_id/content`
- template center example assets are resolved from `v-platform-backend` at request time
- template center example preview is proxied under:
  - `GET /api/v1/ecommerce/template-center/assets/preview?storage_key=...`
- template center list responses can surface `coverAssetUrl` from resolved example assets
- template center detail responses can surface `storageKey`, `assetId`, `mimeType`, `checksum`, and `previewAssetUrl`
- startup now logs a template center seed summary and warns when built-in example assets are missing
- automated validation currently covers package compilation, workspace module tests, and template center API success paths
- billing, template center, and image runtime paths now have targeted handler/service validation in the backend test suite
- download center currently aggregates product export tasks and multi-SKU export package rows at organization scope and exposes authenticated download streaming when `storage_key` is available, with `package_url` kept as a direct-download fallback
- download-center payloads now include linked asset manifest snippets and package metadata/content URLs so frontend pages can trace package records back to product assets
- multi-SKU export package creation supports per-SKU partial success/blockers, creates child export tasks for ready SKUs, persists a package manifest, and serves a zip bundle with `manifest.json` and `listing.csv` through `/downloads/:download_id/content`
- image-runtime tests now also cover product-bound source registration, product-bound job creation, and automatic result archival into product assets
- batch listing now has real bulk draft/preview create and adopt endpoints that accept per-product or per-`sku_code` listing payloads and return per-item success/failure details
- listing version creation now validates that the target SKU exists and is asset-ready before writing data, applies field-level listing validation, and immediately syncs product `listing_status` / main status after a new version is created
- listing version edits are immutable: the patch route creates a new draft version from the selected version plus edits; historical adopted versions are not patched, and adopted versions cannot be deleted

## 6. Runtime Configuration

- `database.driver=sqlite` is the default local mode and writes to `data/ecommerce.db`
- switch `database.driver=postgres` to use a shared or dedicated PostgreSQL instance
- `redis.enabled=true` enables workspace cache acceleration
- `monitoring.metrics.enabled=true` exposes Prometheus metrics
- `monitoring.tracing.enabled=true` enables OTel tracing; set `monitoring.tracing.backend=tempo|otlp` with `monitoring.tracing.otlp_endpoint` for Tempo/OTel Collector, or `backend=jaeger` with `jaeger_endpoint` for legacy Jaeger collector
- `platform.base_url`, `platform.internal_service_secret`, and `platform.jwt_secret` must align with platform backend runtime values
- template example source-of-truth files are:
  - `internal/modules/templatecenter/generated_seed_definitions.json`
  - `internal/modules/templatecenter/example_asset_manifest.json`
- `internal/modules/templatecenter/example_asset_manifest.resolved.json` is an operational import artifact and should not be committed

## 7. Near-Term Next Steps

- replace optional anonymous workspace fallback with stricter authenticated product sessions
- add internal API tests for product auth/session and workspace contracts beyond template center
- unify legacy workspace linked-delivery projections with the product-export-based download center when design workbench backend contracts are ready
- extend product-owned persistence to designer jobs, catalogs, and reports
