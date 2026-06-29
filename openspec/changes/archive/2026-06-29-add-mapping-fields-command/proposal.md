## Why

目前 `es-log` 沒有任何查詢欄位結構（mapping）的能力：使用者只能用 `ls` 看 target 名稱，要知道某個 alias/datastream 有哪些欄位、各欄位型別，只能繞過 CLI 直接 curl Elasticsearch 的 `_mapping` API。更重要的是，上一個 change（improve-es-error-diagnosability）揭露了 partial shard failure 的常見根因——同一欄位在不同 backing index 的型別分歧（例如某欄位在某 index 是 `text`、另一個是 `keyword`）。使用者目前無從在查詢前看見這種型別衝突，只能在查詢失敗後才被動發現。

## What Changes

- 新增唯讀子指令 `es-log fields <target>`，查詢並攤平 target 的 `_mapping`，以「欄位路徑 → 型別」呈現。
- 巢狀 `properties` 攤平成 dotted path（如 `user.id`）；multi-field 攤平成 `message.keyword`；無子欄位的 object 節點以 `object` 呈現。
- 當 target 解析到多個 backing index 且同一欄位出現 ≥2 種型別時，明確標示 `⚠ conflict` 並列出所有型別，讓使用者在查詢前一眼看見型別分歧。
- 「欄位僅存在於部分 index」不視為衝突、不標記（型別一致即查詢安全）。
- 預設 `json` 輸出（與 output-format 既有規範「非 search 一律 json」一致，維持 agent-parseable）；`-o table` 提供人類友善的 `FIELD / TYPE` 呈現並標示衝突；衝突欄位在 json/jsonl 額外帶 per-index 型別 breakdown。
- 底層在唯讀 client 新增 `GetMapping` 方法（`GET /<target>/_mapping`），並更新唯讀 endpoint 白名單。

## Non-Goals (optional)

<!-- design.md 會建立，Non-Goals 寫在 design.md 的 Goals/Non-Goals 一節 -->

## Capabilities

### New Capabilities

- `inspect-mapping`: `es-log fields <target>` 命令的行為——攤平 mapping、跨 index 合併、型別衝突標示，以及 table/json/jsonl 三種輸出。

### Modified Capabilities

- `readonly-es-client`: 唯讀 endpoint 白名單新增 `GetMapping`（`GET /<target>/_mapping`），並定義其攤平與跨 index 型別彙整行為。

## Impact

- Affected specs: `inspect-mapping`（新增）、`readonly-es-client`（修改）
- Affected code:
  - New: internal/cmd/fields.go, internal/cmd/fields_test.go
  - Modified: internal/esclient/client.go, internal/esclient/client_test.go, internal/cmd/root.go, README.md
  - Removed: (none)
