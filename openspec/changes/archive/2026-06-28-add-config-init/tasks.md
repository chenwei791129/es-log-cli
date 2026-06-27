## 1. 範本常數(單一來源)

- [x] 1.1 在 `internal/config/config.go` 新增 exported const(例如 `TemplateYAML`),內容為一份註解式 YAML:頂層 `contexts:` 清單,含一個 `auth.type: apikey`(用 `api-key: ${...}`)context 與一個 `auth.type: basic`(用 `username` + `password: ${...}`)context,並示範 `tls.ca-cert` / `tls.insecure-skip-verify`。完成時:該字串能被 `config.Load`/`yaml.Unmarshal` 解析成 `Config`,且解析後 apikey context 的 `Auth.APIKey`、basic context 的 `Auth.Password` 為非空的 `${...}` 字串(證明 YAML key 與 struct tag 對齊、未被靜默丟棄)。help text 與 `config init` 皆引用此同一 const,不得各自硬編一份。

## 2. config 命令

- [x] 2.1 實作 requirement「Scaffold a config template to stdout」:在 `internal/cmd/config.go` 新增 `newConfigInitCommand`,`Use: "init"`、`Args: cobra.NoArgs`。RunE 先呼叫 `opts.formatFor("config")` 驗證 `-o`(無效值回傳 exit 2 錯誤,與 `config view`/`ls`/`search` 同一路徑),驗證通過後忽略其值,將 `config.TemplateYAML` 原樣寫到 `cmd.OutOrStdout()` 並回傳 nil(exit 0)。不得讀取 `config.ResolvePath`/不得 `os.ReadFile`/不得 `os.WriteFile`/不得建立目錄;不註冊 `--write`/`--force` 或任何寫檔 flag。於 `newConfigCommand` 以 `cmd.AddCommand` 掛上。完成時:`es-log config init` 印出範本、exit 0,且在沒有 config 檔存在時仍成功。
- [x] 2.2 實作 requirement「Config help documents the file format」:在 `newConfigCommand` 的 `config` 群組補 `Long` description:說明預設路徑 `~/.config/es-log/config.yaml`、`$XDG_CONFIG_HOME/es-log/config.yaml` 形式、`--config` 與 `$ES_LOG_CONFIG` 覆寫順序,並內嵌 `config.TemplateYAML` 的 `contexts:` 範例。範例文字必須直接引用該 const(例如以 `fmt.Sprintf` 將整段 const 嵌入 Long),不得手抄節錄,以維持單一來源、避免與 `config init` 輸出 drift。完成時:`es-log config --help` 同時出現預設路徑、`$ES_LOG_CONFIG` 字樣與 `contexts:` 範例片段。
- [x] 2.3 校正 `config` 群組的既有不變式描述:`internal/cmd/config.go` 中 `config` 群組的 `Short`(現為 "Inspect configuration (read-only)")與 `newConfigCommand` 上方註解(現述「read-only inspection only; there are deliberately no write/use/delete subcommands」)在 `init` 掛上後已不精確(`init` 是 generator 而非 inspector)。更新 `Short` 與該註解,使其涵蓋「inspect + scaffold,但仍不寫入既有設定、無 set/use/delete」的事實。完成時:`config --help` 的 Short 與原始碼註解皆與實際子命令集一致,且不再宣稱「沒有非 inspector 子命令」。

## 3. 測試

- [x] 3.1 驗證 requirement「Scaffold a config template to stdout」與「Config init emits a fixed template regardless of output format」:在 `internal/cmd/config_cmd_test.go` 新增 `config init` 行為測試(僅測本 repo 擁有的邏輯,不測 cobra flag 解析):(a) 執行後將 stdout 經 `config.Load` 載入,斷言含 apikey 與 basic 兩 context,且 apikey context 的 `Auth.APIKey`、basic context 的 `Auth.Password` 為非空 `${...}` 值(欄位層級斷言,抓得到 key 拼錯);(b) 在指向不存在路徑的 `--config` 下仍 exit 0 且該檔未被建立;(c) `config init -o bogus` 走 `formatFor` 驗證、exit 2,而 `config init -o json` 與無 `-o` 輸出完全相同。斷言不依賴原始碼行號。
- [x] 3.2 驗證 requirement「Config help documents the file format」:在 `internal/cmd/config_cmd_test.go` 新增 help 內容測試,執行 `config --help`(或檢視 `config` 命令的 `Long`),斷言輸出含預設路徑 `~/.config/es-log/config.yaml`、`ES_LOG_CONFIG` 與 `contexts:` 範例字樣。

## 4. 文件

- [x] 4.1 更新 `README.md` 的「Configuration」段:加入一句指向 `es-log config init` 作為取得範本 config 的官方途徑(降低 README 範例與 `config.TemplateYAML` 各自漂移的風險)。README 仍保留人類可讀的簡短範例,但明示 `config init` 為 canonical 範本來源。

## 5. 驗證

- [x] 5.1 跑 `make test`、`make lint`、`make fmt` 全綠;`make build` 後執行 `./es-log config init > "$TMPDIR/es-log-init.yaml" && ./es-log config view --config "$TMPDIR/es-log-init.yaml"` 確認範本寫入暫存檔後可被 `config view` 解析載入(避免 `/dev/stdin` 在 macOS 上讀取不穩;欄位層級的 round-trip 由 3.1 單元測試保證,本步僅作 parse 煙霧測試)。
