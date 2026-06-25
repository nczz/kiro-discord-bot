# 監聽模式

Listen mode 決定 Discord 訊息什麼時候會變成 agent 工作。它是避免 bot-to-bot loop 的主要安全層，同時讓單 bot 頻道保持順手。

## Parent Channel Modes

| Mode | 觸發 | Thread 行為 | 常見用途 |
| --- | --- | --- | --- |
| Full-listen | 任何一般 human message | 預設新任務開 thread | 只有一個 assistant bot 的頻道 |
| Mention-only | 真正 Discord mention 該 bot | 不開新 thread，除非重新啟用 | 共用頻道或安靜模式 |
| Automatic multi-bot mention-only | 真正 mention 特定 bot | 依 target 當下 thread setting | 多個 peer bot 都能回應的頻道 |

`/pause` 會把 parent channel 切到 mention-only 並停用新任務 thread。`/back` 會恢復 full-listen 並重新開啟新任務 thread。

## Thread 行為

每個 task thread 都可以繼續成為獨立 agent conversation。Thread 會保留建立時捕捉的 listen mode；之後 parent channel 的變更不會默默改寫舊 thread。

Thread agent 和 parent channel agent 彼此獨立。一個 thread 的工作不會阻塞 parent channel 或其他 thread。

## Mentions

請使用 Discord UI 產生的真正 mention。像 `@BuildBot` 這種純文字不一定會觸發 bot。當 human message 開頭有多個 peer bot mention，這些 leading mentions 會被視為 routing metadata，並從送給各 bot 的 task text 移除。

bot 也會提供結構化 mention references 給 agent，讓 final answer 可以 mention 已驗證的使用者或 peer bot，不需要猜 raw Discord ID。

## Multi-bot Handoff

Peer bots 會在 bot 啟動時從 guild bot members 自動發現。`BOT_PEERS` 只用於覆蓋名稱/role、加入 discovery 看不到的 bot，或排除某個 bot。

Bot-authored messages 預設會被忽略。Peer handoff 只在受控情境接受，例如 thread 中 target bot 被明確 mention，且原始任務已完成。這可以避免 agent 回應進度更新或半成品 tool output。

## Debug Listen Mode

在目標 channel 或 thread 執行 `/doctor`。它會顯示目前是 open、mention-only、由 `/back` 開放，或 automatic multi-bot mention-only，也會檢查 bot 是否能 view/respond。
