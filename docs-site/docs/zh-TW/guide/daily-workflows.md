# 日常工作流

這一頁整理一般使用者與 channel owner 常見操作模式。

## 在工作頻道提問

把部門或專案頻道當成主要工作空間。Bot 會在該處累積 context、使用該 channel CWD，並套用該 channel 的 MCP policy。

可以 mention bot，或依照設定的 listen mode 互動。Thread mode 開啟時，較大的任務會移到任務討論串，讓 parent channel 保持可讀。

## 保存長期指引

用 `/memory` 管理輕量 Discord-native rules。只要規則出現在 `/memory list`，就會影響未來 turns。

用 `/flashmemory` 管理暫時性強調，不應成為長期專案行為。

用 `/steering create` 與 `/steering edit` 管理應該跟著 repository 走的專案指引，檔案位於 `.kiro/steering/`。

## 清理過期 Context

如果 persistent memory rule 錯誤或過期，先移除它。如果目前 Kiro session 已經看過該規則，也要執行 `/clear` 與 `/reset`，讓後續 turns 不再受到舊注入 context 影響。

## 多 Bot 協作

部門特助模式下，讓各 bot 的 durable memory 與 channel policy 貼近自己的部門頻道。在主管或執行長頻道中，可以邀請多個 bot，分別請它們基於可存取的 channel 與被授權的 MCP tools 回報摘要、風險與後續動作。

跨頻道溝通應透過明確的 Discord MCP access、摘要、共享專案檔案或 steering files。不要假設某個 bot 能讀到另一個 bot 的私有 channel state，除非 channel、Discord permissions 與 MCP policy 都允許。

## 檢視營運狀態

用 `/usage` 查看計量工作，用 `/audit` 檢查近期 bot 與 Discord activity，用 `/doctor` 排查 local shell、launchd、systemd 或遠端主機行為不一致的問題。
