# Agent Ecommerce Backend

`v-ecommerce-backend` is the product backend for Agent Ecommerce.

## Scope

This service owns product-facing orchestration and product-owned persistence for Agent Ecommerce workflows, while reusing `v-platform-backend` for shared identity, org, RBAC, and wallet truth.

Current backend surfaces include:

- product-facing register, login, session, and access projection
- template center catalog, detail, recommendation, favorite, copy, and use-route resolution
- workspace persistence for saved templates, workflow events, linked assets, linked deliveries, and template bridges
- product-owned user preferences and activity persistence
- infrastructure baseline for metrics, tracing, request context, access logs, schema migration tracking, and internal service authentication

## Quick Start

```bash
cd v-ecommerce-backend
go mod tidy
go run ./cmd/server -config config.local
```

## Common Commands

```bash
make tidy
make test
make run
make build
```

## Runtime Notes

- local default DB is `sqlite` at `data/ecommerce.db`
- enable Redis cache by setting `redis.enabled=true`
- register/login/session depend on `v-platform-backend`
- template center routes are exposed under `/api/v1/ecommerce/template-center/*`
- metrics are exposed at `/metrics` when enabled
- readiness is exposed at `/readyz`

## Documentation

- [Backend Guide](docs/BACKEND_GUIDE.md)
- [Service Boundary](docs/architecture/SERVICE_BOUNDARY.md)
- [Project Agent Context](AGENTS.md)
