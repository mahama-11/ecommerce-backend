# Template Center 数据结构模型

## 1. 文档目标

本文档是 [Agent Ecommerce 模板中心设计稿](file:///Users/bytedance/Documents/project/go/v/docs/architecture/AGENT_ECOMMERCE_TEMPLATE_CENTER_DESIGN.md) 的数据结构落地稿。

它重点回答这些问题：

- 模板中心的核心实体有哪些
- 官方模板、模板版本、模板 schema、模板示例、收藏、我的模板、使用事件之间是什么关系
- 哪些字段第一版必须做，哪些字段可以后置
- 前端列表页、详情页、执行入口、收藏、复制到我的模板分别依赖哪些数据结构
- 后端表结构如何既支持第一版快速上线，又为后续视频模板、工作流模板、团队模板预留扩展空间

本文档默认所有表由 `v-ecommerce-backend` 拥有，并采用产品域前缀，例如 `ecommerce_template_*`。

## 2. 设计原则

## 2.1 模板是“资产对象”，不是“Prompt 文本”

模板数据模型必须能承载：

- 元数据
- 执行目标
- 输入 / 输出 schema
- Prompt 分层
- 示例资产
- 推荐状态
- 生命周期
- 版本
- 用户关系

因此不能只设计一个：

```sql
content text
```

## 2.2 官方目录与用户实例分离

必须清晰区分：

- `官方模板目录（catalog）`
- `用户模板实例（instance）`

原因：

- 官方模板会持续迭代版本
- 用户复制后的模板可能被二次编辑
- 官方模板更新时，不应覆盖用户个人实例

## 2.3 稳定身份与版本内容分离

模板必须同时支持：

- 一个稳定的模板身份 `template_id`
- 多个版本化内容 `version_id`

因此不能把所有字段都堆在一张 catalog 表上。

## 2.4 模态统一、执行分流

模板中心统一管理：

- 文本模板
- 图片模板
- 视频模板
- 工作流模板

但执行逻辑不能统一。
因此模型必须显式包含：

- `modality`
- `executor_type`

## 2.5 Schema 驱动前端

前端模板中心不应写死大量模板专属表单。

因此后端必须提供：

- 输入 schema
- 输出 schema
- 执行 schema
- Prompt 分层 schema

由前端进行动态渲染与路由分发。

## 3. 核心实体总览

第一版建议核心实体如下：

- `template_catalog`
- `template_catalog_locale`
- `template_catalog_version`
- `template_catalog_schema`
- `template_catalog_example`
- `template_favorite`
- `template_instance`
- `template_instance_locale`
- `template_usage_event`

第二阶段可扩展实体：

- `template_publish_log`
- `template_recommendation_slot`
- `template_import_batch`
- `template_team_share`
- `template_instance_revision`

## 4. 实体关系

可按下面理解：

- 一个 `template_catalog` 表示一个官方模板的稳定身份
- 一个 `template_catalog` 可以有多条 `template_catalog_locale`
- 一个 `template_catalog` 可以有多条 `template_catalog_version`
- 一条 `template_catalog_version` 对应一条 `template_catalog_schema`
- 一条 `template_catalog_version` 可以有多条 `template_catalog_example`
- 用户可以对 `template_catalog` 产生收藏关系 `template_favorite`
- 用户可以从 `template_catalog_version` 复制出自己的 `template_instance`
- 用户或系统对模板行为会产生 `template_usage_event`

简化关系图：

```text
template_catalog
  ├─ template_catalog_locale (1:n)
  ├─ template_catalog_version (1:n)
  │    ├─ template_catalog_schema (1:1)
  │    └─ template_catalog_example (1:n)
  ├─ template_favorite (1:n by user/org)
  └─ template_instance (1:n by user/org, copied from version)

template_usage_event
  ├─ may reference template_catalog
  ├─ may reference template_catalog_version
  └─ may reference template_instance
```

## 5. 核心枚举

## 5.1 `modality`

- `text`
- `image`
- `video`
- `workflow`

## 5.2 `executor_type`

- `chat`
- `image_tool`
- `video_tool`
- `batch_pipeline`
- `hybrid_workflow`

## 5.3 `scope`

- `official`
- `personal`
- `team`
- `org`

## 5.4 `lifecycle_status`

- `draft`
- `internal`
- `published`
- `archived`
- `deprecated`

## 5.5 `capability_type`

建议不做数据库枚举，而做字符串规范字段，允许未来扩展，例如：

- `listing_write`
- `review_analysis`
- `consumer_insight`
- `creator_outreach`
- `model_swap`
- `mannequin_to_model`
- `background_replace`
- `virtual_try_on`
- `accessory_on_model`
- `product_scene_compositing`
- `product_swap`
- `scene_fission`
- `scene_asset_generation`
- `handheld_product`
- `product_retouching`

## 5.6 `source_type`

用户模板来源建议：

- `preset_catalog`
- `chat_result`
- `design_workflow`
- `manual_create`
- `imported`

## 5.7 `usage_event_type`

- `impression`
- `detail_view`
- `favorite`
- `unfavorite`
- `copy`
- `use`
- `execute_success`
- `execute_failed`
- `publish`
- `archive`

## 6. 表结构设计

## 6.1 `template_catalog`

用途：

- 官方模板的稳定身份
- 列表页查询主表
- 推荐、排序、状态过滤主表

建议字段：

```text
id                      varchar(64) pk
slug                    varchar(128) unique not null
external_code           varchar(64) null
scope                   varchar(16) not null default 'official'
modality                varchar(16) not null
executor_type           varchar(32) not null
series                  varchar(64) not null
capability_type         varchar(64) not null
interaction_mode        varchar(32) not null
status                  varchar(16) not null
current_version_id      varchar(64) null
default_locale          varchar(16) not null default 'zh'
cover_asset_url         text null
icon_asset_url          text null
platform_tags_json      json/text not null
industry_tags_json      json/text not null
scenario_tags_json      json/text not null
compliance_tags_json    json/text not null
is_featured             boolean not null default false
recommend_score         int not null default 0
sort_order              int not null default 0
cost_estimate_min       bigint not null default 0
cost_estimate_max       bigint not null default 0
success_rate_hint       decimal(5,2) null
owner_team              varchar(64) null
created_by              varchar(64) null
updated_by              varchar(64) null
created_at              timestamp not null
updated_at              timestamp not null
published_at            timestamp null
archived_at             timestamp null
```

说明：

- `external_code` 用于保留 `M1-T01`、`P2-T03` 这种资产编码
- `current_version_id` 指向当前默认发布版本
- 标签采用 JSON 数组，第一版实现更快
- `cost_estimate_min/max` 用于后续积分或成本展示

第一版必须字段：

- `id`
- `slug`
- `scope`
- `modality`
- `executor_type`
- `series`
- `capability_type`
- `interaction_mode`
- `status`
- `current_version_id`
- `cover_asset_url`
- `platform_tags_json`
- `industry_tags_json`
- `is_featured`
- `recommend_score`
- `sort_order`
- `created_at`
- `updated_at`

## 6.2 `template_catalog_locale`

用途：

- 多语言展示文案
- 列表页 / 详情页的标题、摘要、场景说明

建议字段：

```text
id                      varchar(64) pk
template_catalog_id     varchar(64) not null
locale                  varchar(16) not null
name                    varchar(255) not null
short_name              varchar(128) null
summary                 text not null
description             text not null
scenario_description    text null
input_description       text null
output_description      text null
seo_title               varchar(255) null
seo_description         text null
created_at              timestamp not null
updated_at              timestamp not null
```

唯一约束：

- `(template_catalog_id, locale)` 唯一

## 6.3 `template_catalog_version`

用途：

- 存模板版本化内容快照
- 支持发布、回滚、历史版本对比

建议字段：

```text
id                      varchar(64) pk
template_catalog_id     varchar(64) not null
version_no              int not null
version_label           varchar(32) not null
status                  varchar(16) not null
change_note             text null
is_publishable          boolean not null default true
is_default              boolean not null default false
source_asset_ref        varchar(255) null
created_by              varchar(64) null
published_by            varchar(64) null
created_at              timestamp not null
published_at            timestamp null
archived_at             timestamp null
```

唯一约束：

- `(template_catalog_id, version_no)` 唯一

说明：

- `status` 推荐与 catalog lifecycle 同步，但 version 自身也允许存在 `draft/internal/published/archived`
- `is_default` 表示该版本是否为当前默认发布版本

## 6.4 `template_catalog_schema`

用途：

- 真正承接模板“可执行结构”
- 支撑前端动态表单和执行路由

建议字段：

```text
id                      varchar(64) pk
template_version_id     varchar(64) not null unique
input_schema_json       json/text not null
output_schema_json      json/text not null
execution_schema_json   json/text not null
prompt_layers_json      json/text not null
policy_schema_json      json/text null
default_variables_json  json/text not null
tool_binding_json       json/text not null
created_at              timestamp not null
updated_at              timestamp not null
```

其中：

- `input_schema_json`：前端表单字段、文件槽位、校验规则
- `output_schema_json`：输出张数、比例、分辨率、格式等
- `execution_schema_json`：执行目标路由、预填参数、步骤、异步模式
- `prompt_layers_json`：L1/L2/L3 分层 Prompt
- `policy_schema_json`：合规或安全规则
- `tool_binding_json`：工具 slug、工作流节点、默认执行器绑定

### 6.4.1 `input_schema_json` 示例

```json
{
  "mode": "upload_form",
  "fields": [
    {
      "key": "product_image",
      "label": { "zh": "商品主图", "en": "Product Image" },
      "type": "image",
      "required": true,
      "accept": ["image/png", "image/jpeg"],
      "maxCount": 1
    },
    {
      "key": "target_market",
      "label": { "zh": "目标市场", "en": "Target Market" },
      "type": "select",
      "required": true,
      "options": ["amazon-us", "amazon-uk", "tiktok-shop-us"]
    }
  ]
}
```

### 6.4.2 `output_schema_json` 示例

```json
{
  "primaryOutput": "image",
  "image": {
    "count": 4,
    "ratio": "1:1",
    "resolution": "2048x2048",
    "transparentBackground": false
  }
}
```

### 6.4.3 `execution_schema_json` 示例

```json
{
  "executorType": "image_tool",
  "route": "/draw/changing-model",
  "toolSlug": "changing-model",
  "prefill": {
    "templateId": "tpl_model_swap_001",
    "defaultScene": "studio-clean"
  },
  "supportsAsyncJob": true,
  "supportsBatch": false
}
```

### 6.4.4 `prompt_layers_json` 示例

```json
{
  "l1": {
    "name": "tool_system_prompt",
    "content": "..."
  },
  "l2": {
    "name": "template_case_prompt",
    "content": "..."
  },
  "l3": {
    "name": "user_diy_prompt",
    "defaultContent": "",
    "editable": true
  }
}
```

## 6.5 `template_catalog_example`

用途：

- 展示示例输入 / 输出 / 预览素材
- 供模板详情页、推荐卡片使用

建议字段：

```text
id                      varchar(64) pk
template_version_id     varchar(64) not null
example_type            varchar(32) not null
title                   varchar(255) null
description             text null
input_asset_url         text null
output_asset_url        text null
preview_asset_url       text null
video_poster_url        text null
sort_order              int not null default 0
created_at              timestamp not null
updated_at              timestamp not null
```

`example_type` 建议：

- `before_after`
- `output_only`
- `input_only`
- `video_demo`
- `workflow_preview`

## 6.6 `template_favorite`

用途：

- 用户收藏官方模板

建议字段：

```text
id                      varchar(64) pk
template_catalog_id     varchar(64) not null
user_id                 varchar(64) not null
organization_id         varchar(64) not null
created_at              timestamp not null
```

唯一约束：

- `(template_catalog_id, user_id, organization_id)` 唯一

## 6.7 `template_instance`

用途：

- 用户的模板实例
- 承接“复制到我的模板”
- 承接从 chat/design 等流程沉淀下来的模板

建议字段：

```text
id                      varchar(64) pk
user_id                 varchar(64) not null
organization_id         varchar(64) not null
preset_template_id      varchar(64) null
preset_version_id       varchar(64) null
source_type             varchar(32) not null
source_label            varchar(255) null
modality                varchar(16) not null
executor_type           varchar(32) not null
series                  varchar(64) not null
capability_type         varchar(64) not null
status                  varchar(16) not null default 'published'
is_archived             boolean not null default false
is_favorite             boolean not null default false
editable_schema_json    json/text not null
prompt_layers_json      json/text not null
platform_tags_json      json/text not null
industry_tags_json      json/text not null
saved_at                timestamp not null
updated_at              timestamp not null
archived_at             timestamp null
```

说明：

- `preset_template_id` / `preset_version_id` 用于追溯来源
- `editable_schema_json` 与官方 schema 解耦，允许用户编辑后形成自己的配置
- `prompt_layers_json` 可从官方版本拷贝后再编辑

## 6.8 `template_instance_locale`

用途：

- 用户模板的多语言标题、摘要

建议字段：

```text
id                      varchar(64) pk
template_instance_id    varchar(64) not null
locale                  varchar(16) not null
name                    varchar(255) not null
summary                 text not null
description             text null
created_at              timestamp not null
updated_at              timestamp not null
```

唯一约束：

- `(template_instance_id, locale)` 唯一

## 6.9 `template_usage_event`

用途：

- 模板行为统计与分析
- 支撑热门、推荐、成功率、使用量等指标

建议字段：

```text
id                      varchar(64) pk
event_type              varchar(32) not null
template_catalog_id     varchar(64) null
template_version_id     varchar(64) null
template_instance_id    varchar(64) null
executor_type           varchar(32) null
modality                varchar(16) null
user_id                 varchar(64) null
organization_id         varchar(64) null
request_id              varchar(64) null
trace_id                varchar(64) null
route_path              varchar(255) null
status                  varchar(16) null
cost_estimate           bigint not null default 0
latency_ms              int not null default 0
payload_json            json/text null
created_at              timestamp not null
```

说明：

- 允许 catalog / version / instance 任一维度落事件
- `payload_json` 可承载额外埋点上下文

## 7. 索引建议

## 7.1 `template_catalog`

建议索引：

- `idx_template_catalog_status_featured_sort(status, is_featured, sort_order desc)`
- `idx_template_catalog_modality_series(modality, series)`
- `idx_template_catalog_executor_type(executor_type)`
- `idx_template_catalog_capability_type(capability_type)`
- `idx_template_catalog_recommend_score(recommend_score desc)`
- `uk_template_catalog_slug(slug)`
- `uk_template_catalog_external_code(external_code)` 可选

## 7.2 `template_catalog_version`

- `uk_template_catalog_version(template_catalog_id, version_no)`
- `idx_template_catalog_version_status(status, published_at desc)`

## 7.3 `template_favorite`

- `uk_template_favorite(template_catalog_id, user_id, organization_id)`
- `idx_template_favorite_user_org(user_id, organization_id, created_at desc)`

## 7.4 `template_instance`

- `idx_template_instance_user_org(user_id, organization_id, saved_at desc)`
- `idx_template_instance_preset(preset_template_id, preset_version_id)`
- `idx_template_instance_status(is_archived, status)`

## 7.5 `template_usage_event`

- `idx_template_usage_catalog(template_catalog_id, created_at desc)`
- `idx_template_usage_instance(template_instance_id, created_at desc)`
- `idx_template_usage_event_type(event_type, created_at desc)`
- `idx_template_usage_user_org(user_id, organization_id, created_at desc)`

## 8. 前端 DTO 建议

## 8.1 模板列表项 `TemplateListItem`

```ts
type TemplateListItem = {
  id: string
  slug: string
  name: string
  summary: string
  modality: 'text' | 'image' | 'video' | 'workflow'
  executorType: 'chat' | 'image_tool' | 'video_tool' | 'batch_pipeline' | 'hybrid_workflow'
  series: string
  capabilityType: string
  coverAssetUrl?: string
  platformTags: string[]
  industryTags: string[]
  isFeatured: boolean
  recommendScore: number
  isFavorited: boolean
  useCount: number
  successRateHint?: number
}
```

## 8.2 模板详情 `TemplateDetail`

```ts
type TemplateDetail = {
  catalog: TemplateListItem
  locale: {
    description: string
    scenarioDescription?: string
    inputDescription?: string
    outputDescription?: string
  }
  version: {
    id: string
    versionNo: number
    versionLabel: string
    status: string
  }
  schema: {
    inputSchema: Record<string, unknown>
    outputSchema: Record<string, unknown>
    executionSchema: Record<string, unknown>
    promptLayers: Record<string, unknown>
    policySchema?: Record<string, unknown>
  }
  examples: Array<{
    id: string
    exampleType: string
    title?: string
    description?: string
    inputAssetUrl?: string
    outputAssetUrl?: string
    previewAssetUrl?: string
  }>
}
```

## 8.3 使用入口响应 `TemplateUseResponse`

```ts
type TemplateUseResponse = {
  targetRoute: string
  executorType: string
  toolSlug?: string
  prefilledInputSchema: Record<string, unknown>
  preloadedTemplatePayload: Record<string, unknown>
  supportsAsyncJob: boolean
  supportsBatch: boolean
}
```

## 8.4 我的模板列表项 `MyTemplateItem`

```ts
type MyTemplateItem = {
  id: string
  presetTemplateId?: string
  sourceType: string
  sourceLabel?: string
  name: string
  summary: string
  modality: string
  executorType: string
  series: string
  capabilityType: string
  isArchived: boolean
  savedAt: string
  updatedAt: string
}
```

## 9. 第一版必须做与可后置项

## 9.1 第一版必须做

表级：

- `template_catalog`
- `template_catalog_locale`
- `template_catalog_version`
- `template_catalog_schema`
- `template_catalog_example`
- `template_favorite`
- `template_instance`
- `template_usage_event`

能力级：

- 目录列表
- 目录详情
- 收藏 / 取消收藏
- 复制到我的模板
- 模板执行路由返回
- 第一批 seed 导入

## 9.2 第一版可后置

- `template_instance_locale`
- `template_publish_log`
- `template_recommendation_slot`
- `template_team_share`
- `template_instance_revision`
- 复杂的组织共享权限模型
- 完整视频模板执行状态管理

说明：

- `template_instance_locale` 如果第一版只做中文，可暂时合并进 `template_instance`
- 但字段命名上应预留未来拆表可能

## 10. 迁移策略

## 10.1 与现有 saved-template 的关系

当前 `workspace.saved_templates` 更像“用户模板实例”的早期形态。

迁移建议：

第一阶段：

- 保留旧接口
- 新增 `template_center` 模块
- 新复制逻辑写入 `template_instance`

第二阶段：

- 旧的 `saved_templates` 从“模板中心主表”降级为兼容桥
- 前端改为主读 `template_center/my-templates`

第三阶段：

- 如无外部依赖，可逐步下线旧 contract

## 10.2 Prompt 文档导入方式

建议不要直接把 Markdown 文档内容存数据库。

推荐流程：

1. 从 Prompt 文档提取结构化 seed 数据
2. 生成 `template_catalog` + `version` + `schema` + `example` 记录
3. 导入后以数据库为主事实源
4. 文档继续作为知识源和人工维护源

## 11. 风险与约束

## 11.1 过度简化风险

如果只做一张 catalog 表并把所有内容塞进 `content` 字段，后续会遇到：

- 图片模板和文本模板无法共享表单渲染逻辑
- 视频模板很难落地
- 版本无法回滚
- 收藏和复制难以追踪
- 示例资产与执行目标难以管理

## 11.2 过度设计风险

如果第一版就做太多复杂拆分，也会导致：

- 导入成本高
- 前端无法快速消费
- 后端 seed 太重

因此本文档已经按“第一版最小可用 + 后续扩展”进行折中。

## 12. 建议的第一批落地顺序

1. 先落 `template_catalog`、`template_catalog_version`、`template_catalog_schema`
2. 再落 `template_favorite` 和 `template_instance`
3. 接着做目录详情、收藏、复制到我的模板
4. 最后接 `template_usage_event`

这样可以先把官方模板目录与用户实例的边界打清楚，再补运营与统计。

