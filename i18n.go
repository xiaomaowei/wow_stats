package main

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
)

// Supported UI languages
const (
	LangEn     = "en"
	LangZhTW   = "zh-TW"
	cookieName = "ui_lang"
)

// Global default UI language resolved at startup
var (
	defaultUILang = LangEn
	uiLangMu      sync.RWMutex
)

// InitUILanguage determines default UI language from UI_LOCALE, LOCALE, REGION.
// Priority:
// 1. Explicit UI_LOCALE ("zh_TW", "zh-TW", "zh", "tw" -> zh-TW; "en", "en_US", "en_GB" -> en)
// 2. If API LOCALE is zh_TW (or case-insensitive zh-tw, zh_tw) -> zh-TW
// 3. Otherwise default to en
func InitUILanguage(apiLocale, uiLocaleEnv string) {
	uiLangMu.Lock()
	defer uiLangMu.Unlock()

	cleanUI := normalizeLangCode(uiLocaleEnv)
	if cleanUI != "" {
		defaultUILang = cleanUI
		return
	}

	cleanAPI := strings.ToLower(strings.TrimSpace(apiLocale))
	if cleanAPI == "zh_tw" || cleanAPI == "zh-tw" || cleanAPI == "zh" {
		defaultUILang = LangZhTW
	} else {
		defaultUILang = LangEn
	}
}

// GetDefaultUILang returns the configured fallback UI language
func GetDefaultUILang() string {
	uiLangMu.RLock()
	defer uiLangMu.RUnlock()
	return defaultUILang
}

// normalizeLangCode normalizes arbitrary string into "en", "zh-TW", or ""
func normalizeLangCode(val string) string {
	s := strings.ToLower(strings.TrimSpace(val))
	switch s {
	case "zh-tw", "zh_tw", "zhtw", "zh", "tw", "traditional_chinese":
		return LangZhTW
	case "en", "en_us", "en-us", "en_gb", "en-gb", "english":
		return LangEn
	default:
		return ""
	}
}

// ResolveLanguage inspects request cookie and query param (?lang=) without mutating globals.
// Validates unknown language and falls back to defaultUILang.
func ResolveLanguage(r *http.Request) string {
	// Query param override (e.g. ?lang=zh-TW or ?lang=en)
	if r != nil {
		if q := r.URL.Query().Get("lang"); q != "" {
			if norm := normalizeLangCode(q); norm != "" {
				return norm
			}
		}
		if cookie, err := r.Cookie(cookieName); err == nil && cookie.Value != "" {
			if norm := normalizeLangCode(cookie.Value); norm != "" {
				return norm
			}
		}
	}
	return GetDefaultUILang()
}

// SetLanguageCookie sets the UI language cookie on response writer
func SetLanguageCookie(w http.ResponseWriter, lang string) {
	norm := normalizeLangCode(lang)
	if norm == "" {
		norm = GetDefaultUILang()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    norm,
		Path:     "/",
		MaxAge:   365 * 24 * 3600,
		HttpOnly: false, // Accessible to client-side JS if needed
		SameSite: http.SameSiteLaxMode,
	})
}

// cleanRedirectPath strips existing lang query parameter and validates redirect safety
func cleanRedirectPath(rawPath string) string {
	if rawPath == "" {
		return "/"
	}
	u, err := url.Parse(rawPath)
	if err != nil {
		return "/"
	}
	q := u.Query()
	q.Del("lang")
	u.RawQuery = q.Encode()
	cleaned := u.String()
	if cleaned == "" || !isSafeRedirect(cleaned) {
		return "/"
	}
	return cleaned
}

// handleSetLang handles switching language and redirecting back
func handleSetLang(w http.ResponseWriter, r *http.Request) {
	lang := r.URL.Query().Get("lang")
	norm := normalizeLangCode(lang)
	if norm == "" {
		norm = LangEn
	}
	SetLanguageCookie(w, norm)

	ref := r.URL.Query().Get("redirect")
	if ref == "" {
		ref = r.Header.Get("Referer")
	}
	if ref == "" || !isSafeRedirect(ref) {
		ref = "/"
	} else {
		ref = cleanRedirectPath(ref)
	}
	http.Redirect(w, r, ref, http.StatusSeeOther)
}

func isSafeRedirect(target string) bool {
	if strings.HasPrefix(target, "/") && !strings.HasPrefix(target, "//") {
		return true
	}
	u, err := url.Parse(target)
	if err != nil {
		return false
	}
	// relative or same host
	return u.Scheme == "" && u.Host == "" && strings.HasPrefix(u.Path, "/")
}

// Translations dictionary
var translations = map[string]map[string]string{
	// Common / Navigation
	"nav.home": {
		LangEn:   "🏠 Home",
		LangZhTW: "🏠 首頁",
	},
	"nav.vault": {
		LangEn:   "Great Vault",
		LangZhTW: "宏偉寶庫",
	},
	"nav.roster": {
		LangEn:   "Weekly Roster",
		LangZhTW: "每週重置清單",
	},
	"nav.professions": {
		LangEn:   "Professions",
		LangZhTW: "專業技能",
	},
	"nav.mythicplus": {
		LangEn:   "Mythic+",
		LangZhTW: "傳奇+",
	},
	"nav.equipment": {
		LangEn:   "🛡️ Equipment",
		LangZhTW: "🛡️ 裝備檢視",
	},
	"nav.achievements": {
		LangEn:   "🏆 Achievements",
		LangZhTW: "🏆 成就",
	},
	"nav.history": {
		LangEn:   "History",
		LangZhTW: "歷程記錄",
	},
	"nav.logout": {
		LangEn:   "🔓 Re-authenticate",
		LangZhTW: "🔓 重新驗證",
	},
	"github.view": {
		LangEn:   "View on GitHub",
		LangZhTW: "在 GitHub 上檢視",
	},

	// Common Notice / Status / Refresh
	"notice.not_logged_in_title": {
		LangEn:   "Not logged in",
		LangZhTW: "尚未登入",
	},
	"notice.not_logged_in_desc": {
		LangEn:   "Please log in with Battle.net first.",
		LangZhTW: "請先透過 Battle.net 登入。",
	},
	"notice.login_link": {
		LangEn:   "log in with Battle.net",
		LangZhTW: "透過 Battle.net 登入",
	},
	"notice.loading_title": {
		LangEn:   "Loading…",
		LangZhTW: "載入中…",
	},
	"notice.data_refreshed_at": {
		LangEn:   "Data refreshed at %s · cached for 10 minutes · ",
		LangZhTW: "資料重新整理於 %s · 快取 10 分鐘 · ",
	},
	"notice.refreshed_at": {
		LangEn:   "Refreshed at %s · cached for 10 minutes · ",
		LangZhTW: "重新整理於 %s · 快取 10 分鐘 · ",
	},
	"notice.force_refresh": {
		LangEn:   "force refresh",
		LangZhTW: "強制重新整理",
	},
	"notice.try_refreshing": {
		LangEn:   "refreshing",
		LangZhTW: "重新整理",
	},
	"notice.try_again": {
		LangEn:   "Try again",
		LangZhTW: "再試一次",
	},

	// Home Page
	"home.title": {
		LangEn:   "WoW Character Stats",
		LangZhTW: "魔獸世界角色統計",
	},
	"home.last_updated": {
		LangEn:   "Last Updated: %s",
		LangZhTW: "最後更新：%s",
	},
	"home.welcome_title": {
		LangEn:   "Welcome to WoW Stats Tracker!",
		LangZhTW: "歡迎使用魔獸世界數據追蹤器！",
	},
	"home.welcome_desc": {
		LangEn:   "Please login with your Battle.net account to view your characters.",
		LangZhTW: "請使用您的 Battle.net 帳號登入以檢視您的角色。",
	},
	"home.login_btn": {
		LangEn:   "🔐 Login with Battle.net",
		LangZhTW: "🔐 透過 Battle.net 登入",
	},
	"home.loading_chars": {
		LangEn:   "Loading your character data from Battle.net...",
		LangZhTW: "正在從 Battle.net 載入您的角色資料...",
	},
	"home.loading_auto_refresh": {
		LangEn:   "This page will refresh automatically.",
		LangZhTW: "此頁面將會自動重新整理。",
	},
	"home.total_gold": {
		LangEn:   "Total Account Gold",
		LangZhTW: "帳號總金幣",
	},
	"home.mounts_collected": {
		LangEn:   "Mounts Collected",
		LangZhTW: "已收集坐騎",
	},
	"home.pets_collected": {
		LangEn:   "Pets Collected",
		LangZhTW: "已收集寵物",
	},
	"home.chars_by_faction": {
		LangEn:   "Characters by Faction",
		LangZhTW: "陣營角色分佈",
	},
	"home.faction_alliance": {
		LangEn:   "Alliance",
		LangZhTW: "聯盟",
	},
	"home.faction_horde": {
		LangEn:   "Horde",
		LangZhTW: "部落",
	},
	"home.below_level_10": {
		LangEn:   "Below level 10 — limited profile data",
		LangZhTW: "等級低於 10 級 — 個人資料受限",
	},
	"home.stat_level": {
		LangEn:   "Level:",
		LangZhTW: "等級：",
	},
	"home.stat_item_level": {
		LangEn:   "Item Level:",
		LangZhTW: "物品等級：",
	},
	"home.stat_gold": {
		LangEn:   "Gold:",
		LangZhTW: "金幣：",
	},
	"home.stat_last_played": {
		LangEn:   "Last Played:",
		LangZhTW: "上次遊玩：",
	},
	"home.stat_last_raid_kill": {
		LangEn:   "Last Raid Kill:",
		LangZhTW: "最新團隊首領擊殺：",
	},
	"home.professions_label": {
		LangEn:   "Professions:",
		LangZhTW: "專業技能：",
	},
	"home.no_professions": {
		LangEn:   "No professions selected",
		LangZhTW: "未選擇專業技能",
	},
	"home.btn_refresh": {
		LangEn:   "🔄 Refresh Data",
		LangZhTW: "🔄 重新整理資料",
	},
	"home.refresh_note": {
		LangEn:   "💡 Data only updates after you log out of a character in-game — Blizzard's API reflects the last logout state.",
		LangZhTW: "💡 資料只會在遊戲內角色登出後更新 — Blizzard API 反映的是最後登出時的狀態。",
	},
	"home.refresh_banner": {
		LangEn:   "⏳ Fetching latest data from Battle.net… page will update automatically.",
		LangZhTW: "⏳ 正在從 Battle.net 擷取最新資料… 頁面將會自動更新。",
	},
	"home.time.unknown": {
		LangEn:   "Unknown",
		LangZhTW: "未知",
	},
	"home.time.years_ago": {
		LangEn:   "%d years ago",
		LangZhTW: "%d 年前",
	},
	"home.time.year_ago": {
		LangEn:   "1 year ago",
		LangZhTW: "1 年前",
	},
	"home.time.months_ago": {
		LangEn:   "%d months ago",
		LangZhTW: "%d 個月前",
	},
	"home.time.month_ago": {
		LangEn:   "1 month ago",
		LangZhTW: "1 個月前",
	},
	"home.time.yesterday": {
		LangEn:   "Yesterday",
		LangZhTW: "昨天",
	},
	"home.time.just_now": {
		LangEn:   "Just now",
		LangZhTW: "剛剛",
	},
	"home.time.minute_ago": {
		LangEn:   "1 minute ago",
		LangZhTW: "1 分鐘前",
	},
	"home.time.minutes_ago": {
		LangEn:   "%d minutes ago",
		LangZhTW: "%d 分鐘前",
	},
	"home.time.hour_ago": {
		LangEn:   "1 hour ago",
		LangZhTW: "1 小時前",
	},
	"home.time.hours_ago": {
		LangEn:   "%d hours ago",
		LangZhTW: "%d 小時前",
	},
	"home.time.day_ago": {
		LangEn:   "1 day ago",
		LangZhTW: "1 天前",
	},
	"home.time.days_ago": {
		LangEn:   "%d days ago",
		LangZhTW: "%d 天前",
	},

	// Great Vault
	"vault.page_title": {
		LangEn:   "Great Vault Tracker",
		LangZhTW: "宏偉寶庫追蹤器",
	},
	"vault.subtitle": {
		LangEn:   "Drag characters onto slots to track your weekly Great Vault progress",
		LangZhTW: "將角色拖曳至格子以追蹤每週宏偉寶庫進度",
	},
	"vault.not_logged_in": {
		LangEn:   "Please %s to use the vault tracker.",
		LangZhTW: "請先 %s 以使用寶庫追蹤器。",
	},
	"vault.no_chars": {
		LangEn:   "No character data yet — go back and wait for data to load.",
		LangZhTW: "尚無角色資料 — 請返回並等待資料載入。",
	},
	"vault.your_chars": {
		LangEn:   "Your Characters",
		LangZhTW: "您的角色",
	},
	"vault.section_raids": {
		LangEn:   "Raids",
		LangZhTW: "團隊副本",
	},
	"vault.section_dungeons": {
		LangEn:   "Dungeons",
		LangZhTW: "地城",
	},
	"vault.section_world": {
		LangEn:   "World (Delves / World Activities)",
		LangZhTW: "世界（探究 / 世界活動）",
	},
	"vault.slot_1": {
		LangEn:   "Slot 1",
		LangZhTW: "欄位 1",
	},
	"vault.slot_2": {
		LangEn:   "Slot 2",
		LangZhTW: "欄位 2",
	},
	"vault.slot_3": {
		LangEn:   "Slot 3",
		LangZhTW: "欄位 3",
	},
	"vault.raid_req_1": {
		LangEn:   "Defeat 2 Midnight Season 1 Boss",
		LangZhTW: "擊敗 2 名 Midnight 第 1 季首領",
	},
	"vault.raid_req_2": {
		LangEn:   "Defeat 4 Midnight Season 1 Boss",
		LangZhTW: "擊敗 4 名 Midnight 第 1 季首領",
	},
	"vault.raid_req_3": {
		LangEn:   "Defeat 6 Midnight Season 1 Boss",
		LangZhTW: "擊敗 6 名 Midnight 第 1 季首領",
	},
	"vault.dungeon_req_1": {
		LangEn:   "Complete 1 Heroic, Mythic, or Timewalking Dungeon",
		LangZhTW: "完成 1 次英雄、傳奇或時光漫遊地城",
	},
	"vault.dungeon_req_2": {
		LangEn:   "Complete 4 Heroic, Mythic, or Timewalking Dungeon",
		LangZhTW: "完成 4 次英雄、傳奇或時光漫遊地城",
	},
	"vault.dungeon_req_3": {
		LangEn:   "Complete 8 Heroic, Mythic, or Timewalking Dungeon",
		LangZhTW: "完成 8 次英雄、傳奇或時光漫遊地城",
	},
	"vault.world_req_1": {
		LangEn:   "Complete 2 Delves or World Activities",
		LangZhTW: "完成 2 次探究或世界活動",
	},
	"vault.world_req_2": {
		LangEn:   "Complete 4 Delves or World Activities",
		LangZhTW: "完成 4 次探究或世界活動",
	},
	"vault.world_req_3": {
		LangEn:   "Complete 8 Delves or World Activities",
		LangZhTW: "完成 8 次探究或世界活動",
	},
	"vault.drop_hint": {
		LangEn:   "Drop character",
		LangZhTW: "拖曳角色至此",
	},
	"vault.clear_all": {
		LangEn:   "🗑 Clear All Slots",
		LangZhTW: "🗑 清空所有欄位",
	},
	"vault.remove_title": {
		LangEn:   "Remove",
		LangZhTW: "移除",
	},

	// Weekly Roster
	"roster.title": {
		LangEn:   "Weekly Reset Roster",
		LangZhTW: "每週重置清單",
	},
	"roster.subtitle": {
		LangEn:   "Track your weekly tasks — vault slots auto-complete when you drag characters in the Great Vault",
		LangZhTW: "追蹤您的每週目標 — 當在宏偉寶庫中分配角色時，對應的寶庫欄位將自動標記完成",
	},
	"roster.not_logged_in": {
		LangEn:   "Please %s to use the roster.",
		LangZhTW: "請先 %s 以使用重置清單。",
	},
	"roster.no_chars": {
		LangEn:   "No character data yet — go back and wait for data to load.",
		LangZhTW: "尚無角色資料 — 請返回並等待資料載入。",
	},
	"roster.objectives": {
		LangEn:   "Weekly Objectives",
		LangZhTW: "每週目標",
	},
	"roster.resets_tuesday": {
		LangEn:   "Resets every Tuesday",
		LangZhTW: "每週四伺服器維護時重置",
	},
	"roster.chars_done": {
		LangEn:   "Characters fully done:",
		LangZhTW: "已完全達成角色：",
	},
	"roster.total_tracked": {
		LangEn:   "Total tracked:",
		LangZhTW: "總追蹤數：",
	},
	"roster.profile_btn": {
		LangEn:   "Profile",
		LangZhTW: "個人英雄榜",
	},
	"roster.dungeons_btn": {
		LangEn:   "Dungeons",
		LangZhTW: "地城進度",
	},
	"roster.reset_note": {
		LangEn:   "Resets every Tuesday",
		LangZhTW: "每週二伺服器維護時重置",
	},
	"roster.btn_reset_manual": {
		LangEn:   "Clear Manual Checks",
		LangZhTW: "清除手動勾選",
	},
	"roster.done_suffix": {
		LangEn:   "done",
		LangZhTW: "已完成",
	},
	"roster.badge_vault": {
		LangEn:   "Vault ✓",
		LangZhTW: "寶庫 ✓",
	},
	"roster.confirm_reset": {
		LangEn:   "Reset all manual checkboxes for this week?",
		LangZhTW: "要重置本週所有手動勾選的任務嗎？",
	},
	"roster.task_vault_raid_1": {
		LangEn:   "Vault: Raid Slot 1",
		LangZhTW: "寶庫：團隊副本欄位 1",
	},
	"roster.task_vault_raid_2": {
		LangEn:   "Vault: Raid Slot 2",
		LangZhTW: "寶庫：團隊副本欄位 2",
	},
	"roster.task_vault_raid_3": {
		LangEn:   "Vault: Raid Slot 3",
		LangZhTW: "寶庫：團隊副本欄位 3",
	},
	"roster.task_vault_dungeon_1": {
		LangEn:   "Vault: Dungeon Slot 1",
		LangZhTW: "寶庫：地城欄位 1",
	},
	"roster.task_vault_dungeon_2": {
		LangEn:   "Vault: Dungeon Slot 2",
		LangZhTW: "寶庫：地城欄位 2",
	},
	"roster.task_vault_dungeon_3": {
		LangEn:   "Vault: Dungeon Slot 3",
		LangZhTW: "寶庫：地城欄位 3",
	},
	"roster.task_vault_world_1": {
		LangEn:   "Vault: World Slot 1",
		LangZhTW: "寶庫：世界欄位 1",
	},
	"roster.task_vault_world_2": {
		LangEn:   "Vault: World Slot 2",
		LangZhTW: "寶庫：世界欄位 2",
	},
	"roster.task_vault_world_3": {
		LangEn:   "Vault: World Slot 3",
		LangZhTW: "寶庫：世界欄位 3",
	},
	"roster.task_open_vault": {
		LangEn:   "Open your Vault",
		LangZhTW: "開啟宏偉寶庫",
	},
	"roster.task_world_event": {
		LangEn:   "World Event quest (Lady Liadrin — earn a Spark)",
		LangZhTW: "世界事件任務 (利亞德倫女士 — 取得火花)",
	},
	"roster.task_housing": {
		LangEn:   "Housing weekly (Vaeli, outside Silvermoon bank)",
		LangZhTW: "住所每週任務 (瓦莉，銀月城銀行外)",
	},
	"roster.task_world_boss": {
		LangEn:   "World Boss",
		LangZhTW: "世界首領",
	},
	"roster.task_lfr_set": {
		LangEn:   "LFR — hunt for 4-set bonus",
		LangZhTW: "團隊搜尋器 — 湊齊4件套裝獎勵",
	},
	"roster.task_mplus_weekly": {
		LangEn:   "M+ keys — farm highest possible (up to +10)",
		LangZhTW: "傳奇鑰石 — 盡可能打最高層 (最高至 +10)",
	},
	"roster.task_prey_weekly": {
		LangEn:   "Nightmare Prey ×3 (weekly quest)",
		LangZhTW: "夢魘獵物 ×3 (每週任務)",
	},
	"roster.task_delve_t11": {
		LangEn:   "Delves — at least one T11",
		LangZhTW: "探究 — 至少完成一次 11 級",
	},
	"roster.task_spend_crests": {
		LangEn:   "Spend Champion crests & below on upgrades",
		LangZhTW: "消耗勇士及以下紋章進行裝備升級",
	},
	"roster.task_tw_raid": {
		LangEn:   "Timewalking raid quest (Hero-track reward)",
		LangZhTW: "時光漫遊團隊任務 (英雄等級獎勵)",
	},
	"roster.task_tw_dungeon": {
		LangEn:   "Timewalking dungeon quest (if active)",
		LangZhTW: "時光漫遊地城任務 (若開放)",
	},
	"roster.sec_raids": {
		LangEn:   "Raids",
		LangZhTW: "團隊副本",
	},
	"roster.sec_dungeons": {
		LangEn:   "Dungeons",
		LangZhTW: "地城",
	},
	"roster.sec_world": {
		LangEn:   "World",
		LangZhTW: "世界",
	},
	"roster.sec_vault": {
		LangEn:   "Vault",
		LangZhTW: "宏偉寶庫",
	},
	"roster.sec_general": {
		LangEn:   "General",
		LangZhTW: "綜合",
	},
	"roster.sec_delves": {
		LangEn:   "Delves",
		LangZhTW: "探究",
	},
	"roster.sec_currencies": {
		LangEn:   "Currencies",
		LangZhTW: "貨幣",
	},
	"roster.sec_optional": {
		LangEn:   "Optional",
		LangZhTW: "可選任務",
	},

	"roster.clear_manual": {
		LangEn:   "🗑 Clear Manual Checks",
		LangZhTW: "🗑 清除手動勾選",
	},
	"roster.btn_mark_done": {
		LangEn:   "Mark done",
		LangZhTW: "標記完成",
	},
	"roster.btn_undo": {
		LangEn:   "✓ Done — undo?",
		LangZhTW: "✓ 已完成 — 復原？",
	},
	"roster.vault_badge": {
		LangEn:   "Vault ✓",
		LangZhTW: "寶庫 ✓",
	},
	"roster.profile_link": {
		LangEn:   "Profile",
		LangZhTW: "個人英雄榜",
	},
	"roster.dungeons_link": {
		LangEn:   "Dungeons",
		LangZhTW: "地城進度",
	},
	// Roster Tasks
	"roster.task.raids-0":           {LangEn: "Vault: Raid Slot 1", LangZhTW: "寶庫：團隊副本欄位 1"},
	"roster.task.raids-1":           {LangEn: "Vault: Raid Slot 2", LangZhTW: "寶庫：團隊副本欄位 2"},
	"roster.task.raids-2":           {LangEn: "Vault: Raid Slot 3", LangZhTW: "寶庫：團隊副本欄位 3"},
	"roster.task.dungeons-0":        {LangEn: "Vault: Dungeon Slot 1", LangZhTW: "寶庫：地城欄位 1"},
	"roster.task.dungeons-1":        {LangEn: "Vault: Dungeon Slot 2", LangZhTW: "寶庫：地城欄位 2"},
	"roster.task.dungeons-2":        {LangEn: "Vault: Dungeon Slot 3", LangZhTW: "寶庫：地城欄位 3"},
	"roster.task.world-0":           {LangEn: "Vault: World Slot 1", LangZhTW: "寶庫：世界欄位 1"},
	"roster.task.world-1":           {LangEn: "Vault: World Slot 2", LangZhTW: "寶庫：世界欄位 2"},
	"roster.task.world-2":           {LangEn: "Vault: World Slot 3", LangZhTW: "寶庫：世界欄位 3"},
	"roster.task.open-vault":        {LangEn: "Open your Vault", LangZhTW: "領取宏偉寶庫獎勵"},
	"roster.task.world-event-quest": {LangEn: "World Event quest (Lady Liadrin — earn a Spark)", LangZhTW: "世界事件任務（莉雅德琳女士 — 獲取火花）"},
	"roster.task.housing-quest":     {LangEn: "Housing weekly (Vaeli, outside Silvermoon bank)", LangZhTW: "每週居所任務（瓦莉，銀月城銀行外）"},
	"roster.task.world-boss":        {LangEn: "World Boss", LangZhTW: "世界首領"},
	"roster.task.lfr-4set":          {LangEn: "LFR — hunt for 4-set bonus", LangZhTW: "團隊搜尋器 — 收集 4 件套裝加成"},
	"roster.task.mplus-weekly":      {LangEn: "M+ keys — farm highest possible (up to +10)", LangZhTW: "傳奇+ 鑰石 — 盡量打最高層（最高至 +10）"},
	"roster.task.prey-weekly":       {LangEn: "Nightmare Prey ×3 (weekly quest)", LangZhTW: "夢魘獵物 ×3（每週任務）"},
	"roster.task.delve-t11":         {LangEn: "Delves — at least one T11", LangZhTW: "探究 — 至少完成一次 11 層"},
	"roster.task.spend-crests":      {LangEn: "Spend Champion crests & below on upgrades", LangZhTW: "消耗勇士（含）以下紋章升級裝備"},
	"roster.task.timewalking-raid":  {LangEn: "Timewalking raid quest (Hero-track reward)", LangZhTW: "時光漫遊團隊副本任務（英雄級獎勵）"},
	"roster.task.timewalking-dung":  {LangEn: "Timewalking dungeon quest (if active)", LangZhTW: "時光漫遊地城週任（若開放）"},
	"roster.sec.raids":              {LangEn: "Raids", LangZhTW: "團隊副本"},
	"roster.sec.dungeons":           {LangEn: "Dungeons", LangZhTW: "地城"},
	"roster.sec.world":              {LangEn: "World", LangZhTW: "世界"},
	"roster.sec.vault":              {LangEn: "Vault", LangZhTW: "寶庫"},
	"roster.sec.general":            {LangEn: "General", LangZhTW: "一般"},
	"roster.sec.delves":             {LangEn: "Delves", LangZhTW: "探究"},
	"roster.sec.currencies":         {LangEn: "Currencies", LangZhTW: "通貨"},
	"roster.sec.optional":           {LangEn: "Optional", LangZhTW: "選用"},

	// Professions
	"prof.title": {
		LangEn:   "Profession Tracker",
		LangZhTW: "專業技能追蹤器",
	},
	"prof.subtitle": {
		LangEn:   "Primary & secondary professions across your Warband",
		LangZhTW: "戰隊全角色的主要與次要專業技能",
	},
	"prof.tab_primary": {
		LangEn:   "Primary Professions",
		LangZhTW: "主要專業技能",
	},
	"prof.tab_secondary": {
		LangEn:   "Secondary Professions",
		LangZhTW: "次要專業技能",
	},
	"prof.tab_all": {
		LangEn:   "All Tiers (History)",
		LangZhTW: "所有資料片版本（歷程）",
	},
	"prof.th_character": {
		LangEn:   "Character",
		LangZhTW: "角色",
	},
	"prof.learned": {
		LangEn:   "learned",
		LangZhTW: "已習得",
	},

	// History
	"history.page_title": {
		LangEn:   "Progression History",
		LangZhTW: "歷程進度記錄",
	},
	"history.subtitle": {
		LangEn:   "Gold & item level trends over time",
		LangZhTW: "金幣與物品等級趨勢變化",
	},
	"history.db_not_configured_title": {
		LangEn:   "Database Not Configured",
		LangZhTW: "尚未設定資料庫",
	},
	"history.db_not_configured_p1": {
		LangEn:   "History tracking is optional. To enable it, add the following to your .env file and restart the app:",
		LangZhTW: "歷程追蹤功能為選用項目。若要啟用，請在您的 .env 檔案中加入以下內容並重啟應用程式：",
	},
	"history.db_not_configured_p2": {
		LangEn:   "The database schema will be created automatically on first startup. Snapshots are saved every time you hit Refresh on the home page.",
		LangZhTW: "資料庫結構將於首次啟動時自動建立。每次在首頁按下「重新整理」時都會儲存快照。",
	},
	"history.no_history_title": {
		LangEn:   "No History Yet",
		LangZhTW: "尚無歷史記錄",
	},
	"history.no_history_desc": {
		LangEn:   "Database is connected! Snapshots are saved each time you refresh your character data on the home page. Come back after a couple of refreshes to see your trends here.",
		LangZhTW: "資料庫已連線！每次在首頁重新整理角色資料時都會儲存快照。多重新整理幾次後，即可在此處檢視趨勢。",
	},
	"history.account_wide": {
		LangEn:   "Account-wide",
		LangZhTW: "帳號整體",
	},
	"history.chart_gold_title": {
		LangEn:   "💰 Total Gold across all characters",
		LangZhTW: "💰 全角色總金幣",
	},
	"history.chart_collections_title": {
		LangEn:   "🐾 Collection Growth — Mounts & Pets",
		LangZhTW: "🐾 收藏成長 — 坐騎與寵物",
	},
	"history.per_char": {
		LangEn:   "Per-Character Progression",
		LangZhTW: "個別角色進度",
	},
	"history.chart_ilvl": {
		LangEn:   "⚔️ Item Level",
		LangZhTW: "⚔️ 物品等級",
	},
	"history.chart_level": {
		LangEn:   "🎖️ Character Level",
		LangZhTW: "🎖️ 角色等級",
	},
	"history.chart_gold": {
		LangEn:   "💰 Gold",
		LangZhTW: "💰 金幣",
	},
	"history.ds_total_gold": {
		LangEn:   "Total Gold",
		LangZhTW: "總金幣",
	},
	"history.ds_mounts": {
		LangEn:   "Mounts",
		LangZhTW: "坐騎",
	},
	"history.ds_pets": {
		LangEn:   "Pets",
		LangZhTW: "寵物",
	},
	"history.ds_ilvl": {
		LangEn:   "iLvl",
		LangZhTW: "物品等級",
	},
	"history.ds_level": {
		LangEn:   "Level",
		LangZhTW: "等級",
	},
	"history.ds_gold": {
		LangEn:   "Gold",
		LangZhTW: "金幣",
	},

	// Mythic+
	"mplus.title": {
		LangEn:   "Mythic+ Breakdown",
		LangZhTW: "傳奇+ 表現分析",
	},
	"mplus.subtitle": {
		LangEn:   "Best key per dungeon for the current season, across your whole roster",
		LangZhTW: "目前賽季中，戰隊所有角色各地下城的最佳鑰石紀錄",
	},
	"mplus.loading_title": {
		LangEn:   "Fetching Mythic+ data…",
		LangZhTW: "正在擷取傳奇+ 資料…",
	},
	"mplus.loading_desc": {
		LangEn:   "Loading from Battle.net — this page will refresh automatically.",
		LangZhTW: "正在從 Battle.net 載入 — 此頁面將自動重新整理。",
	},
	"mplus.no_chars_title": {
		LangEn:   "No characters found",
		LangZhTW: "找不到角色",
	},
	"mplus.no_chars_desc": {
		LangEn:   "No eligible characters. Try %s.",
		LangZhTW: "沒有符合資格的角色。請嘗試 %s。",
	},
	"mplus.th_character": {
		LangEn:   "Character",
		LangZhTW: "角色",
	},
	"mplus.th_rating": {
		LangEn:   "M+ Rating",
		LangZhTW: "傳奇+ 分數",
	},
	"mplus.vault_rewards_title": {
		LangEn:   "Great Vault — Mythic+ Rewards This Week",
		LangZhTW: "宏偉寶庫 — 本週傳奇+ 獎勵預估",
	},
	"mplus.vault_rewards_note": {
		LangEn:   "Estimated from this week's runs that Blizzard reports as best-in-dungeon. Runs that were overwritten by a higher key in the same dungeon are not visible to the API, so the real vault may be higher.",
		LangZhTW: "依據 Blizzard API 回報為各地下城最佳的本週通關紀錄估算。同一地下城被更高層紀錄覆蓋的通關無法被 API 檢視，因此實際寶庫獎勵可能更高。",
	},
	"mplus.vault_ilvl_placeholder_note": {
		LangEn:   "Reward item levels are placeholders (TWW-era values) — edit data/vault_ilvl.json to match the current season.",
		LangZhTW: "獎勵物品等級為佔位符（地心之戰數值）— 請編輯 data/vault_ilvl.json 以符合當前賽季。",
	},
	"mplus.runs_this_week": {
		LangEn:   "%d run(s) this week",
		LangZhTW: "本週完成 %d 次",
	},
	"mplus.slot_run": {
		LangEn:   "%d%s run",
		LangZhTW: "第 %d%s 次",
	},
	"mplus.slot_locked": {
		LangEn:   "locked",
		LangZhTW: "未解鎖",
	},
	"mplus.status_timed": {
		LangEn:   "Timed",
		LangZhTW: "限時完成",
	},
	"mplus.status_depleted": {
		LangEn:   "Depleted",
		LangZhTW: "超時完成",
	},
	"mplus.tooltip_rating": {
		LangEn:   "rating",
		LangZhTW: "分數",
	},
	"mplus.tooltip_in": {
		LangEn:   "in",
		LangZhTW: "用時",
	},
	"mplus.tooltip_affixes": {
		LangEn:   "affixes",
		LangZhTW: "詞綴",
	},

	// Equipment
	"equip.title": {
		LangEn:   "Equipment Inspector",
		LangZhTW: "裝備檢視器",
	},
	"equip.subtitle": {
		LangEn:   "Gear, enchants, gems and secondary stats for each character",
		LangZhTW: "每名角色的裝備、附魔、寶石與次要屬性",
	},
	"equip.loading_title": {
		LangEn:   "Fetching equipment…",
		LangZhTW: "正在擷取裝備資料…",
	},
	"equip.loading_desc": {
		LangEn:   "Loading from Battle.net — this page will refresh automatically.",
		LangZhTW: "正在從 Battle.net 載入 — 此頁面將自動重新整理。",
	},
	"equip.no_chars_title": {
		LangEn:   "No characters found",
		LangZhTW: "找不到角色",
	},
	"equip.no_chars_desc": {
		LangEn:   "Try %s.",
		LangZhTW: "嘗試 %s。",
	},
	"equip.char_meta": {
		LangEn:   "%s · Level %s · Item level %s",
		LangZhTW: "%s · 等級 %s · 物品等級 %s",
	},
	"equip.error_prefix": {
		LangEn:   "Could not load equipment: ",
		LangZhTW: "無法載入裝備：",
	},
	"equip.sec_stats_title": {
		LangEn:   "Secondary Stats",
		LangZhTW: "次要屬性",
	},
	"equip.stat_crit": {
		LangEn:   "Critical Strike",
		LangZhTW: "致命一擊",
	},
	"equip.stat_haste": {
		LangEn:   "Haste",
		LangZhTW: "加速",
	},
	"equip.stat_mastery": {
		LangEn:   "Mastery",
		LangZhTW: "精通",
	},
	"equip.stat_vers": {
		LangEn:   "Versatility",
		LangZhTW: "臨機應變",
	},
	"equip.stat_stamina": {
		LangEn:   "Stamina",
		LangZhTW: "耐力",
	},
	"equip.stat_armor": {
		LangEn:   "Armor",
		LangZhTW: "護甲值",
	},
	"equip.gear_check_title": {
		LangEn:   "Gear Check",
		LangZhTW: "裝備檢查",
	},
	"equip.gear_check_ok": {
		LangEn:   "✔ No missing enchants or empty sockets.",
		LangZhTW: "✔ 沒有缺少附魔或空的寶石插槽。",
	},
	"equip.gear_rules_note": {
		LangEn:   "Enchant rules are editable in data/gear_rules.json.",
		LangZhTW: "附魔檢查規則可在 data/gear_rules.json 中自訂。",
	},
	"equip.missing_enchant": {
		LangEn:   "⚠ No enchant",
		LangZhTW: "⚠ 未附魔",
	},
	"equip.gem_empty": {
		LangEn:   "◇ empty",
		LangZhTW: "◇ 空插槽",
	},
	"equip.slot_empty": {
		LangEn:   "%s — empty",
		LangZhTW: "%s — 空",
	},
	"equip.warn_missing_enchant": {
		LangEn:   "%s: missing enchant",
		LangZhTW: "%s：缺少附魔",
	},
	"equip.warn_empty_socket": {
		LangEn:   "%s: empty gem socket",
		LangZhTW: "%s：寶石插槽未鑲嵌",
	},

	// Slot names
	"slot.HEAD":      {LangEn: "Head", LangZhTW: "頭部"},
	"slot.NECK":      {LangEn: "Neck", LangZhTW: "項鍊"},
	"slot.SHOULDER":  {LangEn: "Shoulders", LangZhTW: "肩部"},
	"slot.BACK":      {LangEn: "Back", LangZhTW: "背部"},
	"slot.CHEST":     {LangEn: "Chest", LangZhTW: "胸部"},
	"slot.SHIRT":     {LangEn: "Shirt", LangZhTW: "襯衣"},
	"slot.TABARD":    {LangEn: "Tabard", LangZhTW: "外袍"},
	"slot.WRIST":     {LangEn: "Wrists", LangZhTW: "手腕"},
	"slot.HANDS":     {LangEn: "Hands", LangZhTW: "手套"},
	"slot.WAIST":     {LangEn: "Waist", LangZhTW: "腰帶"},
	"slot.LEGS":      {LangEn: "Legs", LangZhTW: "腿部"},
	"slot.FEET":      {LangEn: "Feet", LangZhTW: "腳部"},
	"slot.FINGER_1":  {LangEn: "Ring 1", LangZhTW: "戒指 1"},
	"slot.FINGER_2":  {LangEn: "Ring 2", LangZhTW: "戒指 2"},
	"slot.TRINKET_1": {LangEn: "Trinket 1", LangZhTW: "飾品 1"},
	"slot.TRINKET_2": {LangEn: "Trinket 2", LangZhTW: "飾品 2"},
	"slot.MAIN_HAND": {LangEn: "Main Hand", LangZhTW: "主手"},
	"slot.OFF_HAND":  {LangEn: "Off Hand", LangZhTW: "副手"},

	// Primary Stats
	"stat.Strength":  {LangEn: "Strength", LangZhTW: "力量"},
	"stat.Agility":   {LangEn: "Agility", LangZhTW: "敏捷"},
	"stat.Intellect": {LangEn: "Intellect", LangZhTW: "智力"},

	// Achievements
	"ach.title": {
		LangEn:   "Achievements",
		LangZhTW: "成就",
	},
	"ach.subtitle": {
		LangEn:   "%s achievements, plus anything else you're %d%%+ of the way through",
		LangZhTW: "%s 成就，以及其他達成進度已達 %d%%+ 的成就",
	},
	"ach.err_title": {
		LangEn:   "Couldn't load achievements",
		LangZhTW: "無法載入成就",
	},
	"ach.loading_title": {
		LangEn:   "Loading achievements…",
		LangZhTW: "正在載入成就…",
	},
	"ach.loading_desc": {
		LangEn:   "The first load fetches Blizzard's achievement catalogue and may take up to a minute. This page refreshes automatically.",
		LangZhTW: "首次載入需要擷取 Blizzard 成就目錄，可能需要一分鐘。此頁面將自動重新整理。",
	},
	"ach.stat_points": {
		LangEn:   "Achievement points",
		LangZhTW: "成就點數",
	},
	"ach.stat_overall": {
		LangEn:   "Completed overall",
		LangZhTW: "總完成數",
	},
	"ach.stat_current_xpac": {
		LangEn:   "Current expansion",
		LangZhTW: "當前資料片",
	},
	"ach.search_placeholder": {
		LangEn:   "Search achievements…",
		LangZhTW: "搜尋成就…",
	},
	"ach.filter_all": {
		LangEn:   "All",
		LangZhTW: "全部",
	},
	"ach.filter_prog": {
		LangEn:   "In progress",
		LangZhTW: "進行中",
	},
	"ach.filter_done": {
		LangEn:   "Completed",
		LangZhTW: "已完成",
	},
	"ach.filter_none": {
		LangEn:   "Not started",
		LangZhTW: "未開始",
	},
	"ach.sec_pinned": {
		LangEn:   "⭐ Pinned",
		LangZhTW: "⭐ 已釘選",
	},
	"ach.sec_almost": {
		LangEn:   "🔥 Almost there — %d%%+ (other expansions)",
		LangZhTW: "🔥 差一點完成 — %d%%+（其他資料片）",
	},
	"ach.sec_current_xpac": {
		LangEn:   "Current expansion",
		LangZhTW: "當前資料片",
	},
	"ach.sec_recent": {
		LangEn:   "Recently completed",
		LangZhTW: "最近完成",
	},
	"ach.pin_title": {
		LangEn:   "Pin",
		LangZhTW: "釘選",
	},
	"ach.no_matches_title": {
		LangEn:   "No current-expansion categories matched",
		LangZhTW: "未符合任何當前資料片分類",
	},
	"ach.no_matches_desc": {
		LangEn:   "No achievement category is named “%s”. Edit data/achievement_scope.json to set the right category name(s).",
		LangZhTW: "沒有名稱為「%s」的成就分類。請編輯 data/achievement_scope.json 設定正確的分類名稱。",
	},
	"ach.footer_note": {
		LangEn:   "Refreshed at %s · cached for 10 minutes · %s · scope is editable in data/achievement_scope.json",
		LangZhTW: "重新整理於 %s · 快取 10 分鐘 · %s · 範圍可在 data/achievement_scope.json 中修改",
	},
	"ach.js_loading": {
		LangEn:   "Loading…",
		LangZhTW: "載入中…",
	},
	"ach.js_no_description": {
		LangEn:   "No description",
		LangZhTW: "無說明",
	},
	"ach.js_pts": {
		LangEn:   "pts",
		LangZhTW: "點",
	},
	"ach.js_error": {
		LangEn:   "Could not load details.",
		LangZhTW: "無法載入詳細資訊。",
	},

	// Player Currency
	"currency.title": {
		LangEn:   "Player Currency",
		LangZhTW: "角色通貨一覽",
	},
	"currency.desc": {
		LangEn:   "This page displays selected currencies per-character (requires you to be logged in).",
		LangZhTW: "此頁面顯示各角色的指定通貨數量（需要先登入）。",
	},
	"currency.login_prompt": {
		LangEn:   "Please %s to view per-character currencies.",
		LangZhTW: "請先 %s 以檢視各角色的通貨。",
	},
	"currency.login_link": {
		LangEn:   "log in",
		LangZhTW: "登入",
	},
	"currency.th_character": {
		LangEn:   "Character",
		LangZhTW: "角色",
	},
	"currency.no_data": {
		LangEn:   "No currency data (not authenticated or API error)",
		LangZhTW: "無通貨資料（尚未驗證或 API 錯誤）",
	},

	// Language Switcher
	"lang.switch_to_en": {
		LangEn:   "English",
		LangZhTW: "English",
	},
	"lang.switch_to_zhtw": {
		LangEn:   "繁體中文",
		LangZhTW: "繁體中文",
	},
	"lang.aria_label": {
		LangEn:   "Language Selector",
		LangZhTW: "語言切換",
	},
}

// T returns the translated string for the given key and language.
// Falls back to English if missing in target language, or key itself if missing completely.
func T(lang, key string) string {
	if dict, ok := translations[key]; ok {
		if val, has := dict[lang]; has && val != "" {
			return val
		}
		if val, has := dict[LangEn]; has && val != "" {
			return val
		}
	}
	return key
}

// TFmt returns formatted translated string
func TFmt(lang, key string, args ...interface{}) string {
	pattern := T(lang, key)
	return fmt.Sprintf(pattern, args...)
}

// SlotLabel returns localized slot label
func SlotLabel(lang, slotType string) string {
	k := "slot." + slotType
	if dict, ok := translations[k]; ok {
		if val, has := dict[lang]; has && val != "" {
			return val
		}
	}
	if l, ok := slotLabels[slotType]; ok {
		return l
	}
	return slotType
}

// StatName returns localized primary stat name
func StatName(lang, rawName string) string {
	k := "stat." + rawName
	return T(lang, k)
}

// LanguageSelectorHTML generates the shared language switch buttons
func LanguageSelectorHTML(currentLang, currentPath string) template.HTML {
	// Destination URL keeps path, or uses /set-lang?lang=...&redirect=...
	safeRedirect := url.QueryEscape(cleanRedirectPath(currentPath))
	if safeRedirect == "" {
		safeRedirect = "%2F"
	}

	enClass := "lang-btn"
	zhClass := "lang-btn"
	if currentLang == LangZhTW {
		zhClass += " active"
	} else {
		enClass += " active"
	}

	html := fmt.Sprintf(`<div class="header-lang-selector" aria-label="%s">`+
		`<a href="/set-lang?lang=en&redirect=%s" class="%s" title="Switch to English">English</a>`+
		`<span class="lang-sep">|</span>`+
		`<a href="/set-lang?lang=zh-TW&redirect=%s" class="%s" title="切換為繁體中文">繁體中文</a>`+
		`</div>`,
		template.HTMLEscapeString(T(currentLang, "lang.aria_label")),
		safeRedirect, enClass,
		safeRedirect, zhClass,
	)
	return template.HTML(html)
}

// NavHTML returns the navigation menu buttons localized for current language
func NavHTML(lang string) string {
	return fmt.Sprintf(`<div class="header-nav-btns">
    <a href="/" class="nav-btn">%s</a>
    <a href="/vault" class="nav-btn nav-btn-vault"><img src="/images/vault-button.png" style="width:20px;height:20px;flex-shrink:0;filter:drop-shadow(0 1px 3px rgba(0,0,0,0.6));" alt="">%s</a>
    <a href="/roster" class="nav-btn nav-btn-roster"><img src="/images/quest.png" style="width:20px;height:20px;flex-shrink:0;filter:drop-shadow(0 1px 3px rgba(0,0,0,0.6));" alt="">%s</a>
    <a href="/professions" class="nav-btn nav-btn-prof"><img src="/images/professions.png" style="width:20px;height:20px;flex-shrink:0;filter:drop-shadow(0 1px 3px rgba(0,0,0,0.6));" alt="">%s</a>
    <a href="/mythic-plus" class="nav-btn nav-btn-mplus"><img src="/images/dungeon.png" style="width:20px;height:20px;flex-shrink:0;filter:drop-shadow(0 1px 3px rgba(0,0,0,0.6));" alt="">%s</a>
    <a href="/equipment" class="nav-btn nav-btn-gear">%s</a>
    <a href="/achievements" class="nav-btn nav-btn-ach">%s</a>
    <a href="/history" class="nav-btn nav-btn-history"><img src="/images/progress.png" style="width:20px;height:20px;flex-shrink:0;filter:drop-shadow(0 1px 3px rgba(0,0,0,0.6));" alt="">%s</a>
    <a href="/logout" class="nav-btn nav-btn-logout">%s</a>
  </div>`,
		template.HTMLEscapeString(T(lang, "nav.home")),
		template.HTMLEscapeString(T(lang, "nav.vault")),
		template.HTMLEscapeString(T(lang, "nav.roster")),
		template.HTMLEscapeString(T(lang, "nav.professions")),
		template.HTMLEscapeString(T(lang, "nav.mythicplus")),
		template.HTMLEscapeString(T(lang, "nav.equipment")),
		template.HTMLEscapeString(T(lang, "nav.achievements")),
		template.HTMLEscapeString(T(lang, "nav.history")),
		template.HTMLEscapeString(T(lang, "nav.logout")),
	)
}

// BuildPageHead returns shared head markup parameterized by lang and title
func BuildPageHead(lang, extraCSS, title string) string {
	return `<!DOCTYPE html>
<html lang="` + lang + `">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body { background: linear-gradient(to bottom, #0a0e1a 0%, #1a1f2e 50%, #0f1419 100%); color: #f8e6c8;
  font-family: 'Palatino Linotype','Book Antiqua',Palatino,serif; min-height: 100vh; padding: 20px; }
.container { max-width: 1400px; margin: 0 auto; padding: 0 20px; }
header { text-align: center; margin-bottom: 20px; padding: 5px 20px; position: relative; }
.header-nav-btns { position: absolute; top: 10px; left: 10px; display: flex; flex-direction: column; gap: 8px; width: max-content; z-index: 5; }
.nav-btn { display: inline-flex; align-items: center; padding: 10px 16px; font-size: 0.9em; gap: 8px; white-space: nowrap;
  width: 100%; border-radius: 4px; border: 2px solid #7d6a4d; background: linear-gradient(135deg, rgba(61,48,32,0.8), rgba(40,32,20,0.8));
  color: #f8e6c8; text-decoration: none; font-family: inherit; letter-spacing: 1px; text-shadow: 1px 1px 2px rgba(0,0,0,0.8);
  transition: border-color .2s, background .2s; }
.nav-btn:hover { border-color: #9d8a6d; background: linear-gradient(135deg, rgba(81,68,52,0.9), rgba(60,52,40,0.9)); }
.nav-btn-vault { background: linear-gradient(135deg, rgba(80,40,120,0.85), rgba(50,20,80,0.85)); border-color: #a060e0; color: #e8d0ff; }
.nav-btn-roster { background: linear-gradient(135deg, rgba(100,80,20,0.85), rgba(70,55,10,0.85)); border-color: #c8a020; color: #ffe680; }
.nav-btn-prof { background: linear-gradient(135deg, rgba(20,70,50,0.85), rgba(10,50,35,0.85)); border-color: #20c87a; color: #80ffcc; }
.nav-btn-history { background: linear-gradient(135deg, rgba(20,50,90,0.85), rgba(10,35,70,0.85)); border-color: #4080c0; color: #a0d0ff; }
.nav-btn-mplus { background: linear-gradient(135deg, rgba(110,50,20,0.85), rgba(80,30,10,0.85)); border-color: #e07830; color: #ffc890; }
.nav-btn-gear { background: linear-gradient(135deg, rgba(90,30,60,0.85), rgba(60,15,40,0.85)); border-color: #d050a0; color: #ffc0e8; }
.nav-btn-logout { background: linear-gradient(135deg, rgba(80,20,20,0.8), rgba(50,10,10,0.8)); border-color: #a04040; color: #ffaaaa; }
.logo { max-width: 350px; height: auto; filter: drop-shadow(0 4px 8px rgba(0,0,0,0.5)); margin-bottom: 5px; }
.page-header { text-align: center; margin-bottom: 30px; }
.page-header h1 { font-size: 2.2em; letter-spacing: 2px; text-shadow: 2px 2px 6px rgba(0,0,0,0.8); }
.page-header p { color: #9b8a6e; font-size: .95em; margin-top: 6px; }
.page-wrap { max-width: 1300px; margin: 0 auto; padding: 0 20px 60px; }
.card { background: linear-gradient(135deg, rgba(26,31,46,0.9), rgba(15,20,25,0.9)); border: 2px solid #3d3020;
  border-radius: 10px; padding: 20px 24px; margin-bottom: 24px; box-shadow: 0 8px 32px rgba(0,0,0,0.6); }
.notice-box { max-width: 620px; margin: 40px auto; text-align: center; }
.notice-box h2 { margin-bottom: 12px; letter-spacing: 1px; }
.notice-box p { color: #9b8a6e; line-height: 1.7; }
.muted { color: #9b8a6e; }
.section-title { font-size: 1.05em; color: #c8a050; border-bottom: 1px solid #3d3020; padding-bottom: 8px;
  margin: 28px 0 16px; letter-spacing: 1.5px; text-transform: uppercase; }
` + extraCSS + `
` + navLayoutCSS + `
</style>
<title>` + template.HTMLEscapeString(title) + `</title>
</head>`
}

// BuildPageNav returns shared header markup with logo, localized nav and lang switcher
func BuildPageNav(lang, currentPath string) string {
	return `<body>
<div class="container">
<header>
  ` + string(LanguageSelectorHTML(lang, currentPath)) + `
  ` + NavHTML(lang) + `
  <img src="/images/World-of-Warcraft-Logo-2001.png" alt="World of Warcraft" class="logo">
</header>
</div>
`
}

// BuildPageLoginNotice returns localized login notice
func BuildPageLoginNotice(lang string) string {
	return `<div class="page-wrap"><div class="card notice-box"><h2>` +
		template.HTMLEscapeString(T(lang, "notice.not_logged_in_title")) + `</h2>
<p>` + template.HTMLEscapeString(T(lang, "notice.not_logged_in_desc")) + ` <a href="/login" style="color:#80c0ff">` +
		template.HTMLEscapeString(T(lang, "notice.login_link")) + `</a></p></div></div></body></html>`
}

func init() {
	// Initialize default UI language from environment if already available
	InitUILanguage(os.Getenv("LOCALE"), os.Getenv("UI_LOCALE"))
}
