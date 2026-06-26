## 1. 專案骨架與建構

- [x] 1.1 建立 go.mod 與標準 layout（cmd/ + internal/），完成「採 cobra 建構 CLI 指令樹」：`es-log` 根指令掛載 config/ls/search/ping/version 子指令與全域 flags `--context/-c`、`--output/-o`、`--quiet`、`--config`。驗證：`go run ./cmd/es-log --help` 列出所有子指令與全域 flags。
- [x] 1.2 完成 Makefile，產出 `CGO_ENABLED=0` 單一靜態 binary；`version` 子指令印出版本字串。驗證：`make build` 成功且 `./es-log version` 印出版本。
- [x] 1.3 完成 signal handling：主程式攔截 SIGINT/SIGTERM，長時間操作以 `context.Context` 可取消，退出碼遵循 128+signum（130/143）。驗證：對執行中 search 送 SIGINT，程式中止並回 exit 130（手動驗證 + cancel 單元測試）。

## 2. 設定與 context 解析（context-config）

- [x] 2.1 實作「設定檔格式與機密展開」之載入，滿足「Config file location and override」：扁平式 YAML（無 current-context），路徑預設 `~/.config/es-log/config.yaml`，可由 `--config` 與 `$ES_LOG_CONFIG` 覆寫（flag 優先）。驗證：`TestConfigPathOverride` 斷言覆寫優先序。
- [x] 2.2 [P] 實作「Environment variable expansion for secrets」：使用該 context 機密時（建立連線）才展開 `${ENV_VAR}`，未設定變數對「被使用的 context」回 exit 2；`config get-contexts`/`config view` 不因其他 context 的未設定變數而失敗。驗證：`TestEnvExpansion`（使用時已定義變數成功展開、使用時未定義報錯 exit 2、`get-contexts` 在另一 context 變數未設定仍 exit 0）。
- [x] 2.3 實作「Context selection without hidden default」對應「context 解析：顯式、無隱藏狀態」：ES 指令的 context 由 `--context/-c` 優先、再 `$ES_LOG_CONTEXT`；皆缺時印錯誤 + 列出可用 context 名稱 + exit 2。驗證：`TestContextResolution`（flag 優先、env fallback、缺 context exit 2 且 stdout 空）。
- [x] 2.4 [P] 實作 `config get-contexts`，滿足「List contexts」：列出設定檔所有 context 名稱、不需選定 context。驗證：`TestGetContexts` 斷言輸出含全部名稱。
- [x] 2.5 [P] 實作 `config view`，滿足「View resolved configuration with secret redaction」：`api-key`/`password` 一律 redact 為 `***`，不印 Authorization header。驗證：`TestConfigViewRedaction` 斷言輸出含 `***` 且不含原始機密。

## 3. 唯讀 ES client（readonly-es-client）

- [x] 3.1 完成「esclient 唯讀端點白名單（唯讀保證的設計核心）」並依「用官方 go-elasticsearch 還是自封裝 net/http」決策實作，滿足「Read-only endpoint whitelist」：只暴露 `ListAliases`/`ListDataStreams`/`Search`/`GetMaxResultWindow`/`Ping`，無接受任意 verb/path 的通用 request 介面。驗證：`TestListAliases`/`TestListDataStreams` 對 httptest stub 斷言送出正確 verb/path。
- [x] 3.2 [P] 實作「Authentication」：API Key 送 `Authorization: ApiKey <key>`、Basic Auth 送 HTTP Basic header，不支援其他模式。驗證：`TestAuthApiKey`、`TestAuthBasic` 斷言 stub 收到對應 Authorization header。
- [x] 3.3 [P] 實作「TLS configuration」：支援 `tls.ca-cert` 與 `tls.insecure-skip-verify`。驗證：`TestTLSCustomCA`、`TestTLSInsecureSkipVerify` 對 httptest TLS server 連線成功。
- [x] 3.4 實作「Connectivity check」（`Ping` 打 `GET /_cluster/health`）與「Max result window resolution」（`GetMaxResultWindow` 打 `_settings/index.max_result_window?include_defaults=true&flat_settings=true`，解析 flat key、跨多 index 取最小值、解析不到讓 caller fallback 10000）。驗證：`TestPing`（健康成功、認證失敗報錯）、`TestGetMaxResultWindow` 斷言端點含 `flat_settings=true`、單 index 解析、多 index 取最小值。
- [x] 3.5 撰寫唯讀白名單鎖定測試：枚舉 esclient 公開方法，斷言不存在發出 `PUT`/`DELETE`/`PATCH` 或非白名單路徑的程式碼路徑。驗證：`TestReadOnlyWhitelistLock` 通過。
- [x] 3.6 實作 `es-log ping` 指令：包裝 `Ping()`，成功 exit 0、連線/認證失敗 stderr + exit 3。驗證：`TestPingCommand`（成功 exit 0、不可達 exit 3）。

## 4. 輸出格式與錯誤（output-format）

- [x] 4.1 實作「Output format selection and defaults」：`-o jsonl|json|table`，`search` 預設 jsonl、其餘預設 json，table 為對齊欄位。驗證：`TestOutputDefaults`、`TestTableAlignment`。
- [x] 4.2 實作「search 輸出 schema 契約」之 jsonl 部分，滿足「Search JSONL output is bare source」：每行 = 純 `_source`、無 `_id`/`_index`/`_score`，`--fields` 時只含投影欄位。驗證：`TestJSONLBareSource` 斷言每行為合法 JSON 且等於 `_source`。
- [x] 4.3 [P] 實作「Search JSON output carries metadata」：json 模式輸出 `{"total":N,"hits":[{_id,_index,_score,_source}]}`。驗證：`TestJSONWithMetadata` 斷言 `total` 與 metadata 欄位齊備。
- [x] 4.4 [P] 實作「Quiet mode keeps stdout clean」：`--quiet` 抑制連線資訊/進度/warning，stdout 只含結果，雜訊改走 stderr。驗證：`TestQuietSuppressesWarning` 斷言 capping warning 不出現在 stdout。
- [x] 4.5 實作「錯誤輸出與分層 exit code」對應「Errors on stderr with layered exit codes」：錯誤走 stderr 純文字、stdout 失敗時不吐半截結果，exit code 分層 0/2/3/4。驗證：`TestExitCodeMapping` 表格驅動斷言各情境退出碼。
- [x] 4.6 [P] 實作「JSON document shapes for non-search commands」：`ls` 輸出 row 物件陣列、`config get-contexts` 輸出名稱字串陣列、`config view` 輸出設定物件；table 共用同一對齊渲染器、欄位對應各指令 json 形狀。驗證：`TestLsJSONArray`、`TestGetContextsJSONArray` 斷言形狀可被 `jq '.[]'` 迭代。

## 5. 列舉 targets（list-targets）

- [x] 5.1 實作「List aliases and datastreams in a combined view」：`ls` 合併列出 aliases 與 datastreams，每列含 `name` 與 `type(alias|datastream)`。驗證：`TestLsCombined` 對 stub 斷言合併視圖與 type 標示。
- [x] 5.2 [P] 實作「Filter listing by target type」：`ls aliases` 只列 alias、`ls datastreams` 只列 datastream。驗證：`TestLsAliasesOnly`、`TestLsDatastreamsOnly`。
- [x] 5.3 [P] 實作「Type-specific count fields」：datastream 列帶 `backing_indices_count`、alias 列帶 `index_count`。驗證：`TestLsCountFields` 斷言計數正確。

## 6. 搜尋 logs（log-search）

- [x] 6.1 實作「Target selection」：`search` 經 `-t/--target` 或 positional arg 指定 target，送 `POST /<target>/_search`，缺 target 報錯 exit 2。驗證：`TestSearchTargetFlag`、`TestSearchMissingTarget`。
- [x] 6.2 實作「Lucene query」並落實「兩層過濾職責分離（仿 stern）」之 ES 端：`-q` 組 `query_string`，省略時 `match_all`。驗證：`TestQueryString`、`TestMatchAllDefault` 斷言 request body。
- [x] 6.3 實作「Time range filtering」對應「時間範圍與排序」：`--since` 轉相對 range、`--from/--to` 絕對範圍且與 `--since` 互斥（衝突 exit 2），`--timestamp-field` 預設 `@timestamp` 且**同時**套用到 range filter 與預設 sort 欄位。驗證：`TestSinceRange`、`TestTimeFlagsMutualExclusion`、`TestTimestampFieldRetargetsRangeAndSort`（`--timestamp-field event.created` 時 range 與預設 sort 皆用 `event.created`）。
- [x] 6.4 實作「Result size and unbounded pagination」對應「--size 語意與 search_after 分頁」：lazy 取 `W`（僅 `--size 0` 或 `N>10000` 才打 `GetMaxResultWindow`，否則直接 `size:N`）、W fallback 10000、`N>W` 截斷並 stderr warning、`--size 0` 以 `search_after`（末端 `_doc` tiebreaker，**不**用 `_shard_doc`）逐頁取全部；`--size` 約束抓取量、不回補 include/exclude 過濾掉的筆數。驗證：`TestSizeNoLookupForSmallN`（預設 50 不打 `_settings`）、`TestSizeCapping`、`TestSizeZeroPagination`（stub 回多頁、後續請求含 search_after、彙整全部）、`TestWindowFallbackTo10000`。
- [x] 6.5 [P] 實作「Source field projection and sorting」：`--fields` 轉 `_source` 投影、`--sort` 預設為解析後 timestamp-field 的 `:desc`。驗證：`TestFieldsProjection`、`TestDefaultSort`。
- [x] 6.6 實作「Client-side regex filtering」並落實「兩層過濾職責分離（仿 stern）」之 client 端：`--include/--exclude`（可重複）對每筆 hit 的序列化 `_source` JSON（與 `-o` 無關、`--fields` 投影後）做 regex 篩選。驗證：`TestIncludeFilter`、`TestExcludeFilter`、`TestFilterSameAcrossFormats`（jsonl 與 json 同結果）。

## 7. e2e 測試（httptest 模擬 ES）

- [x] 7.1 撰寫 happy path e2e：指向 stub 的 context 執行 `search -c test -t app-logs -q 'level:error' --since 1h`（預設 size 50 ≤ 10000，故**不**打 `_settings`），斷言僅送出 `POST /app-logs/_search`（body 含 query_string、`@timestamp` range、size:50、sort desc）、jsonl 為純 `_source`、`-o json` 帶 metadata/total、`--include/--exclude` 正確篩選。驗證：`TestE2EHappyPath` 通過。
- [x] 7.2 撰寫錯誤 e2e — 缺 context 與連線失敗（**分別**確定退出碼）：未給 `-c` 且 `$ES_LOG_CONTEXT` 未設 → exit **2**；server 不可達 → exit **3**；兩者 stdout 皆乾淨。驗證：`TestE2EMissingContext`（斷言 exit==2）、`TestE2EConnectionFailure`（斷言 exit==3）。
- [x] 7.3 撰寫錯誤 e2e — target 不存在：stub 回 404 index_not_found，斷言友善錯誤（含 target 名稱）+ exit 4。驗證：`TestE2ETargetNotFound`。

## 8. 文件與品質

- [x] 8.1 [P] 撰寫 `skill/SKILL.md`：說明何時使用、各指令、Lucene query 範例、jsonl/json 輸出格式與 exit code。驗證：內容審查涵蓋上述章節且呼叫 PATH 上的 `es-log`。
- [x] 8.2 [P] 撰寫 `README.md`（英文）：安裝、設定檔範例、使用範例。驗證：內容審查涵蓋安裝/設定/使用三節。
- [x] 8.3 將 `golangci-lint` 以 `go get -tool` 加入 `go.mod`（不全域安裝），確保 `go tool golangci-lint run` 與 `go fmt` 乾淨、`make build` 產出靜態 binary。驗證：`go tool golangci-lint run` 與 `gofmt -l .` 無輸出、`make build` 成功。
