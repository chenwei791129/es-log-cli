## Context

Greenfield 專案，無既有程式碼。目標是打造一個**唯讀、為 AI agent 特化**的 Elasticsearch log 查詢 CLI `es-log`，編譯為單一靜態 binary（`CGO_ENABLED=0`，跨 darwin/linux × amd64/arm64）。logs 由 beats 經 index rollover alias 或 datastream 進入 ES（只支援 **8.x+**）。主要使用者是 agent，其次是人類與 CI。核心約束：從程式碼層面保證**不可能**執行破壞性動作。

## Goals / Non-Goals

**Goals:**

- 唯讀保證：client 只能發出固定白名單的讀取請求，從介面層杜絕誤觸破壞性 API。
- agent 友善：`search` 預設 JSONL（純 `_source`）、`--quiet` 純淨 stdout、分層 exit code，方便 `jq`/pipe 與程式分支。
- 顯式 context：每次查詢明確綁定一個 cluster，無隱藏預設狀態。
- 機密安全：`${ENV_VAR}` 展開、`config view` redact、不在 log 印 Authorization header。

**Non-Goals:**

- 不做任何維護操作：`DELETE index`、`_rollover`、`_forcemerge`、`_reindex`、ILM、ML、APM、Security 管理、Ingest pipeline。
- 不在 `esclient` 暴露通用「任意 request」介面。
- 不提供 context 寫入指令（`set/use/delete/current-context`）；context 由使用者手動維護設定檔。
- 不相容 ES 7.x。
- `--follow`（tail 即時尾隨）列為未來功能，不在首版。
- 不自動下載/安置 binary（安裝由使用者 `make build` 或取得 release）。

## Decisions

### 採 cobra 建構 CLI 指令樹

CLI 框架用 `spf13/cobra`（kubectl/gh 同款）。根指令 `es-log` 掛載子指令 `config`、`ls`、`search`、`ping`、`version`。全域 persistent flags：`--context/-c`、`--output/-o`、`--quiet`、`--config`。理由：社群慣例、子命令與 flag 解析成熟、help 自動生成。替代方案 `urfave/cli` 與手刻 `flag` 套件，前者生態較小、後者要自行處理子命令樹，皆不採。

### esclient 唯讀端點白名單（唯讀保證的設計核心）

`internal/esclient` 只暴露固定方法，每個方法綁死 HTTP 動詞與路徑樣板，**不提供**接受任意 method/path 的通用 request 介面：

| 方法 | HTTP | 路徑 |
|------|------|------|
| `ListAliases()` | GET | `/_alias` |
| `ListDataStreams()` | GET | `/_data_stream` |
| `Search(target, body)` | POST | `/<target>/_search`（含 `--size 0` 的 `search_after` 分頁，同一端點） |
| `GetMaxResultWindow(target)` | GET | `/<target>/_settings/index.max_result_window?include_defaults=true&flat_settings=true` |
| `Ping()` | GET | `/_cluster/health` |

`search_after` 僅是 `_search` request body 欄位、不新增端點，也就不需要 PIT（`POST /_pit`）或其 `DELETE /_pit`，唯讀保證仍成立。此白名單以單元測試鎖定：列舉 client 公開方法、斷言不存在發出非白名單動詞/路徑的程式碼路徑。替代方案「薄封裝 + 通用 `Do(req)`」被否決，因為它讓破壞性呼叫變得可能。

### 用官方 go-elasticsearch 還是自封裝 net/http

預設採官方 `elastic/go-elasticsearch`（正確處理 auth/TLS/retry），但只透過上述白名單包裝對外暴露。若實作時發現依賴量不可接受，可改 `net/http` 薄封裝——**對外介面與測試不變**，由實作者於 PR 說明取捨。理由：先求正確再求精簡，介面隔離讓底層可替換。

### context 解析：顯式、無隱藏狀態

context 來源優先序：`--context/-c` flag > `$ES_LOG_CONTEXT` 環境變數。對 ES 指令（`ls`/`search`/`ping`）兩者皆缺即報錯（exit 2）並列出可用 context 名稱。設定檔**無 `current-context` 欄位**。理由：agent 行為必須完全可預測，不能依賴看不見的預設；移除所有寫入型 context 指令同時消除機密誤寫風險。替代方案 kubectl 式 `current-context` + `use-context` 被否決（引入隱藏狀態）。

### 設定檔格式與機密展開

`~/.config/es-log/config.yaml`（XDG 路徑，`$XDG_CONFIG_HOME` fallback `~/.config`，可由 `--config`/`$ES_LOG_CONFIG` 覆寫；建議 `0600`）。context 內嵌 `server` + `auth` + `tls` 扁平式（非 kubectl 三段式）。機密值支援 `${ENV_VAR}` 字串展開：讀取設定時對字串做 env 展開，未設定的變數視為錯誤。`config view` 對 `api-key`/`password` 一律 redact 為 `***`。

### search 輸出 schema 契約

`jsonl`（search 預設）：每行 = 純 `_source`（log 原貌，無 ES 包裝），`--fields` 對 `_source` 做欄位篩選。`json`：單一文件 `{"total":N,"hits":[{"_id":...,"_index":...,"_score":...,"_source":{...}},...]}`。`table`：人類可讀欄位對齊。理由：jsonl 逐筆對 `grep`/`head`/`jq -c` 與串流最友善；json 給需要 metadata/total 的整包消費者。

### --size 語意與 search_after 分頁

`--size/-n N`（短旗標 `-n`、別名 `--limit`、預設 50）：

- **W 的取得（lazy）**：`W` = target 的 `max_result_window`（ES 預設 10000）。為避免每次查詢都多一個 round-trip，`GetMaxResultWindow(target)` **僅在需要時呼叫**：(a) `N == 0`（分頁需要頁大小），或 (b) `N > 10000`（可能超過 window、需確認是否截斷）。`0 < N <= 10000` 時**不呼叫**，直接 `size:N` 單次 `_search`（10000 是 ES 預設下限，常見情境免去多餘請求）。
- **W fallback**：當 `GetMaxResultWindow` 不可用、回空、或解析不到值時，`W` 一律 fallback 為 **10000**（不得視為 0）。target 為 alias/datastream 跨多個 backing index 時，回傳為 per-index 設定 map，取**所有 index 的最小值**作為 `W`（安全下界，確保每頁 `size:W` 不被任一 index 拒絕）。
- `N > 0` 且 `N > W`：stderr 印 warning（`requested size N exceeds max_result_window W; capping to W`）並以 `size:W` 單次 `_search`。
- `N == 0`：取**全部**符合結果，以 `search_after` + sort 尾鍵分頁，每頁 `size:W`，迴圈直到回傳空頁。sort 末端附加 tiebreaker `_doc`（PIT-free 可用、每 shard 內穩定）確保分頁推進；無 PIT 期間若 index 持續寫入，邊界可能極少數重複/遺漏（見 Risks）。**不使用 `_shard_doc`**（其僅在 PIT context 合法，與本設計「不引入 PIT」衝突）。

理由：保留「有限、會結束」的 agent 友善語意，讓 `0` 能真正取全部而不撞 `max_result_window`，且不離開 `_search` 端點；lazy W-fetch 讓常見小 `N` 查詢維持單一 round-trip。

### 兩層過濾職責分離（仿 stern）

`--query`（Lucene query string，省略=`match_all`）在 **ES 端**縮小來源與結構化條件（走索引、高效），組成 `query_string`。`--include/--exclude`（可重複）在 **client 端**對每筆 hit 的**序列化 `_source` JSON 字串**做 regex 細篩（ES query string 不易表達的 pattern）。比對對象固定為 `_source`（**與 `-o` 格式無關**，避免 `_id`/`_index` 等 envelope 欄位干擾），確保同一組 include/exclude 在 jsonl 與 json 模式得到相同篩選結果。

**altitude 分工**：`--size/-n` 約束的是**向 ES 抓取的 hit 數**（即 ES `size`/分頁總量）；`--include/--exclude` 是對已抓回結果的**事後**過濾，因此最終輸出筆數可能**少於** `N`（仿 stern：抓 N 筆再 client 端細篩，不回補頁）。理由：各取所長，ES 負責高效初篩、client 負責表達力；不回補可保持「有限、會結束」與請求數可預測。

### 時間範圍與排序

`--since <dur>`（`15m`/`1h`/`24h`/`7d`）轉成相對 now 的 range；`--from`/`--to`（RFC3339）為絕對範圍，與 `--since` 互斥（同時給則報錯 exit 2）。時間欄位由 `--timestamp-field` 指定，預設 `@timestamp`，**range filter 與預設 sort 皆套用此欄位**：未給 `--sort` 時，預設 sort = `<timestamp-field>:desc`（例如 `--timestamp-field event.created` 時預設 sort 為 `event.created:desc`，不會留下硬編碼 `@timestamp` 導致對缺欄位的 index 排序錯誤）。顯式 `--sort` 覆寫此預設。

### 錯誤輸出與分層 exit code

錯誤一律輸出至 **stderr 純文字**，stdout 在失敗時不產生半截結果。退出碼：`0` 成功、`2` 參數/設定錯（含缺 context、`--since`/`--from` 衝突）、`3` 連線/認證失敗、`4` target 不存在（ES 404 index_not_found 轉成指出 target 名稱的友善訊息）。理由：agent 可靠靠 exit code 分支，不需解析訊息文字。

### signal handling

主程式攔截 SIGINT/SIGTERM，搜尋（含分頁迴圈）以 `context.Context` 可取消，退出碼遵循 128+signum（130/143）。

## Implementation Contract

**Behavior（使用者/agent 可觀察）**

- `es-log config get-contexts` → 列出設定檔中所有 context 名稱（預設 json）。
- `es-log config view` → 印解析後設定，`api-key`/`password` 顯示為 `***`。
- `es-log -c <ctx> ls [aliases|datastreams]` → 列出 targets，欄位 `name`、`type(alias|datastream)`、datastream 帶 `backing_indices_count`、alias 帶 `index_count`。
- `es-log -c <ctx> search -t <target> [-q '<lucene>'] [flags]` → 依輸出格式回傳 hits。
- `es-log -c <ctx> ping` → 對 `/_cluster/health` 驗證連線與認證；成功印健康摘要並 exit 0，連線/認證失敗 stderr + exit 3。
- `es-log version` → 印 binary 版本（不需 context）。

**Interface / data shape**

- 設定檔：扁平式 YAML，`contexts: [{name, server, auth:{type:apikey|basic, api-key|username+password}, tls:{ca-cert, insecure-skip-verify}}]`，無 `current-context`。
- `search` 送出 `POST /<target>/_search`，body：有 `--query` 時為 `query_string`（必要時與 `@timestamp` range 以 `bool.filter` 組合），省略 `--query` 時為 `match_all`（同樣可與 range 以 `bool` 組合）；另含 `size`、`sort`（末端含 `_doc` tiebreaker）、`--size 0` 時帶 `search_after`、`--fields` 時帶 `_source` 篩選。
- jsonl 輸出每行 = 純 `_source`；search json 輸出 = `{"total":N,"hits":[...]}` 帶 `_id`/`_index`/`_score`。
- 非 search 指令的 json 形狀：`ls` = row 物件**陣列**（`[{name,type,...}]`，可直接 `jq '.[]'`）；`config get-contexts` = 名稱**字串陣列**；`config view` = 解析後設定**物件**（機密 redact）。
- table 輸出每個指令的欄位 = 其 json 形狀的欄位（search：`_id`/`_index`/`@timestamp`/訊息摘要；`ls`：`name`/`type`/count），共用同一個對齊渲染器、不另立 per-command 排版。
- `--include/--exclude` 比對對象固定為每筆 hit 的序列化 `_source` JSON，與 `-o` 無關。
- esclient 公開方法集合僅為上述 5 個白名單方法。

**Failure modes**

- 缺 context（`-c` 與 `$ES_LOG_CONTEXT` 皆無）→ stderr 訊息 + 列出可用 context + exit 2。
- `--since` 與 `--from`/`--to` 同時給 → stderr + exit 2。
- 連線/認證失敗（含 `ping`）→ stderr + exit 3，stdout 無輸出。
- target 404 → stderr 友善訊息（含 target 名稱）+ exit 4。
- `--size N > W` → stderr warning（非錯誤），仍回 `W` 筆、exit 0。
- 未設定的 `${ENV_VAR}` → 僅在**實際使用該機密**（建立 ES 連線）時 stderr + exit 2；`config get-contexts`（只列名稱）與 `config view`（一律 redact、不需真值）**不**因其他 context 的未設定變數而失敗，仍正常列出/顯示。

**Acceptance criteria**

- e2e（httptest stub）：happy path（`--size 50` ≤ 10000 故**不**打 `_settings`，斷言僅送 `POST /_search`、request 正確 + jsonl/json 輸出正確 + include/exclude 篩選）、缺 context（exit **2**、stdout 乾淨）、連線失敗（exit **3**、stdout 乾淨）兩者**分別**斷言確定退出碼、target 404（exit 4、友善訊息）。
- 唯讀白名單鎖定單元測試：斷言無非白名單動詞/路徑。
- `--size` 行為測試：`N>W`（如 `--size 50000`）打 `_settings` 取 `W` → capping warning + 實送 `size:W`；`--size 0` 打 `_settings` 取 `W` 後多次 `_search` 含 `search_after` 且彙整全部筆數；`0<N<=10000`（如預設 50）**不**打 `_settings`。
- `GetMaxResultWindow` 端點（含 `flat_settings=true`）與解析測試、W fallback 10000、多 index 取最小值；`--timestamp-field` 覆寫 range 與預設 sort 的測試；`--since` 解析、`--from/--to` 互斥、`--fields` 轉 `_source`、`config view` redact、table 對齊單元測試。
- `make build` 產出 `CGO_ENABLED=0` 單一靜態 binary；`golangci-lint` / `go fmt` 乾淨。

**Scope boundaries**

- In scope：上述 5 capability、`skill/SKILL.md`、`README.md`（英文）、`Makefile`。
- Out of scope：任何寫入/維護端點、PIT/scroll、`--follow`、context 寫入指令、ES 7.x 相容、自動安裝 binary。

## Risks / Trade-offs

- [機密外洩（明文 config / log / view）] → `${ENV}` 展開、`view` redact、建議 `0600`、不印 Authorization header。
- [誤觸破壞性 API] → esclient 唯讀白名單 + 鎖定測試，從介面層杜絕。
- [`go-elasticsearch` 依賴過重] → 僅用少數端點且以白名單隔離；不可接受時可換 `net/http` 薄封裝，介面與測試不變。
- [datastream vs alias 語意差異致 `ls`/`search` 行為不一致] → `ls` 明確標示 type；`search` 對兩者皆走 `/<target>/_search`（ES 8.x 對 alias 與 datastream 皆支援）。
- [`--size 0` 分頁在高基數資料量大時耗時/記憶體壓力] → 串流逐頁輸出 jsonl（不全部緩存）、`context.Context` 可中斷；文件提醒搭配 `--since` 縮範圍。
- [`search_after` 無 PIT 期間 index 變動致邊界重複/遺漏] → 對唯讀 log 查詢可接受；以 `@timestamp` + tiebreaker 降低影響，文件說明此限制。

## Migration Plan

全新專案，無既有系統耦合。部署 = 取得/編譯 binary 放上 `PATH` + 手動建立 `~/.config/es-log/config.yaml`。Rollback = 移除 binary 與 skill 目錄，不影響 ES。

## Open Questions

(none — 規劃階段已於 plan.md 的 Decisions 全數拍板)
