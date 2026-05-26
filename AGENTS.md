# Agent Ecommerce Backend - Agent Context

> This repository owns Agent Ecommerce product workflows. Reuse platform auth, org, RBAC, billing, and entitlement capabilities through service contracts instead of duplicating their source-of-truth data.

## 1. Purpose

`v-ecommerce-backend` is the product backend for Agent Ecommerce.

It should host:

- product-facing orchestration for AI commerce workflows
- template marketplace and saved-template product contracts
- workflow state, asset linkage, and delivery linkage owned by the product
- future product-owned job orchestration, asset pipelines, and reporting semantics
- backend infrastructure baseline for metrics, tracing, logs, request context, and internal service auth

It should not host:

- shared login/register truth
- shared organization/membership truth
- shared RBAC truth
- shared subscription, wallet, payment, or metering truth

## 2. Key Documents

- [Current Code Alignment](../docs/ecommerce/current-code-alignment.md)
- [Backend Guide](docs/BACKEND_GUIDE.md)
- [Service Boundary](docs/architecture/SERVICE_BOUNDARY.md)
- [Visual Workflow AI Requirement Contract](docs/architecture/VISUAL_WORKFLOW_AI_REQUIREMENT_CONTRACT.md)
- [Visual Workflow Frontend Contract](docs/architecture/VISUAL_WORKFLOW_FRONTEND_CONTRACT.md)
- [Image Generation Runtime Integration](docs/architecture/IMAGE_GENERATION_RUNTIME_INTEGRATION.md)
- [Prod Deploy Runbook](docs/PROD_DEPLOY_RUNBOOK.md)
- [Workspace Cloud Dev Deploy Runbook](../tools/dev/README.md) — Cloud dev 部署固定入口；不要用本 repo 旧 `build.sh dev` 或 prod deploy script 伪装 dev 部署。
- [OpenAPI Guide](docs/openapi/README.md)
- [Template Center Data Model](docs/architecture/TEMPLATE_CENTER_DATA_MODEL.md)

## 3. Commands

```bash
cd v-ecommerce-backend
go mod tidy
go test ./...
go run ./cmd/server -config config.local
make run
make test
```

## 4. Documentation Rules

- Add backend docs under `docs/` or `docs/architecture/`
- Keep route, boundary, and migration docs aligned with code
- Update this file and the root `AGENTS.md` whenever long-lived backend docs are added
