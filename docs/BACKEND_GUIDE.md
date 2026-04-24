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
- product-facing register/login/session orchestration
- product-facing access resolution backed by platform identity and permission context
- product-facing wallet summary projection for current org credits
- product-owned image-generation callback projection from platform runtime into ecommerce jobs and assets
- product-facing source asset registration and image job creation/query routes for the first `image_to_image` slice

This creates a stable product contract before moving visual tools, async jobs, or deeper billing-heavy flows.

## 4. Integration Principle

When Agent Ecommerce needs identity, org, permission, or billing truth, call `v-platform-backend` through stable APIs.
Do not duplicate shared tables as a second source of truth.

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
- first public image runtime routes are available under:
  - `POST /api/v1/ecommerce/assets/source`
  - `GET /api/v1/ecommerce/image-jobs`
  - `POST /api/v1/ecommerce/image-jobs`
  - `GET /api/v1/ecommerce/image-jobs/:jobID`
- internal runtime callbacks are reserved under:
  - `/internal/v1/ecommerce/jobs/:jobID/runtime`
  - `/internal/v1/ecommerce/jobs/:jobID/results`
- product-owned asset content is exposed under:
  - `/api/v1/ecommerce/assets/:assetID/content`
- automated validation currently covers package compilation, workspace module tests, and template center API success paths

## 6. Runtime Configuration

- `database.driver=sqlite` is the default local mode and writes to `data/ecommerce.db`
- switch `database.driver=postgres` to use a shared or dedicated PostgreSQL instance
- `redis.enabled=true` enables workspace cache acceleration
- `monitoring.metrics.enabled=true` exposes Prometheus metrics
- `monitoring.tracing.enabled=true` enables Jaeger exporter bootstrap
- `platform.base_url`, `platform.internal_service_secret`, and `platform.jwt_secret` must align with platform backend runtime values

## 7. Near-Term Next Steps

- replace optional anonymous workspace fallback with stricter authenticated product sessions
- add internal API tests for product auth/session and workspace contracts beyond template center
- extend product-owned persistence to designer jobs, catalogs, and reports
