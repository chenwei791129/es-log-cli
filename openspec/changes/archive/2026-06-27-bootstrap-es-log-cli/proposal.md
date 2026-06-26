## Why

團隊的 logs 由 beats 經 index rollover alias 或 datastream 進入 Elasticsearch，但目前沒有一個「為 AI agent 特化、保證唯讀」的查詢入口：agent 直接打 ES REST API 既冗長又有誤觸破壞性端點（`DELETE`、`_rollover`、`_reindex`）的風險，且回傳格式不利於 `jq`/pipe 解析。需要一個單一靜態 binary 的 CLI `es-log`，從程式碼層面保證只能讀，並提供 agent 友善的 JSONL 輸出與明確的退出碼。

## What Changes

- 新增 greenfield Go CLI `es-log`（cobra 根指令 + 子指令），編譯為單一靜態 binary（`CGO_ENABLED=0`）。
- **context 設定（唯讀檢視）**：`config get-contexts` / `config view`（機密 redact）；context 由使用者手動維護 `~/.config/es-log/config.yaml`；對 ES 指令一律靠 `--context/-c`（或 `$ES_LOG_CONTEXT`）顯式指定，**無 current-context 隱藏狀態**。**不**提供 `set/use/delete/current-context`。
- **唯讀 ES client**：`internal/esclient` 只暴露白名單方法（`ListAliases`、`ListDataStreams`、`Search`、`GetMaxResultWindow`、`Ping`），固定 HTTP 動詞與路徑樣板，不提供任意 request 介面；支援 API Key 與 Basic Auth 認證與 TLS。
- **列舉 targets**：`ls` 列出 aliases 與 datastreams（合併視圖，標示 type）。
- **搜尋 logs**：`search -t <target>` 支援 Lucene `--query`、`--since`/`--from`/`--to` 時間範圍、`--size`（含 `--size 0` 以 `search_after` 分頁取全部、超過 `max_result_window` 自動截斷並警告）、`--fields`、`--sort`、client 端 `--include/--exclude` regex 細篩。
- **輸出格式與退出碼**：`-o jsonl|json|table`（`search` 預設 jsonl=純 `_source`；`json` 帶 metadata 與 total）、`--quiet`、分層 exit code（2/3/4）。
- **agent 包裝層**：`skill/SKILL.md` 說明何時用、query 範例、輸出格式；`README.md`（英文）含安裝與使用。

## Non-Goals (optional)

<!-- design.md 將建立，Non-Goals 記於 design.md 的 Goals/Non-Goals -->

## Capabilities

### New Capabilities

- `context-config`: 設定檔讀取、context 解析（`--context/-c` 與 `$ES_LOG_CONTEXT`、ES 指令必填、無 current-context）、`${ENV_VAR}` 展開、機密 redact，以及 `config get-contexts` / `config view` 指令。
- `readonly-es-client`: 唯讀端點白名單 ES client，保證只發出白名單內的讀取請求；API Key / Basic Auth 認證與 TLS（含 CA cert、insecure-skip-verify）。
- `list-targets`: `ls` 指令列舉 aliases 與 datastreams（合併與分項視圖、type 標示與計數欄位）。
- `log-search`: `search` 指令——Lucene query 組裝、時間範圍 filter、`--size` 與 `search_after` 分頁、`--fields`/`--sort`、client 端 regex include/exclude 細篩。
- `output-format`: 輸出格式契約（jsonl/json/table 的精確 schema）、`--quiet` 行為、stderr 純文字錯誤與分層 exit code。

### Modified Capabilities

(none)

## Impact

- Affected specs: 新增 `context-config`、`readonly-es-client`、`list-targets`、`log-search`、`output-format` 五個 capability。
- Affected code:
  - New:
    - go.mod
    - go.sum
    - cmd/es-log/main.go
    - internal/config/config.go
    - internal/config/resolve.go
    - internal/esclient/client.go
    - internal/esclient/auth.go
    - internal/output/format.go
    - internal/cmd/root.go
    - internal/cmd/config.go
    - internal/cmd/ls.go
    - internal/cmd/search.go
    - internal/cmd/ping.go
    - internal/cmd/version.go
    - skill/SKILL.md
    - README.md
    - Makefile
  - Modified: (none — greenfield)
  - Removed: (none)
