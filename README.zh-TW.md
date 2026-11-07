# 魔獸世界角色統計追蹤器 (WoW Character Stats Tracker)

這是一個自託管的《魔獸世界》（World of Warcraft）角色資訊主頁，使用官方 Battle.net API 呈現您帳號內所有角色的各項統計數據，並追蹤每週活動進度。

![WoW Stats Tracker 預覽](example.png)

## 功能特色

### 🏠 首頁 — 角色資訊主頁
- **所有角色** — 自動探索您帳號中的每一名角色
- **帳號摘要** — 總金幣、坐騎數量、寵物數量，以及聯盟／部落分佈一目了然
- **角色卡片** — 顯示頭像、等級、物品等級、種族、職業（含職業圖示）、陣營與上次遊玩時間戳記
- **金幣追蹤** — 個別角色的金幣數量與格式化後的帳號總金幣
- **排序顯示** — 角色依等級排序，同等級則依物品等級由高至低（降冪）排序
- **團隊副本進度** — 顯示各角色當前團隊副本進度（普通 / 英雄 / 傳奇）
- **地城與探究資訊** — 追蹤各角色的傳奇+（Mythic+）與探究（Delves）完成情況

### 🔒 宏偉寶庫 (`/vault`)
- **拖放式介面** — 從角色清單中拖曳角色至寶庫欄位
- **三大活動途徑** — 團隊副本（3 個欄位）、傳奇+ 地城（3 個欄位）、探究（3 個欄位）
- **狀態持久化** — 欄位分配會儲存至 `localStorage`，重新整理頁面依然保留
- **跨分頁同步** — 在多個開啟的分頁間即時同步寶庫狀態
- **視覺化反饋** — 每個已填入的欄位皆會顯示角色頭像與職業圖示

### 📋 每週重置清單 (`/roster`)
- **任務日誌風格版面** — 以可捲動清單列出每名角色的每週任務
- **任務追蹤** — 宏偉寶庫欄位、團隊副本首領、地城與探究
- **手動勾選** — 點擊即可標記任務完成；狀態持久保存在 `localStorage`
- **寶庫連動** — 拖曳至宏偉寶庫頁面的角色會自動在此勾選對應的寶庫任務
- **戰隊檢視** — 在同一個頁面中顯示所有符合資格的角色（等級 90+）

### ⚒️ 專業技能追蹤器 (`/professions`)
- **完整專業技能矩陣** — 欄為所有角色、列為所有專業技能
- **三大分頁** — 主要專業技能 / 次要專業技能 / 所有資料片版本（歷程）
- **技能進度條** — 顯示每個資料片版本的視覺化進度條，懸停時會顯示資料片名稱
- **滿點醒目提示** — 專業技能練滿時，技能條會轉為金色醒目顯示
- **所有資料片版本** — 顯示每個資料片的專業技能進度，而非僅限於當前版本
- **次要專業技能** — 烹飪、釣魚、考古學等次要專業技能分開獨立追蹤

### 🗝️ 傳奇+ 表現分析 (`/mythic-plus`)
- **地城矩陣** — 每名角色在各地下城的最佳鑰石紀錄，以限時／超時著色區分，懸停可檢視詳細場次資訊
- **傳奇+ 分數** — 當前賽季積分，採用 Blizzard 原生評分顏色顯示
- **寶庫預估** — 依據本週通關場次估算傳奇+ 宏偉寶庫欄位（第 1/4/8 場）；獎勵物品等級來自 `data/vault_ilvl.json`（佔位數值 — 可針對當前賽季進行編輯）

### 🛡️ 裝備檢視器 (`/equipment`)
- **紙娃娃檢視** — 顯示所有裝備欄位，包含品質邊框、物品等級、附魔與寶石
- **裝備檢查** — 提示缺少附魔與空的寶石插槽；附魔欄位規則可在 `data/gear_rules.json` 中設定
- **次要屬性** — 致命一擊、加速、精通與臨機應變進度條，以及主要屬性、耐力與護甲值

> 這兩個頁面均在首次造訪時延遲擷取並快取 10 分鐘（在網址後加上 `?refresh=1` 可強制重新擷取）。

### 🏆 成就 (`/achievements`)
- **角色選擇器** — 可選擇任一角色；系統會記住您上次的選擇（Blizzard 未提供全帳號成就 API，因此以角色為單位）
- **當前資料片** — 顯示當前資料片分類中的所有成就與進度條
- **差一點完成** — 當達成進度達到 80%+ 時，顯示來自其他資料片的成就
- **搜尋、篩選與釘選** — 快速尋找成就、依狀態篩選，並釘選正在努力追蹤的成就（儲存於 `localStorage`）
- **可自訂範圍** — 資料片分類名稱與 80% 門檻值設定於 `data/achievement_scope.json` 中

### 🌐 通用特性
- **安全多使用者架構** — 完整隔離並行使用者 Session；不同瀏覽器與使用者絕不共用 Token、角色摘要、快取、歷程記錄或 OAuth State
- **瀏覽器獨立安全 Session** — 透過 `wow_session_id` Cookie 追蹤 Session，具備 `HttpOnly`、`SameSite=Lax` 與 HTTPS 自動啟用 `Secure` 旗標
- **單次拋棄式 OAuth State** — 每次登入獨立生成高強度密碼學隨機 State，包含過期驗證與單次消耗機制，杜絕 CSRF 與重放攻擊
- **Battle.net 伺服器端身分驗證** — 帳號所有權直接由 Battle.net 標準 OpenID Connect UserInfo 端點使用穩定唯一的 `sub` 識別碼進行驗證，完全不採信使用者自訂輸入或遊戲角色設定檔 Fallback
- **帳號隔離歷程記錄追蹤** — PostgreSQL 歷程記錄依 Battle.net 帳號嚴格劃分，既有舊版無主資料保持獨立私密、不外洩
- **繁體中文 (zh-TW) 與英文 UI 支援** — 完整介面在地化，使用台灣魔獸世界專有名詞（宏偉寶庫、聯盟、部落、專業技能等）
- **持久化語言選擇器** — 在每個頁面皆可在 English 與繁體中文之間切換（透過 `ui_lang` Cookie 在重整與頁面跳轉間保持設定）
- **Battle.net OAuth2** — 安全登入；不儲存任何密碼
- **平行 API 擷取** — 同時平行擷取所有角色資料，確保快速載入
- **自動重新整理** — 首次登入後，頁面會自動輪詢直到角色資料載入完畢
- **一致的導航列** — 每個頁面皆具備包含標誌與顏色標記按鈕的共用頁首導航列
- **多區域支援** — 可透過環境變數設定 US、EU、KR、TW 伺服器區域

---

## 系統需求

- [Go 1.22+](https://golang.org/dl/) **或** [Docker](https://docs.docker.com/get-docker/)
- 一組 [Battle.net 開發者帳號 (Battle.net Developer Account)](https://develop.battle.net/)

---

## 步驟 1 — 建立 Battle.net API 用戶端

1. 前往 [https://develop.battle.net/access/](https://develop.battle.net/access/) 並使用您的 Battle.net 帳號登入
2. 點擊 **Create Client**
3. 填寫相關資訊：
   - **Client Name**：自訂名稱（例如 `WoW Stats Tracker`）
   - **Redirect URLs**：`http://localhost:8081/callback`
   - **Service URL**：`http://localhost:8081`
4. 點擊 **Save** — 您將會取得一組 **Client ID** 與 **Client Secret**

> ⚠️ 請妥善保管您的 Client Secret — 切勿將其提交至公開儲存庫。

---

## 步驟 2 — 設定應用程式

複製環境變數範例檔並填入您的憑證：

```bash
cp .env.example .env
```

編輯 `.env`：

```env
BLIZZARD_CLIENT_ID=your_client_id_here
BLIZZARD_CLIENT_SECRET=your_client_secret_here

REGION=eu        # us, eu, kr, tw
LOCALE=en_GB     # en_US, en_GB, de_DE, fr_FR, es_ES, etc.
# WOW_GAME_VERSION=retail  # retail (預設) 或 forever
PORT=8081

# 公開 OAuth 回呼網址（選填 — 預設為 http://localhost:<PORT>/callback）
# 適用於反向代理或正式 HTTPS 部署：
# REDIRECT_URL=https://your-domain.com/callback
# 或設定 PUBLIC_URL=https://your-domain.com

# 選用：PostgreSQL 持久化連線字串（帳號隔離歷程記錄追蹤）
# DATABASE_URL=postgres://user:password@localhost:5432/wow_stats

# Cookie 安全性（選填 — 在 HTTPS 環境或 REDIRECT_URL 以 https:// 開頭時自動啟用）
# SESSION_COOKIE_SECURE=true
```

---

## 步驟 3 — 執行應用程式

### 選項 A：Go（本機執行）

```bash
go run .
```

OAuth 使用全域端點（`oauth.battle.net`），而 `REGION` 則用來選擇遊戲資料伺服器。

### 公開回呼網址 (Public Callback URL) 與 HTTPS 部署

預設情況下，應用程式的 OAuth 回呼網址會依據連接埠自動設定為：
```text
http://localhost:<PORT>/callback
```

當透過反向代理伺服器（如 Nginx、Caddy、Cloudflare、Traefik）部署或使用 HTTPS 網域時：
1. 於環境變數中設定 `REDIRECT_URL`（或 `PUBLIC_URL`）：
   ```env
   REDIRECT_URL=https://wow.yourdomain.com/callback
   ```
2. 請務必前往 [Battle.net 開發者入口網站](https://develop.battle.net/access/)，將此完整的重新導向網址加入用戶端設定中。
3. 反向代理伺服器請轉發標準 Proxy 標頭：
   ```nginx
   proxy_set_header Host $host;
   proxy_set_header X-Real-IP $remote_addr;
   proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
   proxy_set_header X-Forwarded-Proto https;
   ```
4. **Cookie 安全性**：
   - Session Cookie（`wow_session_id`）預設包含 `HttpOnly: true` 與 `SameSite: Lax`。
   - 當處於 HTTPS 環境、反向代理轉發 `X-Forwarded-Proto: https`、或 `REDIRECT_URL` 以 `https://` 開頭時，自動加上 `Secure` 旗標。
   - 亦可透過 `SESSION_COOKIE_SECURE=true` 或 `false` 明確指定。

### 選用 PostgreSQL 歷程記錄、安全結構遷移與無主資料防護

本系統支援選用的 PostgreSQL 資料庫以記錄帳號與各角色的裝備物品等級與金幣成長歷程：
- **未設定資料庫 (`DATABASE_URL` 未填)**：應用程式以純記憶體模式正常執行，不影響核心功能，`/history` 頁面顯示未啟用資料庫之提示。
- **已設定資料庫 (`DATABASE_URL` 已填)**：
  - 應用程式啟動時會自動建立資料庫（若不存在）並執行安全結構遷移。
  - 安全結構遷移採用 `ALTER TABLE ... ADD COLUMN IF NOT EXISTS account_id TEXT;` 加入帳號欄位與索引。
  - **帳號嚴格隔離**：使用者查詢歷史快照與成長圖表時，僅能查詢並檢視由 Battle.net 驗證身分所擁有的資料。
  - **既有舊版無主資料防護**：升級前既有的歷史快照（`account_id IS NULL`）將完整保留在資料庫中，不推斷所有權、不進行指派，且絕不會外洩給任何登入之帳號。

### 持久化與伺服器重啟行為說明

- **使用者 Session 與快取（記憶體儲存）**：使用者的登入 Session、OAuth 存取權杖，以及延遲載入之快取資料（傳奇+、裝備、成就）皆儲存於記憶體中的 Session 管理器並受讀寫鎖保護。當伺服器重新啟動時：
  - 記憶體中的 Session 會被清空。
  - 使用者安全地透過 Battle.net OAuth 重新登入即可。
- **成長歷史記錄（PostgreSQL 持久化）**：儲存於 PostgreSQL 中的歷程快照（`account_snapshots` 與 `character_snapshots`）會在伺服器重啟或容器重建後永久保留。
- **介面操作狀態（用戶端 `localStorage`）**：宏偉寶庫放置狀態、每週勾選清單與釘選成就直接儲存在使用者的瀏覽器 `localStorage` 中，伺服器重啟完全不影響其內容。

### 語言與在地化設定 (UI_LOCALE 與 LOCALE)

- **UI 語言 (`UI_LOCALE` / `ui_lang` Cookie)**：控制介面文字、標題、導覽、按鈕標籤與狀態訊息。
  - 使用者可隨時透過每個頁面左上角的語言選擇器（`English` / `繁體中文`）切換語言。該設定會儲存於 `ui_lang` Cookie（`en` 或 `zh-TW`）。
  - 預設 UI 語言解析邏輯：
    1. 若在 `.env` 中明確設定 `UI_LOCALE`（例如 `UI_LOCALE=zh-TW` 或 `UI_LOCALE=en`），則優先覆寫伺服器預設值。
    2. 若未設定 `UI_LOCALE`，當 Blizzard API 的 `LOCALE=zh_TW` 時，UI 預設採用 `zh-TW`。否則，預設為英文（`en`）。
- **Blizzard API 遊戲資料 (`REGION` 與 `LOCALE`)**：
  - `LOCALE`（例如 `en_GB`、`en_US`、`zh_TW`）控制 Blizzard API 回傳的遊戲內名稱語言（物品名稱、伺服器名稱、法術與貨幣名稱）。
  - 設定 `REGION=tw` 與 `LOCALE=zh_TW` 可將應用程式設定為向台灣 Blizzard 伺服器查詢，直接以繁體中文擷取遊戲實體資料。

### 遊戲版本設定 (`WOW_GAME_VERSION`)

- **預設版本 (`WOW_GAME_VERSION=retail` 或未設定)**：使用標準《魔獸世界》（正式服/Retail）API，採用 `profile-{region}` 與 `static-{region}` 命名空間，以及基於伺服器的角色端點（`/profile/wow/character/{realm}/{name}`）。
- **WoW Forever 支援 (`WOW_GAME_VERSION=forever`)**：啟用 WoW Forever 的選用支援（適用於 `tw`、`us`、`eu`、`kr` 等地區）。
  - **命名空間**：切換為 `profile-forever-{region}` 與 `static-forever-{region}`。
  - **角色端點**：切換至無伺服器架構的端點，格式為 `/profile/wow/character/{name}-{surname}`（小寫連字號 Slug），而非傳統的 `/profile/wow/character/{realm}/{name}`。
  - **帳號與角色探索**：自動解析包含角色次要名稱（Surname/姓氏）與全區規則集（Ruleset，例如 Normal、PvP）的帳號資料負載。
  - **導覽列與頁面調整**：在 Forever 模式下，全站導覽列將自動於伺服器端隱藏正式服專屬功能連結（`/vault`、`/roster`、`/mythic-plus`），並完整保留適用功能（`/`、`/professions`、`/equipment`、`/achievements`、`/history`、`/logout`）。
  - **實驗性 API 說明**：Blizzard 的 WoW Forever API 仍屬實驗性質；未支援的端點會安全回傳錯誤或空資料，不會造成帳號探索中斷。角色通常需達到 10 級以上且登出遊戲後才會由 Blizzard API 產出詳細設定檔。

### 選項 B：Docker Compose

```bash
docker-compose up --build
```

接著在瀏覽器中開啟 [http://localhost:8081](http://localhost:8081) 並點擊 **Login with Battle.net**。

---

## 頁面與路由

| 路由 | 說明 |
|------|------|
| `/` | 首頁 — 角色資訊主頁與帳號摘要 |
| `/vault` | 宏偉寶庫追蹤器 — 拖放式每週欄位規劃工具 |
| `/roster` | 每週重置清單 — 個別角色每週任務核對清單 |
| `/professions` | 專業技能追蹤器 — 跨角色專業技能矩陣 |
| `/achievements` | 成就 — 當前資料片與進度達 80%+ 的成就 |
| `/mythic-plus` | 傳奇+ 地城矩陣、分數與寶庫預估 |
| `/equipment` | 裝備檢視器 — 裝備、附魔／寶石檢查、次要屬性、原生懸浮裝備提示 |
| `/refresh` | 強制從 Battle.net 重新擷取所有角色資料 |

---

## 運作原理

1. 點擊 **Login with Battle.net** — 系統將引導您至 Blizzard 的 OAuth 授權頁面
2. 完成授權後，應用程式會自動擷取您帳號下的所有角色
3. 針對每名角色，系統會平行呼叫多個 API 端點：
   - 公開個人資料（等級、物品等級、種族、職業、陣營、上次登入時間）
   - 媒體 API（角色頭像 / Avatar）
   - 受保護角色端點（金幣 — 需要 OAuth 授權）
   - 專業技能資料（所有資料片版本、主要與次要技能）
   - 團隊副本進度（當前團隊副本、所有難度）
   - 傳奇+ 與探究活動資料
4. 結果依等級排序顯示，同等級則依物品等級排序
5. 宏偉寶庫與每週重置清單使用 `localStorage` — 無需伺服器端資料庫

---

## 安全性與多使用者架構

- **多使用者完整隔離**：每部瀏覽器皆配發獨立的 Session。並行使用者絕不會互相共享 Token、角色統計資料、裝備與團隊副本進度快取、除錯端點或背景更新工作。
- **單次拋棄式 OAuth State 驗證**：每次登入皆會動態生成具備過期時間且僅能驗證一次的高強度隨機 State，全面杜絕 OAuth CSRF 與授權碼重放攻擊。
- **Battle.net 伺服器端身分驗證**：登入使用者的帳號歸屬（Account ID 與 BattleTag）直接經由 Battle.net 規範之 OpenID Connect UserInfo 端點（`https://oauth.battle.net/oauth/userinfo`）憑 Access Token 驗證確認其穩定唯一的 `sub` 識別碼，絕不採用用戶端傳入之非受信任參數，亦絕不 Fallback 至遊戲設定檔 API 以防識別碼命名空間混淆。
- **Session 輪換與週期性過期清理**：登入成功後立即輪換 Session 識別碼並作廢先前預先授權狀態與背景工作；伺服器端過期或被撤銷之 Session 由背景定時清理機制自動清除。
- **隔離登出與重新整理**：任何用戶端進行登出或重新整理，僅會清除與操作其自身 Session，絕不會影響其他連線使用者的狀態。
- **無主歷史資料隱私**：升級前既有之無主資料（`account_id IS NULL`）維持未指派狀態，任何登入帳號皆無法存取，確保舊資料不外洩。
- **憑證與 Token 保護**：`.env` 已列入 `.gitignore`，機密憑證由伺服器環境提供。OAuth Access Token 僅存於伺服器記憶體中。
- **安全 Cookie 設定**：Session Cookie 預設具備 `HttpOnly: true` 與 `SameSite: Lax`，且於 HTTPS 環境下自動開啟 `Secure` 旗標。

---

## 伺服器區域與語系對照表

| 區域     | 代碼 | 語系範例                                 |
|----------|------|------------------------------------------|
| 歐洲     | `eu` | `en_GB`, `de_DE`, `fr_FR`, `es_ES`, `ru_RU` |
| 美洲     | `us` | `en_US`, `es_MX`, `pt_BR`               |
| 韓國     | `kr` | `ko_KR`                                  |
| 台灣     | `tw` | `zh_TW`                                  |

---

## 常見問題與疑難排解

**"BLIZZARD_CLIENT_ID and BLIZZARD_CLIENT_SECRET are required"**
→ 請確認 `.env` 檔案已存在，且已填入您的 API 憑證。

**登入後未顯示角色**
→ 應用程式在完成 OAuth 後會於背景擷取資料。請稍候數秒 — 頁面將會自動重新整理。

**記錄檔中出現部分角色的 404 錯誤**
→ 這些是您帳號中已刪除或更名但仍殘留於清單中的角色。此為正常現象，系統會自動略過。

**OAuth 重新導向錯誤 (OAuth redirect error)**
→ 請確保 `http://localhost:8081/callback` 已完整且精確地填寫於 Battle.net 開發者入口網站的用戶端設定中的 Redirect URLs。

**重新啟動後寶庫／重置清單狀態遺失**
→ 宏偉寶庫與每週重置清單使用瀏覽器的 `localStorage` — 清除瀏覽器資料會將其重置。重新啟動伺服器並不會影響此狀態。
