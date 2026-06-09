# Ecommerce Backend Full Business Quality Closure Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** 把 `ecommerce-backend` 从“核心模块有测试、质量中等”推进到“登录会话、权限、商业化、钱包、平台契约、路由中间件、存储、观测都被可执行质量门禁覆盖”的全业务质量闭环。

**Architecture:** 采用“质量控制面先行 + 业务纵切补测 + 契约/真实 smoke + CI/SelfCheck 固化”的方式。先建立 coverage/route/contract/evidence 统一门禁，再按业务风险补齐模块测试与 API/契约/关键旅程，最后接入 CI 与 SelfCheck，使后续改动自动选门禁、失败 fail-closed。

**Tech Stack:** Go 1.25, Gin, Gorm, SQLite/Postgres, Redis, Viper, Prometheus, OpenTelemetry, GitHub Actions, Agentic SelfCheck.

---

## 0. 当前基线（2026-06-07 实测）

- 仓库：`/root/work/v/ecommerce-backend`，`main...origin/main` clean。
- 测试：`./scripts/test-quick.sh` PASS；`./scripts/test-all.sh` PASS；`./scripts/check-guardrails.sh` PASS。
- 覆盖率：普通 profile total `55.8%`；`-coverpkg=./...` 全仓视角 total `53.2%`。
- 测试资产：18 个 `_test.go`，118 个 `Test*`；33 个非测试 package 中 9 个有测试。
- CI：`.github/workflows/ci.yml` 只跑 changed gofmt + `go test ./... -count=1`，没有 coverage floor / contract / SelfCheck gate。
- SelfCheck：`ecommerce-v2-prep-sandbox-lowlevel` 当前 PASS，但它覆盖的是 V2 Prep/Sandbox 局部，不等价于全业务后端质量闭环。
- 未跑：prod live smoke。`tools/prod/ecommerce-visual-workflow-smoke.sh --env prod` 会创建 synthetic prod records，必须获得郭凯批准后再执行。

## 1. 目标口径：什么叫“全业务质量闭环”

最终不能只报一个覆盖率百分比。必须同时满足这些层：

1. **代码覆盖门禁**
   - 全仓 `-coverpkg=./...` 不低于基线，且逐阶段提升。
   - 高风险模块设置分模块 floor 与 no-regression。
   - CI 阻止覆盖率下降。
2. **业务纵切验收**
   - 登录/注册/session/protected route。
   - access/me 与权限投影。
   - commercial order/payment/offerings。
   - wallet summary/history/quota。
   - billing/commission/promotion。
   - Product Center / image runtime / Visual Workflow / export/download 继续保持现有覆盖并防回退。
3. **契约闭环**
   - 路由 inventory 从 `engine.Routes()` 生成，保护 public/internal route、auth middleware、method/path。
   - OpenAPI/Swagger 生成与 drift 检查接入门禁。
   - 前端 consumer sweep 覆盖 `ecommerce-frontend/src/services/**` 的真实调用路径。
   - Platform client DTO/envelope/error semantics 有 httptest fake Platform 覆盖。
4. **运行时/真实 smoke**
   - local/dev 默认真实 API fixture + cleanup。
   - prod smoke 单独审批，证据区分 `NOT_RUN` / `PASS_WITH_NOTES` / `PASS`。
5. **观测/安全/失败语义**
   - request_id/trace_id/access log/metrics/tracing/header propagation 有单测或集成测试。
   - secret/token/storage key/provider payload 不进入用户响应和证据报告。
   - live smoke 与 evidence 不允许伪造 provider/runtime 成功。
6. **控制面固化**
   - repo-native scripts、Makefile、CI、SelfCheck feature contract、selector/evidence report 全部可运行。
   - 后续任一高风险路径改动都能自动选中对应门禁。

## 2. 分阶段目标

### Phase A — 质量控制面与基线门禁（先落，1 个 PR）

**Objective:** 先把“怎么量、怎么拦、怎么报告”固化，避免后面补测只停留在一次性报告。

**Target:** 不改业务行为；新增可执行质量基础设施。

**Files:**
- Create: `scripts/coverage-gate.sh`
- Create: `scripts/coverage-baseline.json`
- Create: `scripts/route-inventory-gate.sh`
- Create: `internal/router/router_test.go`
- Create: `internal/testutil/`（fake Platform、test DB、Gin recorder helpers）
- Modify: `Makefile`
- Modify: `.github/workflows/ci.yml`
- Modify: `docs/BACKEND_GUIDE.md`

**Acceptance criteria:**
- `make quality-gate` 一条命令跑完：guardrails + quick tests + coverage gate + route inventory gate。
- coverage baseline 记录当前全仓与模块 floor：
  - total `coverpkg_total >= 53.2` 起步；
  - `visualworkflow >= 71.0`，`templatecenter >= 71.0`，`promptcenter >= 68.0`，`imageruntime >= 59.0`，`productcore >= 51.0`，`billinggate >= 67.0`；
  - 低覆盖模块先设置 `no_regression` 或 `min_delta`，Phase B/C 再提高。
- CI 增加 `Run quality gate`，PR 上失败即 blocked。
- route inventory gate 至少保护：`/auth/session`、`/access/me`、wallet、commercial、billing、internal callbacks、V2 visual workflow、template center protected routes 的 method/path/auth 分组。

**Verifier command:**
```bash
cd /root/work/v/ecommerce-backend
make quality-gate
go test ./internal/router -run TestRouteInventory -count=1
```

**Evidence path:**
- `reports/quality/coverage/latest.json`
- `reports/quality/routes/latest.json`

**需要郭凯决策:** false。

---

### Phase B — 登录会话 + 权限闭环（1 个 PR）

**Objective:** 证明 product auth 只是 Platform truth 的投影，不复制登录/组织/RBAC 真相，同时保护 session/protected route 行为。

**Files:**
- Create: `internal/modules/auth/service_test.go`
- Create: `internal/modules/auth/handler_test.go`
- Create: `internal/modules/access/handler_test.go`
- Create: `internal/modules/authz/service_test.go`
- Create/extend: `internal/middleware/platform_jwt_test.go`
- Extend: `internal/testutil/platform_stub.go`

**Business rules covered:**
- Register/Login 调 Platform 成功后返回 token、user、credits、access projection。
- Platform auth error 映射为稳定 product API error，不泄露上游内部细节。
- Session 使用 Platform JWT claims + Platform profile，org mismatch / expired / malformed token fail closed。
- `/access/me` 必须受 Platform JWT 保护。
- promotion attribution 失败不得破坏注册主流程，但应可观测。

**Acceptance criteria:**
- Auth/access/authz/middleware package 都有测试文件。
- 覆盖 normal / invalid token / upstream unauthorized / missing org / inactive user / platform timeout。
- handler 测试校验统一 response envelope、request_id 存在、无 token/secret 泄露。
- `go test ./internal/modules/auth ./internal/modules/access ./internal/modules/authz ./internal/middleware -count=1` PASS。

**Verifier command:**
```bash
go test ./internal/modules/auth ./internal/modules/access ./internal/modules/authz ./internal/middleware -count=1 -cover
make quality-gate
```

**Evidence path:**
- `reports/quality/auth-access/latest.json`

**需要郭凯决策:** false。

---

### Phase C — Platform 契约客户端闭环（1 个 PR）

**Objective:** 用 fake Platform + DTO snapshot + error mapping tests 保护 Ecommerce 对 Platform 的 shared truth 调用。

**Files:**
- Create: `internal/platform/client_test.go`
- Create: `internal/platform/client_contract_test.go`
- Create: `internal/platform/testdata/*.golden.json`
- Extend: `internal/testutil/platform_server.go`
- Create: `scripts/platform-contract-gate.sh`

**Contracts covered:**
- auth register/login/session profile。
- access context。
- wallet summary/quota/history dependencies。
- offerings/catalog/order/payment。
- promotion/commission channel endpoints。
- runtime/charge/storage callback-relevant endpoints if used by Ecom paths。

**Acceptance criteria:**
- 每个 Platform client method 至少覆盖 success + one representative platform error + malformed envelope。
- `platformError` helpers 覆盖 400/401/404/409/413/5xx。
- DTO golden snapshots 保护 JSON casing 与 envelope shape。
- outbound requests 必须带 internal service auth header；测试禁止记录 secret 明文。

**Verifier command:**
```bash
go test ./internal/platform -count=1 -cover
./scripts/platform-contract-gate.sh
make quality-gate
```

**Evidence path:**
- `reports/quality/platform-contract/latest.json`

**需要郭凯决策:** false。

---

### Phase D — 商业化 + 钱包 + 计费/佣金/推广闭环（2 个 PR，先 service 后 handler/API）

**Objective:** 保护钱/额度/订单/返佣/推广相关的产品侧 projection 与幂等/错误语义。

**Files:**
- Create: `internal/modules/wallet/service_test.go`
- Create: `internal/modules/wallet/handler_test.go`
- Extend: `internal/modules/commercial/service_test.go`
- Create: `internal/modules/commercial/handler_test.go`
- Create: `internal/modules/billing/service_test.go`
- Create: `internal/modules/billing/handler_test.go`
- Create: `internal/modules/commission/service_test.go`
- Create: `internal/modules/commission/handler_test.go`
- Create: `internal/modules/promotion/service_test.go`
- Create: `internal/modules/promotion/handler_test.go`
- Extend: `internal/repository/commercial_repository_test.go`

**Business rules covered:**
- Offerings 只读 Platform commercial truth，按 `product_code=ecommerce` 过滤。
- Create order / confirm payment 的 idempotency、重复确认、失败回滚/错误映射。
- Wallet summary 同时返回 credits 与 quota，Platform 任一关键依赖失败时 fail closed。
- Wallet history 合并 rewards / commissions / wallet ledger / billing charge，并按时间排序/limit。
- Billing internal hooks：record charge、refund、outbox replay，重复回调不制造重复账目。
- Promotion code resolve/signup attribution/ensure code/list conversions。
- Commission redeem/channel projection，不跨 product/org 泄露。

**Acceptance criteria:**
- 上述 package 全部从 `[no test files]` 状态转为有测试。
- money/quota/cost paths 覆盖 success、permission/org mismatch、duplicate/idempotent、platform failure、empty state。
- handler 测试校验统一 envelope 与 HTTP status。
- repository 测试使用 sqlite transaction/cleanup，不依赖生产数据。

**Verifier command:**
```bash
go test ./internal/modules/wallet ./internal/modules/commercial ./internal/modules/billing ./internal/modules/commission ./internal/modules/promotion ./internal/repository -count=1 -cover
make quality-gate
```

**Evidence path:**
- `reports/quality/commercial-wallet/latest.json`

**需要郭凯决策:** false。

---

### Phase E — 路由中间件 + 存储 + 配置启动闭环（1 个 PR）

**Objective:** 保护服务启动、route auth grouping、DB/Redis readiness、storage access、CORS、internal auth 这些横切基础设施。

**Files:**
- Extend: `internal/router/router_test.go`
- Create: `internal/storage/storage_test.go`
- Create: `internal/config/config_test.go`
- Create: `internal/app/app_test.go`
- Extend: `internal/middleware/*_test.go`
- Create: `internal/migration/migration_test.go`（只做 schema and idempotent smoke，不跑生产迁移）

**Acceptance criteria:**
- `engine.Routes()` inventory 证明关键 protected/public/internal 路由分组正确。
- CORS 只回显允许 origin，不 wildcard 泄露。
- `/readyz` 能区分 DB/Redis failure，返回稳定 error code。
- `RequireInternalService` 常量时间或等价安全比较；missing/wrong secret fail closed。
- storage init/ping/driver validation 有 sqlite 与 invalid config 测试。
- app bootstrap 测试验证弱/default prod secret 被拒绝（如当前代码未支持，先补 fail-closed）。

**Verifier command:**
```bash
go test ./internal/router ./internal/middleware ./internal/storage ./internal/config ./internal/app ./internal/migration -count=1 -cover
make quality-gate
```

**Evidence path:**
- `reports/quality/infra-crosscut/latest.json`

**需要郭凯决策:** false，除非发现现有 prod config 依赖弱/default secret 行为；那时升级为 HITL。

---

### Phase F — 观测 + 安全红线闭环（1 个 PR）

**Objective:** 让 request_id/trace_id/metrics/logs/tracing/security redaction 有自动化证明。

**Files:**
- Create: `internal/observability/observability_test.go`
- Create: `internal/telemetry/tracing_test.go`
- Extend: `internal/middleware/access_log_test.go`
- Extend: `internal/middleware/metrics_test.go`
- Create: `scripts/security-redaction-gate.sh`
- Create: `scripts/observability-gate.sh`

**Acceptance criteria:**
- request_id/trace_id 进入 response envelope/header/log fields。
- access logs 不包含 Authorization/Bearer/JWT/password/secret/storage key/provider payload。
- metrics endpoint 注册成功，关键 route latency/counter 可采样。
- tracing middleware 在 disabled/enabled config 下都不 crash。
- evidence report stdout/stderr redaction 覆盖 token、secret、bare IP、ssh target、DB URL。

**Verifier command:**
```bash
go test ./internal/observability ./internal/telemetry ./internal/middleware -count=1 -cover
./scripts/security-redaction-gate.sh
./scripts/observability-gate.sh
make quality-gate
```

**Evidence path:**
- `reports/quality/observability-security/latest.json`

**需要郭凯决策:** false。

---

### Phase G — 真实 API / 关键旅程 / SelfCheck 固化（1-2 个 PR）

**Objective:** 把模块测试升级为业务可验收闭环，形成 release-wide gate。

**Files:**
- Create: `scripts/ecommerce-backend-critical-journey-smoke.py`
- Create: `scripts/ecommerce-backend-api-contract-smoke.py`
- Create: `/root/work/agentic-selfcheck/features/ecommerce-backend-business-quality-closure.yaml`
- Create: `/root/work/agentic-selfcheck/verifiers/ecommerce-backend-quality-*.yaml`
- Create: `/root/work/agentic-selfcheck/scripts/ecommerce_backend_quality_closure_gate.py`
- Extend: `/root/work/agentic-selfcheck/events/changed-v-ecommerce-requirement.yaml` or add dedicated event.

**Critical journeys:**
1. Register → Login → Session → Access/me。
2. Product create → detail → asset/source registration → prompt preview。
3. Visual workflow session → source reference → deconstruction contract boundary → generation version projection。
4. Wallet summary/history → commercial offerings → create order → confirm payment → billing charge projection。
5. Promotion resolve/signup attribution → commission overview/redeem projection。
6. Export package/download content path。
7. Internal runtime callback/result update path。

**Acceptance criteria:**
- local/dev smoke 默认 dry-run or isolated fixture；任何 write 都要 cleanup evidence。
- prod lane 只在显式 `ECOM_PROD_SMOKE_APPROVED=1` 下运行，且报告 synthetic record IDs 与 cleanup/retention policy。
- SelfCheck gate 对 `NOT_RUN` live evidence 只能给 `PARTIAL_PASS` 或明确 `PASS_WITH_NOTES`，不能伪装 full PASS。
- 后端质量 closure feature PASS 后才允许报告“全业务质量闭环”。

**Verifier command:**
```bash
cd /root/work/agentic-selfcheck
scripts/v-requirement-gate.sh ecommerce-backend-business-quality-closure static,api,evidence requirement.changed.v.ecommerce-backend-quality-closure
```

**Evidence path:**
- `/root/work/agentic-selfcheck/reports/ecommerce-backend-business-quality-closure/*.json`
- `/root/work/v/ecommerce-backend/reports/quality/business-journeys/latest.json`

**需要郭凯决策:** true for prod smoke / destructive cleanup policy only。

---

## 3. 目标覆盖率阶梯

不要一上来拍脑袋要求 80%，先用业务风险推进：

### Merge-ready 阶段（Phase A-C）
- 全仓 `-coverpkg=./... >= 58%`。
- 所有高风险 package 不再是 `[no test files]`：auth/access/authz/platform/router/middleware/storage/config 至少有基础测试。
- CI quality gate 必须阻断下降。

### Business-closed 阶段（Phase D-F）
- 全仓 `-coverpkg=./... >= 65%`。
- commercial/wallet/billing/commission/promotion/platform/middleware/router/storage 单模块关键行为覆盖达到 `50%+` 或有明确 no-regression + critical examples。
- money/quota/internal callback/security redaction 必须有负例。

### Release-closed 阶段（Phase G）
- 全仓 `-coverpkg=./... >= 70%`，核心 P0 业务模块不低于各自 baseline。
- Critical journeys local/dev PASS。
- prod smoke 如未批准，最终只能 `PASS_WITH_NOTES`，不能 full PASS。
- SelfCheck feature `ecommerce-backend-business-quality-closure` PASS/PASS_WITH_NOTES，报告路径存在且新鲜。

## 4. 工作拆分建议

按 PR 切分，避免一个巨大 PR 难 review：

1. PR-A: quality control plane + coverage/route gates。
2. PR-B: auth/session/access tests。
3. PR-C: platform contract client tests。
4. PR-D1: wallet/commercial service-level tests。
5. PR-D2: billing/commission/promotion handler/repository tests。
6. PR-E: router/middleware/storage/config/app tests。
7. PR-F: observability/security redaction tests。
8. PR-G: critical journey smoke + SelfCheck feature + CI release gate。

每个 PR 必须带：
- `make quality-gate` 输出。
- 变更模块覆盖率前后对比。
- 业务规则/负例列表。
- 未跑 live/prod 的明确 caveat。

## 5. 风险与边界

- 不在 Ecommerce 侧复制 Platform truth；只测 product projection、client contract、error mapping。
- 不用 mock 伪造 full business closure；mock/fake Platform 只用于 deterministic unit/contract tests。
- 真实 dev/local API smoke 优先；prod smoke 需要批准。
- 商业/钱包/计费测试必须关注幂等、重复回调、org/product scope、防泄露。
- 观测测试必须证明 redaction，而不是只检查日志存在。
- 如果补测试暴露生产代码缺少 fail-closed 防御，按用户偏好执行 A+B：先让测试门禁表达真实风险，再做生产防御性加固，不能只 mock 凑绿。

## 6. 第一批可立即执行的具体任务

### Task A1: Add coverage baseline gate

**Files:** `scripts/coverage-gate.sh`, `scripts/coverage-baseline.json`, `Makefile`

**Steps:**
1. Write `scripts/coverage-gate.sh` to run `go test ./... -covermode=atomic -coverpkg=./... -coverprofile=reports/quality/coverage/cover.out`.
2. Parse `go tool cover -func` and aggregate package coverage from cover profile.
3. Compare with `scripts/coverage-baseline.json`.
4. Emit `reports/quality/coverage/latest.json` with total/package status.
5. Add `make coverage-gate` and `make quality-gate`.
6. Run `make quality-gate`; expected PASS at current baseline.

### Task A2: Add route inventory gate

**Files:** `internal/router/router_test.go`, `scripts/route-inventory-gate.sh`

**Steps:**
1. Use `router.New(...)` with fake handlers/dependencies or existing constructors from app testutil.
2. Enumerate `engine.Routes()`.
3. Assert method/path for public/protected/internal P0 routes.
4. Assert sensitive routes are not accidentally public by checking route grouping expectations and middleware behavior via httptest for missing token.
5. Emit `reports/quality/routes/latest.json`.

### Task A3: Wire CI quality gate

**Files:** `.github/workflows/ci.yml`

**Steps:**
1. Keep existing gofmt and `go test ./... -count=1`.
2. Add `./scripts/check-guardrails.sh`.
3. Add `make quality-gate`.
4. Upload `reports/quality/**` as artifact when failure.

### Task B1: Auth service tests

**Files:** `internal/modules/auth/service_test.go`, `internal/testutil/platform_stub.go`

**Examples:**
- successful register projects Platform user/access/credits。
- platform register conflict returns error and no local activity writes beyond safe behavior。
- login success creates product activity。
- session fails closed when Platform profile lookup fails。

### Task C1: Platform client contract tests

**Files:** `internal/platform/client_test.go`, `internal/platform/testdata/*.golden.json`

**Examples:**
- successful envelope decode。
- non-2xx envelope becomes `platformError` with status/error_code/error_hint。
- malformed JSON becomes safe error。
- internal service secret sent in expected header and never printed in error snapshots。

---

## 7. Done / Stop 条件

可以说“全业务质量闭环”只在以下全部满足后：

- `make quality-gate` PASS。
- `scripts/platform-contract-gate.sh` PASS。
- `scripts/ecommerce-backend-critical-journey-smoke.py --env local-or-dev` PASS with cleanup evidence。
- CI 对 PR 必跑 quality gate。
- SelfCheck `ecommerce-backend-business-quality-closure` PASS/PASS_WITH_NOTES。
- prod smoke 若未批准，报告必须保持 `PASS_WITH_NOTES` 并说明 `prod_live_smoke=NOT_RUN`；若批准并跑通，才可升级为 full PASS。
- 所有新增 evidence 不含 secret/token/storage key/provider payload 明文。
