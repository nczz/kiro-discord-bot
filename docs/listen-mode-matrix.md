# Listen Mode 操作模式矩陣

本文件記錄 thread mode、pause/back、multi-bot 等狀態的組合下，使用者訊息的 UX 結果。

## 核心狀態變數

| 狀態 | 儲存位置 | 預設值 | 控制指令 |
|------|---------|--------|---------|
| `threadMode[channelID]` | `thread_modes.json` | `true` | `/thread on/off` |
| `paused[channelID]` | `listen_modes.json` | `false` | `/pause`, `/back` |
| `threadListen[threadID]` | `thread_modes.json` | 建立時 snapshot | thread 內 `/pause`, `/back` |
| multi-bot mode | runtime 偵測 | peer 存在即啟動 | — |

## 指令副作用（隱式連動）

| 指令 | 在 channel 執行 | 在 thread 執行 |
|------|----------------|---------------|
| `/pause` | `paused[ch]=true` **+ `threadMode[ch]=false`** | `paused[thread]=true`（僅該 thread） |
| `/back` | `paused[ch]=false` **+ `threadMode[ch]=true`** | `paused[thread]=false`（僅該 thread） |
| `/thread on` | `threadMode[ch]=true` | — |
| `/thread off` | `threadMode[ch]=false` | — |

> ⚠️ `/pause` 在 channel 會同時關掉 thread mode，`/back` 會同時打開 thread mode。暫停 = 安靜模式（inline + mention-only）。

## Channel 普通訊息（非指令、非 mention）

| threadMode | paused | multi-bot | 結果 |
|:----------:|:------:|:---------:|------|
| ✅ on | ❌ off | ❌ 無 | 建立新 thread → agent 回覆 |
| ✅ on | ❌ off | ✅ 有 | **忽略**（multi-bot = mention-only） |
| ✅ on | ✅ on | — | **忽略**（paused = mention-only） |
| ❌ off | ❌ off | — | **忽略**（threadMode off = channel 層 mention-only） |
| ❌ off | ✅ on | — | **忽略** |

## Channel @mention

| threadMode | paused | multi-bot | 結果 |
|:----------:|:------:|:---------:|------|
| ✅ on | — | — | 建立新 thread → agent 回覆 |
| ❌ off | — | — | **Inline 回覆**（DeliveryInline） |

> mention 穿透所有 listen mode 限制。threadMode 決定回覆形式。

## Thread 內普通訊息

| thread listen | parent paused | parent threadMode | multi-bot | 結果 |
|:-------------:|:------------:|:-----------------:|:---------:|------|
| `full` | — | — | — | ✅ agent 回覆 |
| `mention` | — | — | — | **忽略** |
| 無設定 | ❌ off | ✅ on | ❌ 無 | ✅ agent 回覆 |
| 無設定 | ✅ on | ❌ off* | — | **忽略** |
| 無設定 | ❌ off | ❌ off | — | **忽略** |
| 無設定 | — | — | ✅ 有 | 需 mention（除非 parent 有 FullListenOverride） |

> \* 正常操作下 `/pause` 必然連動 `threadMode=false`，所以 `paused=true + threadMode=on` 不會透過 bot 指令產生。

> Thread 內 `/pause`/`/back` 建立本地 override，不再繼承 parent。

## Thread 內 @mention

一律回覆（mention 穿透所有 gate）。

## Cron 排程

| 狀態 | 影響 |
|------|------|
| channel 未初始化 | ❌ 拒絕執行 |
| channel paused | ✅ 正常執行（不受 pause 影響） |
| threadMode on/off | ✅ 不影響（cron 建自己的 thread） |
| silent mode | 影響失敗通知可見性 |

## Decision Flow

```
收到訊息
 │
 ├─ bot 自身 / webhook → 丟棄
 ├─ 空內容 → 丟棄
 │
 ├─ 在 thread 中?
 │   ├─ !command → 執行指令
 │   ├─ @mention → 一律處理
 │   └─ 普通訊息
 │       ├─ HasMentionOnlyOverride(thread) → 忽略
 │       ├─ HasFullListenOverride(thread) → 處理
 │       ├─ ThreadListenSnapshot 有值 → 依值
 │       ├─ ThreadMentionOnly → 忽略
 │       │   (查 threadListen → paused[thread] → threadMode[parent])
 │       ├─ multi-bot? → 查 parent override
 │       └─ IsPaused(thread) → 忽略 (safety net)
 │
 └─ 在 channel 中
     ├─ !command → 執行指令
     ├─ @mention → 處理（threadMode 決定 thread/inline）
     └─ 普通訊息
         ├─ requiresHumanMention? → 忽略
         │   (HasMentionOnlyOverride / threadMode=off / multi-bot)
         ├─ IsPaused? → 忽略 (safety net)
         └─ Enqueue job
              threadMode=on → DeliveryThread
              threadMode=off → DeliveryInline
```

## Thread Listen Mode 生命週期

```
Thread 建立
 │
 ├─ handler.go:683 計算 requiresHumanMention(channelID, "", selfID)
 │   → 結果作為 job.ThreadMentionOnly
 │
 ├─ worker.go:440 呼叫 OnThreadCreated(threadID, mentionOnly)
 │   → manager.SetThreadListenMode(threadID, mentionOnly)
 │   → 寫入 threadListen[threadID] = "mention" 或 "full"
 │
 └─ Snapshot 完成（此後不再自動同步 parent 狀態）
```

**關鍵行為：**
- Thread 建立瞬間 snapshot 當時的 listen 狀態
- Parent 之後的 /pause、/back、/thread 變更**不會**傳播到已建立的 thread
- Thread 內 /pause /back 建立 local override，覆蓋 snapshot
- 已建立的 thread 只能透過 thread 內操作改變 listen mode

## Log Reason 對照表

`handler.go` 中訊息被忽略時記錄的 log reason 與實際觸發條件：

| log reason | 觸發條件 | 修復方式 |
|-----------|---------|---------|
| `paused` | HasMentionOnlyOverride=true（channel/thread 被 /pause） | `/back` 或 @mention |
| `thread_mode_off` | ThreadModeEnabled=false（channel threadMode off） | `/thread on` 或 @mention |
| `thread_snapshot_mention` | ThreadListenSnapshot=mention（thread 建立時 snapshot 為 mention-only） | thread 內 `/back` 或 @mention |
| `thread_inherit` | ThreadMentionOnly=true（未知 thread 繼承 parent threadMode off） | parent `/thread on`、thread 內 `/back` 或 @mention |
| `multi_bot_parent_paused` | parent channel 被 /pause，且 thread 內有 peer bot 存在 | parent `/back`、thread 內 `/back` 或 @mention |
| `multi_bot` | channelMultiBotMode=true（有 peer bot 存在） | @mention 或 `/back` |
| *(無 log)* | IsPaused(channelID)=true（handler.go:506 safety net） | `/back` |
| `other_peer_mentioned` | 訊息 mention 了其他 bot 而非自己 | mention 正確的 bot |
| `self_mentioned_as_task_target` | mention 自己但語意判定為指派 | 改寫訊息 |

> ⚠️ `requiresHumanMention` 回傳 `(bool, reason)` 後，log 會保留實際 gating reason，方便對照當下狀態。

## 排查「訊息沒回應」的 Debug SOP

```
1. 確認 channel 已初始化
   → !status → 看是否有 agent

2. 看 log 有無 "ignored human msg reason="
   → 有 → 對照上方 Log Reason 表
   → 無 → 繼續

3. 確認 listen mode 狀態
   → !thread → 看 on/off
   → !status → 看 paused 狀態

4. 確認 multi-bot 環境
   → channel 裡有其他 bot 存在？
   → 有 → 預設 mention-only

5. Thread 專用
   → thread 建立時的 snapshot 可能與 parent 現狀不同
   → thread 內 !back 可強制解除

6. 確認沒有被其他 gate 攔住
   → other_peer_mentioned: 訊息提到了其他 bot
   → self_mentioned_as_task_target: mention 自己但被判為指派

7. 仍無法定位
   → /doctor → 看 listen mode 一致性檢查
   → 檢查 listen_modes.json + thread_modes.json 原始資料
```

## 常見情境

| 情境 | 建議設定 | UX |
|------|---------|-----|
| 單 bot、全自動 | threadMode=on, back | 每則訊息開 thread |
| 單 bot、偶爾問 | threadMode=off, back | @mention 才 inline 回覆 |
| 多 bot 共存 | 自動偵測 | mention-only |
| 暫時安靜 | /pause | mention-only + 不開新 thread |
| thread 獨立安靜 | thread 內 /pause | 僅該 thread 需 mention |
| 定時報告 | /cron | 不受 pause/threadMode 影響 |

---

**相關程式碼：** `bot/handler.go` (handleMessage), `bot/peers.go` (requiresHumanMention), `channel/manager.go` (ThreadMentionOnly, Pause, Back, SetThreadMode, SetThreadListenMode), `channel/worker.go` (execute/executeInline, OnThreadCreated)
