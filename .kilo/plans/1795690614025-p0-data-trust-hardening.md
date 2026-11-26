# P0 数据信任加固 — 实施计划

## 目标
关闭 `docs/DATABASE_COMPLETION_PLAN.md` Phase 0 中两个未勾选项，使"信任既有数据"达到可验证状态：
- 第 15 行：v6/v7 用户库升级前后兼容测试（保留内容、附件、providers、project/task）。
- 第 17 行：规范化 API JSON 解码/校验、SQLite 错误传播、WAL/外键检查、稳定 ID 与索引/查询计划。

这是 `docs/PROJECT_DIRECTION.md` 中 P0 的代码部分，是可信发布的前置条件。不在本计划内：导入导出、模板、10k 性能、UI 功能、macOS 打包（属 P1/P2）。

## 现状（已核对代码，非文档声明）
- 已完成：`initializeDatabase`（main.go:409）已设置并校验 `journal_mode=WAL`、`synchronous=FULL`、`foreign_keys=ON`，并在迁移后调用 `verifyDatabaseForeignKeys`（main.go:666）只读检测孤儿引用；迁移为事务化、forward-only；`rejectUnsupportedSchemaMigrations`（migration_version_guard.go）拒绝更高版本库；`verifyAppliedMigrationSchema` 校验已应用迁移的必需结构。
- 部分完成：多数 db 变更端点已用规范化解码器 `decodeJSONRequest`（main.go:1880，含 4MiB 限制 + 重复键拒绝 + 尾随值拒绝 + 返回字段存在性 map）。
- 缺口 A（JSON 规范化）：3 个处理器绕过 `decodeJSONRequest`，直接用 `json.NewDecoder(r.Body).Decode(...)`，未拒绝重复键/尾随值：
  - `handleSettings` POST（main.go:1336，payload 为 object）。
  - `handleProviderSettings` POST（main.go:1375，payload 为 **数组** `[]providerInput`）。
  - `handleAIPing` POST（main.go:1429，payload 为 object）。
  - 注：4MiB 体积上限已由 `localSecurityMiddleware`（http_security.go:62）统一施加，但重复键/尾随值语义未覆盖这 3 处。
- 缺口 B（真实历史备份）：`db_legacy_v6_test.go` / `db_legacy_v7_test.go` 仅构造 **合成** fixture（从当前库 DROP 表/列），文档明确要求"真实历史生产 v6/v7 备份"验收，仓库内无此类 `.db` 资产。

## 决策
1. 先做 P0（数据信任），再做 P1。
2. 本计划落地 **缺口 A 全部 + 缺口 B 的合成覆盖强化**；缺口 B 的"真实历史备份"部分 **显式标记为受阻/超出范围**，依赖 owner 提供真实 v6/v7 `.db` 备份文件（禁止用生产工作区做 fixture）。
3. 不改变任何既有回归断言；不弱化测试；不触碰 AI Agent/Provider 配置语义（仅统一其 HTTP 解码路径，保持外部行为字节级不变）。

## 任务（有序）

### T1 — 新增统一解码助手（纯 object/array）
- 在 `json_duplicate_keys.go` 旁新增 `decodeJSONBody(r *http.Request, target any) error`：
  - `io.ReadAll(io.LimitReader(r.Body, (4<<20)+1))`；`len > 4<<20` 返回 "request body is too large"。
  - 调用 `rejectDuplicateJSONKeys(body)`（已存在，覆盖嵌套与尾随值）。
  - `json.Unmarshal(body, target)`；对 object 端点额外要求顶层为 object（沿用 `decodeJSONRequest` 的 `fields == nil` 判定思路，但 target 可能是 slice/map，故只校验"非 null"）。
- `decodeJSONRequest` 保持签名与行为不变（PUT 依赖其返回的 fields map）；内部可复用 `decodeJSONBody` 以避免重复逻辑，但不得改变其返回字段存在性语义。

### T2 — 改造 3 个绕过解码的处理器
- `handleSettings`（main.go:1334-1339）：改用 `decodeJSONBody(r, &payload)`（`payload map[string]any`）。
- `handleProviderSettings`（main.go:1373-1378）：改用 `decodeJSONBody(r, &inputs)`（`inputs []providerInput`）。
- `handleAIPing`（main.go:1425-1432）：改用 `decodeJSONBody(r, &req)`。
- 三者错误分支保持现有 400 文案与返回结构，不改变成功路径与 provider 文件字节输出。

### T3 — 回归测试（Go，真实 SQLite + HTTP）
- 新增 `json_decode_normalization_test.go`，对 `/api/settings`、`/api/ai/providers`、`/api/ai/test-connection` 覆盖：
  - 重复键（顶层与嵌套）→ 400，且状态/provider 文件/DB 不变。
  - 尾随 JSON 值（`{}{}` / `[][]`）→ 400。
  - 顶层类型错误（object 端点传数组、array 端点传 object）→ 400。
  - 合法请求仍成功（设置持久化、provider 列表往返、test-connection 的 400/正常分支）。
  - 每次断言 `config/providers.json` 与 `config/app.json` 字节不变（失败用例）或按预期变更（成功用例），并校验 DB 行/版本不变。
- 复用现有 `setupTestContext`、`dbRegressionCreate` 测试夹具；不得修改既有断言。

### T4 — 合成 v6/v7 覆盖强化（缺口 B 可做部分）
- 扩展 `db_legacy_v6_test.go` / `db_legacy_v7_test.go`（或新增 `db_legacy_rich_*_test.go`），在降级前写入更丰富内容：多页 `pages`（含 `parent_id` 嵌套）、`db_values`/`db_views`/`db_properties`、`db_files` 记录、多个 project/task、`app_state` 标记；升级后逐字段断言内容、`version`、`created_at` 不变，迁移计数到 10，二次重启幂等，provider 文件与外部附件字节不变。
- 明确注释：这是合成 fixture，真实历史生产备份仍需 owner 资产（见开放问题）。

### T5 — 索引/查询计划验证（缺口 A 的"index/query plans"）
- 新增 `db_query_plan_test.go`：对 `db_records`（按 `database_id, deleted_at, created_at`）、`db_values(property_id)`、`db_properties(database_id, deleted_at, position)`、`db_views(...)`、`db_files(record_id, deleted_at)` 运行 `EXPLAIN QUERY PLAN`，断言命中对应 `idx_*` 索引（`SEARCH ... USING INDEX`），防止回归丢失索引。
- 不引入 10k 基准（属 P1 规模）。

### T6 — 文档与清单更新
- 在 `docs/DATABASE_COMPLETION_PLAN.md` Evidence log 追加条目（含 commit/CI 占位，待实现后回填真实 SHA 与 Windows CI 链接）。
- 仅勾选能由代码+回归证据+SHA 匹配 Windows CI 证明的项目；缺口 B 真实备份部分保持未勾并注明受阻。
- 同步 `docs/VERIFICATION.md` 的"剩余缺口"措辞（区分已验证/部分/不可用）。

## 验证
- 后端：`go test ./...`（重点 `go test ./src/backend -run 'JSON|Legacy|QueryPlan|Migration'`）。
- 前端无需改动；如构建受影响：`cd src/frontend && npm run build && npm run lint`。
- 真实 Windows CI（start/stop、打包、Playwright）为外部验收，需在实现分支以精确 SHA 记录；本沙箱 shell 受限，无法本地运行，实现者须在 PR 记录精确 head SHA 与 CI 链接。
- 不得以删除/弱化既有断言换取绿色。

## 风险与失败模式
- 改动 provider/settings 解码路径若改变成功路径输出 → 破坏现有 provider 字节不变断言；必须保持成功路径与文件写入不变。
- `decodeJSONBody` 对 array endpoint 误用 object 校验 → 拒绝合法 provider 列表；需按 target 类型区分。
- 合成 fixture 强化若引入对当前 schema 的隐式依赖 → 降级脚本需与历史结构一致，避免"伪历史"。
- 真实历史备份缺失 → 第 15 行无法完全勾选；不得用生产库替代。

## 开放问题（需 owner 决策，不阻塞 T1–T5）
- 能否提供真实 v6、v7 历史 `.db` 备份文件（脱敏、可入库为测试资产）？若不能，第 15 行"真实历史备份"验收将保持未勾，仅交付合成覆盖强化 + 缺口 A 全量。