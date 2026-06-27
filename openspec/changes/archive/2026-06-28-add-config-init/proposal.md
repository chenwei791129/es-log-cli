## Why

README 自稱「Designed for AI agents first」,但 config 檔格式對第一次接觸的 agent 並不可發現:`es-log --help` 與 `es-log config --help` 都不提 config 檔的預設路徑與欄位,也沒有任何 scaffold 指令。靠 `--help` 自我探索的 agent 摸不到 config 格式,只能去翻 README 或原始碼。格式說明目前散在 README、`openspec/specs/context-config/spec.md` 與 `internal/config/config.go` 三處,但都不在 agent 最自然的探索路徑上。

## What Changes

- 在 `es-log config` 命令的 Long description 補上 config 檔的預設路徑(`~/.config/es-log/config.yaml`、`$XDG_CONFIG_HOME`、`$ES_LOG_CONFIG` 覆寫順序)與一段 minimal YAML 範例。
- 新增 `es-log config init` 子命令:**只**將一份註解式的範本 config YAML 印到 stdout,涵蓋 `apikey` 與 `basic` 兩種 auth 形態並示範 `${VAR}` secret 引用。
- `config init` 不寫檔、無 `--write` flag、不讀也不修改任何既有 context;使用者自行 `es-log config init > ~/.config/es-log/config.yaml` 落地。它仍以既有 `formatFor` 路徑驗證 `-o`(無效值 exit 2),維持全 CLI 一致的參數驗證契約。
- 範本 YAML 字串集中為 `internal/config` package 的單一 const,help text 與 `config init` **逐字共用**(非手抄節錄),避免 schema drift。
- 校正 `config` 群組既有的 `Short` 與「no write/use/delete subcommands」註解,使其在 `init` 掛上後仍與事實相符。
- README「Configuration」段補一句指向 `config init` 作為 canonical 範本來源,降低三處範例漂移風險。

## Non-Goals (optional)

- **不**提供任何寫入 config 檔的能力(不加 `--write`/`--force`、不建立目錄、不設定檔案權限、不做覆寫保護)。維持 `context-config` spec「CLI SHALL NOT write the configuration」的唯讀契約一字不改。
- **不**提供互動式建檔或既有 context 的新增/修改/刪除(維持無 `set-context`/`use-context`/`delete-context`)。
- `config init` 的**輸出內容不**因 `-o/--output` 而變(永遠是固定 YAML 文字);但它**不**豁免參數驗證 —— 無效的 `-o` 值仍如其他子命令般 exit 2。

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `context-config`: 新增 `config init` 印範本到 stdout 的 requirement,並明確界定它不違反既有的 no-write-config 條款;另記錄 `config --help` 須含格式範例。
- `output-format`: 新增 requirement,說明 `config init` 對合法 `-o` 一律忽略輸出固定 YAML、對無效 `-o` 仍 exit 2(與其他子命令一致)。

## Impact

- Affected specs: `context-config`, `output-format`
- Affected code:
  - New: (none)
  - Modified:
    - `internal/cmd/config.go` — 新增 `init` 子命令、補 `config` 群組 Long description、校正 Short 與註解
    - `internal/config/config.go` — 新增共用的範本 YAML const
    - `internal/cmd/config_cmd_test.go` — 新增 `config init` 行為測試與 help 內容測試
    - `README.md` — Configuration 段指向 `config init`
  - Removed: (none)
