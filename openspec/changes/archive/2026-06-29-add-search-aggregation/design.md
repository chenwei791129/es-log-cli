## Context

`es-log search` 目前只會送出一個「拉 raw hits」的 `POST /<target>/_search` 請求：body 由旗標組成 `match_all`/`query_string` + `bool/filter` 時間範圍 + `sort` + `_source` + `search_after`，並解析回應的 `hits`。回應裡的 `aggregations` 區塊完全沒有被組出來、也沒有被解析。

`esclient` 是刻意設計的「白名單式」唯讀 client：每個端點都是專屬方法、綁定固定 HTTP verb 與 path，沒有泛用 request 方法，破壞性操作 by construction 不可能發出。`Search` 方法已經走 `POST /<target>/_search`，body 由呼叫端提供。

Elasticsearch 的 aggregation（terms / date_histogram / sum / cardinality / pipeline…）全部都是在 `_search` 的 body 裡加一個 `aggs` key，回應多一個 `aggregations` 區塊。因此聚合能力可以完全沿用既有端點，不需要新端點、不破壞唯讀保證。

`itop-wc-check` 的 benchmark 顯示：缺少 server-side 聚合是 es-log 無法取代 Grafana datasource-proxy `_msearch` 的唯一阻礙。netflow 的 `sum(bytes)` + `date_histogram 5m` + Top ISP/ASN、nginx 的 top buckets、Bifrost 的 unique-IP 計數，目前都得拉數百萬筆 raw doc 在 client 端聚合，不可行。

## Goals / Non-Goals

**Goals:**

- 讓 `es-log search` 能下達 server-side 聚合，覆蓋 benchmark 中 aggregation-heavy section 的需求。
- 提供兩種互補介面：結構化旗標（A，受控、輸出可正規化）與 raw `--aggs`（B，吃下巢狀/排序/pipeline 等 A 表達不出的形狀）。
- 維持既有唯讀保證：不新增端點、不新增 HTTP verb、不引入泛用 request 方法。
- 保留 es-log 相對 raw curl 的價值：時間範圍與 Lucene query 仍由 es-log 統一組裝，兩種聚合模式共用。

**Non-Goals:**

- 不在結構化旗標（A）裡支援多層巢狀聚合或「依子聚合排序 terms」——這些交給 `--aggs`（B）。
- 不實作 `_msearch` 批次查詢：benchmark 顯示平行呼叫多個 es-log 程序已勝過 `_msearch` batch，CLI 實作它為負收益。
- 不新增獨立的 `_count` 端點；count-only 可由聚合或既有 `track_total_hits` 達成。
- 不處理 Grafana/Kibana 與 es-log 之間 `analyze_wildcard`/`default_operator` 造成的 count 語義差異（屬獨立的 query 語義議題）。
- 不支援 live tail / `--follow`。

## Decisions

### D1

**聚合沿用既有 `_search` 端點，不新增白名單方法。** A 與 B 產生的聚合都併入既有 `Search` 方法送出的 body。`esclient.SearchResponse` 新增一個 `Aggregations json.RawMessage` 欄位來捕捉回應的 `aggregations` 區塊。白名單清單不變，唯讀保證不變。即使是 B 的 raw passthrough 也安全——它被限制在 `_search` 的 body 內，動不了 verb/path。

### D2

**A 的旗標文法把參數內嵌在值裡，讓 `--size` 維持「hit 數」語義。** 既有 `--size/-n` 指的是要抓幾筆 hit，不能拿來當 bucket 數。因此 A 的桶大小等參數內嵌在旗標值：

- `--terms <field>[:<size>]`：對 field 做 terms 分桶，預設 bucket size 10，依 `doc_count` 遞減排序。
- `--date-histogram <field>:<interval>`：時間分桶。interval 後綴 `s`/`m`/`h`/`d` 映射成 `fixed_interval`；`w`/`M`/`y` 映射成 `calendar_interval`。
- `--metric <op>:<field>`（可重複）：`op ∈ {sum, avg, min, max, value_count}`。
- `--cardinality <field>`（可重複）：近似去重計數。

### D3

**A 只支援「單層分桶 + 桶內 metric」。** A 最多允許一個分桶旗標（`--terms` 與 `--date-histogram` 互斥）。`--metric`/`--cardinality` 若與分桶旗標並用，會成為桶內的子 metric；若單獨使用，則是 top-level 的單一 metric（回傳單一數值）。「兩層以上分桶」與「依子 metric 排序 terms」不在 A 範圍，明確導向 B。

### D4

**A 產生的聚合使用固定可預測命名。** 為了讓輸出 key 可預測：

- 分桶聚合一律命名 `group`。
- metric 命名 `<op>_<field>`（例：`sum_bytes`、`value_count_status`）。
- cardinality 命名 `cardinality_<field>`。

### D5

**B（`--aggs`）只吃「aggs 子樹」，不吃完整 body。** `--aggs` 接收的是 `aggs` key 的內容（單一 JSON 物件）。es-log 負責包上 `query`（含時間範圍）、`size`。這是 B 相對 raw curl 的價值：使用者只寫聚合那棵樹，時間/查詢便利性照舊。

### D6

**A 的聚合旗標與 B 的 `--aggs` 互斥。** 同一個 `aggs` key 不該有兩個來源。`--terms`/`--date-histogram`/`--metric`/`--cardinality` 任一與 `--aggs` 並用，回 exit code 2 的 usage error（沿用既有 `--since` 與 `--from/--to` 的互斥處理模式）。

### D7

**有聚合時 `size` 預設為 0，可被 `--size` 覆寫。** 純聚合查詢通常不要 raw hits。當任一聚合旗標出現時，請求預設 `size: 0`（只回桶）；`--size N` 覆寫成同時回 N 筆 hit 與聚合結果。

## Implementation Contract

**行為（使用者觀察到的）**

- `es-log search -t <target> --terms <field>[:<size>] [--metric <op>:<field> ...] [--cardinality <field> ...]` 回傳該 field 的 terms 桶，每桶含 `doc_count` 與要求的 metric。
- `es-log search -t <target> --date-histogram <field>:<interval> [--metric ...]` 回傳時間桶。
- `es-log search -t <target> --metric sum:bytes`（無分桶）回傳單一數值。
- `es-log search -t <target> --aggs '<json物件>'` 回傳該聚合子樹的結果，並自動套用 `--query`/`--since` 等組出的 `query` 與 `size: 0`。
- `--query`、`--since`、`--from/--to`、`--timestamp-field` 對兩種模式皆生效，組進 `query`/range。

**介面 / 資料形狀**

es-log 為 A 產生的 canonical body（範例：`--terms log.info:10 --metric sum:bytes --cardinality client.ip`）：

    {
      "size": 0,
      "query": { ...既有 query+range... },
      "aggs": {
        "group": {
          "terms": { "field": "log.info", "size": 10 },
          "aggs": {
            "sum_bytes": { "sum": { "field": "bytes" } },
            "cardinality_client.ip": { "cardinality": { "field": "client.ip" } }
          }
        }
      }
    }

B（`--aggs '{"x":{...}}'`）：es-log 將該物件原樣放進 `aggs`，外層補 `query` 與 `size: 0`。

`esclient.SearchResponse` 新增 `Aggregations json.RawMessage`（對應回應的 `aggregations`）。

輸出形狀：

- **json**：`{"total": N, "aggregations": <block>, "hits": [...]}`；`size:0` 時 `hits` 為空陣列。A 用 canonical 命名，B 用使用者命名。
- **jsonl**：
  - A 分桶模式：每桶一行，攤平為 `{"key": <桶鍵>, "doc_count": N, "<metric名>": <值>, ...}`；date_histogram 的鍵取 `key_as_string`。
  - A 純 metric 模式：單行 `{"<metric名>": <值>}`。
  - B 模式：單行 = 原始 `aggregations` 物件（無法可靠攤平任意巢狀）。
- **table**：
  - A 分桶模式：欄位 `key`、`doc_count`，後接每個 metric 一欄；沿用既有共用 aligned-column renderer。
  - A 純 metric 模式：單列表格。
  - B 模式：輸出原始 `aggregations` JSON（任意巢狀無法對齊成欄）。

**失敗模式**

- `--aggs` 非合法 JSON 物件 → stderr 錯誤、exit 2。
- A 聚合旗標與 `--aggs` 並用 → exit 2。
- `--terms` 與 `--date-histogram` 並用 → exit 2。
- `--metric` 格式非 `op:field` 或 `op` 不在允許集合 → exit 2。
- `--terms` 的 size 非正整數、`--date-histogram` 缺 interval 或 interval 格式非法 → exit 2。
- 回應 `_shards.failed > 0`（如 `Fielddata is disabled on [ip]` 這類部分分片失敗）→ 在 stderr 印出警告，列出 failure reason，仍輸出已得到的聚合；**絕不**把部分失敗當成空結果靜默回 0。
- 上述 exit-2 錯誤皆在送出請求前於旗標驗證階段攔截。

**驗收標準**

- `internal/cmd/aggregation_test.go` 覆蓋：A body 產生（terms / date_histogram / metric / cardinality 各組合與單層巢狀）、B passthrough 的 body 包裝、互斥與格式錯誤的 exit code、`size:0` 預設與 `--size` 覆寫、三種輸出格式的渲染、`_shards.failed` 警告。
- 既有 `internal/cmd/search_test.go` 不回歸（無聚合旗標時行為與輸出不變）。
- 手動驗證：`es-log -c it-es search -t netflow-external --since 1h --aggs '{"top_isp":{"terms":{"field":"isp","size":10,"order":{"bytes":"desc"}},"aggs":{"bytes":{"sum":{"field":"bytes"}}}}}'` 回傳 Top ISP 桶。

**範圍邊界**

- **In scope**：上述 A 旗標（單層分桶 + 桶內/top-level metric/cardinality）、B 的 `--aggs` 子樹 passthrough、兩模式互斥與共用 query/time 旗標、`size:0` 預設與覆寫、三格式輸出、`_shards.failed` 警告、`esclient.SearchResponse` 新增 aggregations 解析、SKILL.md 與 README.md 文件更新。
- **Out of scope**：A 的多層巢狀與依子 metric 排序（用 B）、`_msearch`、獨立 `_count` 端點、`analyze_wildcard`/`default_operator` 的 query 語義對齊、live tail。

## Risks / Trade-offs

- **B 是 ES DSL 逃生口**：相對 es-log「精選安全介面」哲學有張力。緩解：仍限制在 `_search` body、仍唯讀、送出前驗證為合法 JSON 物件，動不了 verb/path。
- **B 在 jsonl/table 無法攤平**：任意巢狀無法保證攤平成行/欄。緩解：B 模式 jsonl/table 輸出原始 `aggregations` 物件，文件明示 B 建議搭配 `-o json`。
- **date_histogram interval 的 fixed vs calendar 映射**可能讓使用者意外（例：`1M` 走 calendar）。緩解：在 SKILL.md/README 明確記載後綴對應規則。
- **新增 `_shards.failed` 警告擴及聚合路徑**：既有 raw-hit 查詢未檢查分片失敗，本變更只在聚合路徑加警告，避免擴大既有行為；raw-hit 路徑維持現狀。
- **count 語義差異未解**：es-log 的 `query_string` 預設 `analyze_wildcard:false`/`default_operator:OR`，與 Grafana/Kibana 可能不同，count 仍可能有小差異——明確列為 out of scope，留待獨立變更。
