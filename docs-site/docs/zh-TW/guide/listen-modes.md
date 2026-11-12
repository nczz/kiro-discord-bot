# 監聽模式

Listen mode 決定 Discord 訊息什麼時候會變成 agent 工作。它是避免 bot-to-bot loop 的主要安全層，同時讓單 bot 頻道保持順手。

## Parent Channel Modes

| Mode | 觸發 | Thread 行為 | 常見用途 |
| --- | --- | --- | --- |
| Full-listen | 任何一般 human message | 預設新任務開 thread | 只有一個 assistant bot 的頻道 |
| Mention-only | 真正 Discord mention 該 bot | 不開新 thread，除非重新啟用 | 共用頻道或安靜模式 |
| Automatic multi-bot mention-only | 真正 mention 特定 bot | 依 target 當下 thread setting | 多個 peer bot 都能回應的頻道 |

`/pause` 會把 parent channel 切到 mention-only 並停用新任務 thread。`/back` 會恢復 full-listen 並重新開啟新任務 thread。

## Job Reaction 狀態

Discord 訊息成為 job 後，bot 會在該來源訊息上維持恰好一個進度 reaction；inline reply、新建 task thread 與 thread follow-up 都適用。進度可能在 `🔄`、`💭`、`✨`、`🔁`、`🛠️`、`⚙️`、`🔧`、`📋` 之間輪替；進入 terminal state 時，會用 `✅`、`⚠️` 或 `❌` 取代所有已追蹤的進度 reaction。Remote A2A task ID 不會被當成本地 Discord reaction target。

Delivery failure 可能刻意在 `❌` 旁再加一個診斷提示，例如 `🔒` 表示權限不足、`🌐` 表示 transport/server failure。這種兩個 reaction 的組合是 terminal state，不代表工作仍在執行。每個 job 的 reaction transition 都會序列化；terminal reconciliation 會重試失敗操作，若 Discord 仍拒絕更新則留下 log。

## Thread 行為

每個 task thread（包含在 Discord 中手動建立的 thread）都會保留建立當下捕捉的 listen mode；之後 parent channel 的變更不會默默改寫舊 thread 的行為。

Thread agent 和 parent channel agent 彼此獨立。一個 thread 的工作不會阻塞 parent channel 或其他 thread。

## Mentions

請使用 Discord UI 產生的真正 mention。像 `@BuildBot` 這種純文字不一定會觸發 bot。當 human message 開頭有多個 peer bot mention，這些 leading mentions 會被視為 routing metadata，並從送給各 bot 的 task text 移除。

bot 也會提供結構化 mention references 給 agent，讓 final answer 可以 mention 已驗證的使用者或 peer bot，不需要猜 raw Discord ID。Peer bot reference 會依 target 限定：只有能有效查看並回應目前 channel 或 thread 的 bot，才可在使用者明確要求時收到可見通知；權限撤銷後，delivery 前會移除 stale peer references。Peer mention 不會委派工作，也不會觸發該 bot。

## Discord Channel Webhooks

Discord webhook 訊息屬於 bot-authored，預設會被忽略。頻道管理者可在 parent channel 執行 `/webhook mode:on`，允許該頻道的 tagged webhook prompt。即使開啟，webhook 內容仍必須以這個 bot 的真正 mention 開頭，例如 `<@BOT_ID>`；在頻道內執行 `/webhook mode:status` 可看到實際 tag。Webhook 訊息不會執行 bang/slash 管理指令；被接受的 webhook 內容只會當作 agent prompt text。

## Multi-bot 協作

Peer bots 會在 bot 啟動時從 guild bot members 自動發現。這份 guild-wide 結果只是一份 catalog；每個 channel 或 thread prompt 只會包含 Discord 權限允許查看並回應目前 target 的 peer。`BOT_PEERS` 只用於覆蓋名稱/role、加入 discovery 看不到的 bot，或排除某個 bot。

Bot-authored Discord 訊息一律不會成為 agent job，即使來源是已知 peer、在已完成的 thread 中明確 mention 這個 bot。Peer mention 只代表可見通知。跨 runtime 的委派、review、比較或協作必須使用 A2A task；admission policy、durable task state、deduplication、quota 與 audit 都由 A2A 負責。Tagged webhook prompt 是獨立路徑，仍必須由管理者透過 `/webhook mode:on` 明確啟用。

## Debug Listen Mode

在目標 channel 或 thread 執行 `/doctor`。它會顯示目前是 open、mention-only、由 `/back` 開放，或 automatic multi-bot mention-only，也會檢查 bot 是否能 view/respond。
