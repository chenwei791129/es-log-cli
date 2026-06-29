## 1. Read-only client aggregation parsing

- [x] 1.1 （Requirement: Read-only client aggregation parsing）在 `internal/esclient/client_test.go` 新增測試：當 `_search` 回應含 `aggregations` 區塊時，`SearchResponse` 暴露該區塊的原始 JSON；先讓測試紅燈。驗證：`go test ./internal/esclient` 該測試由紅轉綠。
- [x] 1.2 （Design D1）在 `internal/esclient/client.go` 為 `SearchResponse` 新增 `Aggregations json.RawMessage` 欄位並解析回應 `aggregations`，且解析 `_shards.failed` 與 `failures` 供呼叫端判斷部分分片失敗；不新增任何端點或 HTTP verb，仍走既有 `Search`／`POST /<target>/_search`。驗證：1.1 測試綠燈，且既有 `internal/esclient` 測試不回歸。

## 2. Structured aggregation flags (A)

- [x] 2.1 （Requirement: Structured aggregation flags）在 `internal/cmd/aggregation_test.go` 新增測試：由 `--terms log.info:10 --metric sum:bytes --cardinality client.ip` 產生的 `aggs` 等於 design 契約所列的 canonical body（`group` terms 含 `sum_bytes`、`cardinality_client.ip` 子聚合）；先紅燈。驗證：`go test ./internal/cmd` 該測試由紅轉綠。
- [x] 2.2 （Design D2 旗標文法、D3 單層分桶+桶內 metric、D4 固定命名）在 `internal/cmd/aggregation.go` 實作結構化旗標解析與 `aggs` 產生：`--terms <field>[:<size>]`（預設 size 10、依 doc_count 遞減）、`--date-histogram <field>:<interval>`（`s/m/h/d`→`fixed_interval`，`w/M/y`→`calendar_interval`）、`--metric <op>:<field>`（op∈sum/avg/min/max/value_count，可重複）、`--cardinality <field>`（可重複），命名遵循 D4 的 `group`／`<op>_<field>`／`cardinality_<field>`，並支援 D3「有分桶時 metric 作為桶內子聚合、無分桶時為 top-level metric」。驗證：2.1 與涵蓋 date_histogram、純 metric 兩情境的測試綠燈。

## 3. Raw aggregation passthrough (B)

- [x] 3.1 （Requirement: Raw aggregation passthrough）在 `internal/cmd/aggregation_test.go` 新增測試：`--aggs '{"top_isp":{"terms":{"field":"isp"}}}'` 搭配 `--since 1h` 時，request body 將該物件原樣置入 `aggs`，並含 `now-1h` range 的 `query`；先紅燈。驗證：`go test ./internal/cmd` 該測試由紅轉綠。
- [x] 3.2 （Design D5）在 `internal/cmd/aggregation.go` 實作 `--aggs` 解析：值須為合法 JSON 物件，原樣作為 `aggs`，外層由既有 query/range 組裝邏輯補上 `query`。驗證：3.1 測試綠燈。

## 4. Flag validation and mutual exclusion

- [x] 4.1 （Requirement: Aggregation flag mutual exclusion and validation）在 `internal/cmd/search_test.go`（或 `aggregation_test.go`）新增測試：A 聚合旗標與 `--aggs` 並用、`--terms` 與 `--date-histogram` 並用、`--metric median:bytes`（未知 op）、`--terms` size 非正整數、`--date-histogram` 缺 interval、`--aggs` 非合法 JSON 物件，皆回 exit code 2 且不送出請求；先紅燈。驗證：`go test ./internal/cmd` 這些案例綠燈。
- [x] 4.2 （Design D6 互斥）在 `internal/cmd/search.go` 的旗標驗證階段（`searchParams.validate`）接上 4.1 的互斥與格式檢查，於送出請求前以 `exitUsage` 攔截。驗證：4.1 測試綠燈，且既有 search 驗證測試不回歸。

## 5. Wiring: shared query, size default, request build

- [x] 5.1 （Requirement: Shared query and time range with aggregation、Requirement: Default hit size during aggregation、Design D7）在 `internal/cmd/search.go` 註冊 `--terms`/`--date-histogram`/`--metric`/`--cardinality`/`--aggs` 旗標，使既有 `--query`/`--since`/`--from`/`--to`/`--timestamp-field` 組出的 `query`/range 同時套用於兩種聚合模式，並讓 `buildBody` 在有任一聚合旗標時注入 `aggs` 並將 `size` 預設為 0、`--size N` 可覆寫成同時回 N 筆 hit。驗證：新增測試斷言「`-q level:error --since 24h --terms service` 的 body 含 bool query + range + aggs」「無 `--size` 時 body `size=0`」「`--size 5` 時 body `size=5` 且仍含 aggs」綠燈。

## 6. Aggregation output rendering

- [x] 6.1 （Requirement: Aggregation output formats）在 `internal/cmd/aggregation_test.go` 新增渲染測試：`json` 輸出為 `{"total","aggregations","hits"}`（size:0 時 hits 空）；`jsonl` 結構化分桶模式每桶攤平為 `{"key","doc_count","<metric>"}`（例：bucket `{"key":"timeout","doc_count":42,"sum_bytes":{"value":1024}}` → `{"key":"timeout","doc_count":42,"sum_bytes":1024}`）；純 metric 模式單行；B 模式 jsonl/table 輸出原始 `aggregations` 物件；`table` 結構化分桶模式以共用 renderer 輸出 `key`/`doc_count`/各 metric 欄。先紅燈。驗證：`go test ./internal/cmd` 渲染測試由紅轉綠。
- [x] 6.2 在 `internal/cmd/aggregation.go` 實作三格式渲染，table 沿用 `internal/output` 既有共用 aligned-column renderer（`RenderTable`），並在 `runSearch` 依「是否聚合」分流至聚合渲染或既有 hit 渲染。驗證：6.1 測試綠燈，且既有非聚合 search 輸出測試不回歸。

## 7. Shard failure surfacing

- [x] 7.1 （Requirement: Partial shard failure surfacing for aggregations）在 `internal/cmd/aggregation_test.go` 新增測試：聚合回應 `_shards.failed > 0` 時，stderr 印出含 failure reason 的警告且仍輸出已得到的聚合，不靜默回 0；先紅燈。驗證：`go test ./internal/cmd` 該測試由紅轉綠。
- [x] 7.2 在聚合處理路徑接上 7.1 行為，使用既有 `output.Printer` 的 `Warnf` 印警告（`--quiet` 時抑制）；僅作用於聚合路徑，不改動既有 raw-hit 查詢行為。驗證：7.1 測試綠燈。

## 8. Documentation

- [x] 8.1 [P] 更新 `skill/SKILL.md`：在 `search` 段補上 A/B 聚合旗標、互斥規則、`size:0` 預設與覆寫、三格式輸出形狀、date_histogram interval 後綴對應，與一個 netflow `--aggs` 範例。驗證：內容審查確認涵蓋上述各點且範例可對應 spec 行為。
- [x] 8.2 [P] 更新 `README.md`（英文）：同步聚合旗標說明與範例。驗證：內容審查確認與 SKILL.md/spec 一致且為英文。
