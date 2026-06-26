---
name: es-log
description: 以唯讀方式查詢 Elasticsearch 中的 log。當需要從 ES（beats → index alias 或 datastream）撈取、搜尋、過濾 log，且要求「絕不執行破壞性操作」與「agent 友善的 JSONL 輸出」時使用。觸發於「查 ES log」「search elasticsearch logs」「撈 app-logs 的 error」等情境。
---

# es-log

`es-log` 是一個唯讀的 Elasticsearch log 查詢 CLI，從程式碼層面保證只會發出讀取請求（無 `PUT`/`DELETE`/`PATCH`、無 `_rollover`/`_reindex`），輸出預設為適合 `jq`/pipe 的 JSONL，並以分層 exit code 讓程式可靠分支。

## 何時使用

- 需要從 Elasticsearch 撈取或搜尋 log（alias 或 datastream）。
- 需要安全、不可能誤觸破壞性端點的查詢入口。
- 需要可被 `jq` 逐行消費的結構化輸出，或需要 metadata/total 的整包結果。

不要用於：任何寫入/維護操作（本工具刻意不提供）、即時 tail（`--follow` 尚未支援）、ES 7.x（僅支援 8.x+）。

## 前置條件

- `es-log` 已安裝於 `PATH`。
- 設定檔 `~/.config/es-log/config.yaml` 已建立（見 README）。可用 `--config` 或 `$ES_LOG_CONFIG` 覆寫。
- 每個 ES 指令都必須指定 context：用 `--context/-c <name>` 或設 `$ES_LOG_CONTEXT`。沒有隱藏的 current-context。

## 全域 flags

- `-c, --context <name>`：選擇要連線的 context（或設 `$ES_LOG_CONTEXT`）。
- `-o, --output <jsonl|json|table>`：輸出格式。`search` 預設 `jsonl`，其餘指令預設 `json`。
- `--quiet`：抑制 warning 等雜訊，stdout 只留結果。
- `--config <path>`：設定檔路徑。

## 指令

### `es-log config get-contexts`

列出設定檔中所有 context 名稱（不需選定 context）。

```bash
es-log config get-contexts            # JSON 字串陣列
es-log config get-contexts -o table
```

### `es-log config view`

印出解析後設定，`api-key`/`password` 一律 redact 為 `***`（不需 context、不展開機密）。

### `es-log -c <ctx> ping`

打 `GET /_cluster/health` 驗證連線與認證。成功 exit 0；連線/認證失敗 exit 3。

### `es-log -c <ctx> ls [aliases|datastreams]`

合併列出 aliases 與 datastreams，每列含 `name` 與 `type`。`ls aliases` / `ls datastreams` 只列單一型別。datastream 列帶 `backing_indices_count`，alias 列帶 `index_count`。

```bash
es-log -c prod ls                     # 合併視圖（JSON 陣列）
es-log -c prod ls datastreams -o table
```

### `es-log -c <ctx> search -t <target> [flags]`

對 `<target>`（alias 或 datastream）執行 `POST /<target>/_search`。target 也可用 positional argument 傳入。

主要 flags：

- `-q, --query <lucene>`：Lucene query string（省略時 `match_all`）。
- `--since <dur>`：相對時間範圍，如 `15m`/`1h`/`24h`/`7d`。與 `--from`/`--to` 互斥。
- `--from <RFC3339> --to <RFC3339>`：絕對時間範圍。
- `--timestamp-field <field>`：時間欄位（預設 `@timestamp`），同時套用到 range filter 與預設 sort。
- `-n, --size <N>`（別名 `--limit`，預設 50）：抓取上限。`--size 0` 以 `search_after` 取**全部**（超過 `max_result_window` 自動分頁）。`N` 超過 window 會截斷並於 stderr 警告。
- `--fields <a,b,c>`：只回傳指定 `_source` 欄位。
- `--sort <field:asc|desc>`：排序（預設 `<timestamp-field>:desc`）。
- `-i, --include <regex>` / `-e, --exclude <regex>`（可重複）：在 client 端對每筆 `_source` 的序列化 JSON 做 regex 細篩（與 `-o` 格式無關）。

#### Lucene query 範例

```bash
# 1 小時內的 error
es-log -c prod search -t app-logs -q 'level:error' --since 1h

# 多條件 + 欄位投影 + client 端細篩
es-log -c prod search -t app-logs \
  -q 'level:error AND service:checkout' \
  --fields '@timestamp,level,message' \
  --include 'timeout|deadline' --exclude 'healthcheck' \
  --since 24h

# 取全部符合結果（自動分頁）
es-log -c prod search -t app-logs -q 'status:500' --size 0 > errors.jsonl

# 絕對時間範圍 + 自訂時間欄位
es-log -c prod search -t metrics -q '*' \
  --timestamp-field event.created \
  --from 2026-06-01T00:00:00Z --to 2026-06-02T00:00:00Z
```

## 輸出格式

- **jsonl**（`search` 預設）：每行一筆 = 純 `_source`，無 `_id`/`_index`/`_score` 包裝。最適合 `jq -c`、`grep`、`head`、串流。
- **json**：`search` 為 `{"total":N,"hits":[{"_id","_index","_score","_source"}]}`；`ls` 為 row 物件陣列；`config get-contexts` 為名稱字串陣列；`config view` 為設定物件。
- **table**：人類可讀對齊欄位。

`--include/--exclude` 比對對象固定為 `_source` JSON，因此同一組 pattern 在 jsonl 與 json 下得到相同篩選結果。

## Exit code

| code | 意義 |
| ---- | ---- |
| 0 | 成功 |
| 2 | 參數/設定錯（缺 context、`--since` 與 `--from/--to` 衝突、缺 target、機密 `${ENV_VAR}` 未設） |
| 3 | 連線或認證失敗 |
| 4 | target 不存在（ES 404 index_not_found） |

錯誤一律走 stderr 純文字；失敗時 stdout 不會有半截結果。
