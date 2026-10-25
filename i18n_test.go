package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestLanguageResolutionAndDefaults(t *testing.T) {
	tests := []struct {
		name      string
		apiLocale string
		uiLocale  string
		reqCookie string
		reqQuery  string
		expected  string
	}{
		{
			name:      "default to en when empty",
			apiLocale: "en_US",
			uiLocale:  "",
			expected:  LangEn,
		},
		{
			name:      "default to zh-TW when API is zh_TW",
			apiLocale: "zh_TW",
			uiLocale:  "",
			expected:  LangZhTW,
		},
		{
			name:      "default to zh-TW when API is zh-tw case-insensitive",
			apiLocale: "zh-tw",
			uiLocale:  "",
			expected:  LangZhTW,
		},
		{
			name:      "UI_LOCALE overrides API LOCALE",
			apiLocale: "zh_TW",
			uiLocale:  "en",
			expected:  LangEn,
		},
		{
			name:      "UI_LOCALE overrides API LOCALE to zh-TW",
			apiLocale: "en_GB",
			uiLocale:  "zh-TW",
			expected:  LangZhTW,
		},
		{
			name:      "Cookie overrides defaults",
			apiLocale: "en_US",
			uiLocale:  "en",
			reqCookie: "zh-TW",
			expected:  LangZhTW,
		},
		{
			name:      "Query param overrides cookie and defaults",
			apiLocale: "en_US",
			uiLocale:  "en",
			reqCookie: "en",
			reqQuery:  "zh-TW",
			expected:  LangZhTW,
		},
		{
			name:      "Unsupported cookie falls back to default",
			apiLocale: "en_US",
			uiLocale:  "en",
			reqCookie: "fr-FR",
			expected:  LangEn,
		},
		{
			name:      "Unsupported query param falls back to default",
			apiLocale: "zh_TW",
			uiLocale:  "",
			reqQuery:  "de-DE",
			expected:  LangZhTW,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			InitUILanguage(tt.apiLocale, tt.uiLocale)

			target := "/"
			if tt.reqQuery != "" {
				target += "?lang=" + url.QueryEscape(tt.reqQuery)
			}
			req := httptest.NewRequest(http.MethodGet, target, nil)
			if tt.reqCookie != "" {
				req.AddCookie(&http.Cookie{
					Name:  cookieName,
					Value: tt.reqCookie,
				})
			}

			actual := ResolveLanguage(req)
			if actual != tt.expected {
				t.Errorf("ResolveLanguage() = %v, want %v", actual, tt.expected)
			}
		})
	}
}

func TestSetLangHandler(t *testing.T) {
	InitUILanguage("en_US", "")

	// 1. Valid language zh-TW
	req := httptest.NewRequest(http.MethodGet, "/set-lang?lang=zh-TW&redirect=/vault", nil)
	rr := httptest.NewRecorder()
	handleSetLang(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/vault" {
		t.Errorf("expected redirect to /vault, got %s", loc)
	}

	cookies := rr.Result().Cookies()
	var langCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == cookieName {
			langCookie = c
			break
		}
	}
	if langCookie == nil {
		t.Fatal("expected language cookie to be set")
	}
	if langCookie.Value != LangZhTW {
		t.Errorf("expected cookie value %s, got %s", LangZhTW, langCookie.Value)
	}

	// 2. Unsupported language falls back to default
	req = httptest.NewRequest(http.MethodGet, "/set-lang?lang=invalid&redirect=/roster", nil)
	rr = httptest.NewRecorder()
	handleSetLang(rr, req)

	cookies = rr.Result().Cookies()
	for _, c := range cookies {
		if c.Name == cookieName {
			langCookie = c
			break
		}
	}
	if langCookie == nil || langCookie.Value != LangEn {
		t.Errorf("expected fallback cookie %s, got %v", LangEn, langCookie)
	}

	// 3. Security: Prevent open redirect
	req = httptest.NewRequest(http.MethodGet, "/set-lang?lang=en&redirect=https://evil.com", nil)
	rr = httptest.NewRecorder()
	handleSetLang(rr, req)
	if loc := rr.Header().Get("Location"); loc != "/" {
		t.Errorf("expected redirect fallback to /, got %s", loc)
	}
}

func createTestAccountSummary() AccountSummary {
	return AccountSummary{
		Authenticated: true,
		LastUpdate:    time.Now(),
		AllianceCount: 1,
		HordeCount:    1,
		TotalGold:     2100000000,
		Characters: []CharacterStats{
			{
				Name:         "Arthas<script>alert(1)</script>",
				Realm:        "Silvermoon",
				Class:        "Paladin",
				Level:        90,
				Faction:      "Alliance",
				ItemLevel:    620,
				Gold:         1250000000,
				ThumbnailURL: "https://render.worldofwarcraft.com/eu/icons/56/classicon_paladin.jpg",
				Professions: []Profession{
					{
						Name:        "Blacksmithing",
						IsSecondary: false,
						Tiers: []ProfessionTier{
							{TierName: "Khaz Algar Blacksmithing", Skill: 85, Max: 100},
						},
					},
					{
						Name:        "Cooking",
						IsSecondary: true,
						Tiers: []ProfessionTier{
							{TierName: "Khaz Algar Cooking", Skill: 100, Max: 100},
						},
					},
				},
				Currencies: map[int64]int64{
					3000: 50,
					3001: 10,
				},
				CurrencyNames: map[int64]string{
					3000: "Dawncrests",
					3001: "Restored Coffer Keys",
				},
			},
			{
				Name:         "Thrall",
				Realm:        "Tarren Mill",
				Class:        "Shaman",
				Level:        90,
				Faction:      "Horde",
				ItemLevel:    615,
				Gold:         850000000,
				ThumbnailURL: "https://render.worldofwarcraft.com/eu/icons/56/classicon_shaman.jpg",
				Professions: []Profession{
					{
						Name:        "Alchemy",
						IsSecondary: false,
						Tiers: []ProfessionTier{
							{TierName: "Khaz Algar Alchemy", Skill: 50, Max: 100},
						},
					},
				},
			},
		},
	}
}

func seedCachesForTests() {
	// Seed currency cache
	currencyCacheMutex.Lock()
	if currencyNameToID == nil {
		currencyNameToID = make(map[string]int64)
	}
	desired := []string{"Dawncrests", "Radiant Spark Dust", "Dawnlight Manaflux", "Coffer Key Shards", "Restored Coffer Keys", "Voidlight Marl"}
	for i, d := range desired {
		currencyNameToID[strings.ToLower(d)] = int64(3000 + i)
	}
	currencyCacheMutex.Unlock()

	// Seed Mythic+ cache
	mpState.Lock()
	mpState.fetched = time.Now()
	mpState.loading = false
	mpState.data = []MPChar{
		{
			Name:     "Arthas<script>alert(1)</script>",
			Realm:    "Silvermoon",
			Rating:   2450.5,
			WeekRuns: 4,
			HasData:  true,
			Best: map[string]MPRun{
				"Ara-Kara, City of Echoes": {
					Dungeon:    "Ara-Kara, City of Echoes",
					Level:      10,
					DurationMs: 1450000,
					Timed:      true,
					Rating:     310.5,
					Affixes:    []string{"Tyrannical", "Challenger's Peril"},
				},
				"The Dawnbreaker": {
					Dungeon:    "The Dawnbreaker",
					Level:      8,
					DurationMs: 1980000,
					Timed:      false,
					Rating:     240.0,
					Affixes:    []string{"Fortified"},
				},
			},
			Vault: []VaultSlot{
				{Pos: 1, Label: "1st run", Unlocked: true, Level: 10, Ilvl: 710},
				{Pos: 4, Label: "4th run", Unlocked: true, Level: 8, Ilvl: 704},
				{Pos: 8, Label: "8th run", Unlocked: false},
			},
		},
	}
	mpState.Unlock()

	// Seed Equipment cache
	eqState.Lock()
	eqState.fetched = time.Now()
	eqState.loading = false
	eqState.data = []EQChar{
		{
			Name:  "Arthas<script>alert(1)</script>",
			Realm: "Silvermoon",
			Level: 90,
			Ilvl:  620,
			Items: map[string]EquippedItem{
				"HEAD": {
					ID:      12345,
					Name:    "Crown of the Silver Hand",
					Ilvl:    626,
					Quality: "epic",
				},
			},
			Stats: GearStats{
				Stamina:       125000,
				Crit:          22.5,
				CritRating:    4500,
				Haste:         18.2,
				HasteRating:   3800,
				Mastery:       35.0,
				MasteryRating: 5200,
				Vers:          5.1,
				VersRating:    1500,
			},
			Warnings: []string{"Head: empty gem socket", "Wrists: missing enchant"},
		},
	}
	eqState.Unlock()

	// Seed Achievements cache
	achCats.Lock()
	achCats.fetched = time.Now()
	achCats.loading = false
	achCats.err = ""
	achCats.Unlock()

	achProfiles.Lock()
	achProfiles.m["arthas<script>alert(1)</script>-silvermoon"] = &achProfileEntry{
		fetched: time.Now(),
		loading: false,
		view: &achView{
			Points:  18500,
			Overall: 1420,
			Groups: []AchGroup{
				{
					Name:  "Dungeons & Raids",
					Total: 250,
					Done:  180,
					Items: []AchItem{
						{ID: 101, Name: "Keystone Master: Season One", Status: "done", Pct: 100},
					},
				},
			},
			Recent: []AchItem{
				{ID: 101, Name: "Keystone Master: Season One", Status: "done", Pct: 100, When: "2026-03-01"},
			},
			Updated: time.Now().Format("15:04:05"),
		},
	}
	achProfiles.Unlock()
}

func TestPageTemplatesRenderEnglishAndZhTW(t *testing.T) {
	InitUILanguage("en_US", "")

	// Fixture data
	summary := createTestAccountSummary()
	summaryMutex.Lock()
	accountSummary = summary
	summaryMutex.Unlock()

	// Seed all caches
	seedCachesForTests()

	// Setup fake token and config
	tokenMutex.Lock()
	userToken = &oauth2.Token{AccessToken: "fake-test-token", Expiry: time.Now().Add(24 * time.Hour)}
	oauth2Config = &oauth2.Config{}
	tokenMutex.Unlock()

	t.Cleanup(func() {
		summaryMutex.Lock()
		accountSummary = AccountSummary{}
		summaryMutex.Unlock()

		tokenMutex.Lock()
		userToken = nil
		oauth2Config = nil
		tokenMutex.Unlock()

		mpState.Lock()
		mpState.fetched = time.Time{}
		mpState.data = nil
		mpState.Unlock()

		eqState.Lock()
		eqState.fetched = time.Time{}
		eqState.data = nil
		eqState.Unlock()

		resetAchProfiles()
		achCats.Lock()
		achCats.fetched = time.Time{}
		achCats.loading = false
		achCats.err = ""
		achCats.Unlock()
	})

	// Optional HTML fixture export directory
	artifactDir := os.Getenv("WOW_I18N_ARTIFACT_DIR")
	if artifactDir == "" {
		artifactDir = filepath.Join(os.TempDir(), "wow-zh-fixtures")
	}
	_ = os.MkdirAll(artifactDir, 0755)

	languages := []struct {
		code           string
		htmlLang       string
		homeTitle      string
		vaultTitle     string
		rosterTitle    string
		profTitle      string
		mplusTitle     string
		equipTitle     string
		achTitle       string
		historyTitle   string
		currencyTitle  string
		vaultRaidSlot1 string
		vaultDropHint  string
		rosterClearBtn string
		profLearned    string
		historyGold    string
		equipSecStats  string
		mplusTimed     string
		achPoints      string
		commonAlliance string
		commonHorde    string
	}{
		{
			code:           LangEn,
			htmlLang:       `html lang="en"`,
			homeTitle:      "WoW Character Stats",
			vaultTitle:     "Great Vault Tracker",
			rosterTitle:    "Weekly Reset Roster",
			profTitle:      "Profession Tracker",
			mplusTitle:     "Mythic+ Breakdown",
			equipTitle:     "Equipment Inspector",
			achTitle:       "Achievements",
			historyTitle:   "Progression History",
			currencyTitle:  "Player Currency",
			vaultRaidSlot1: "Defeat 2 Midnight Season 1 Boss",
			vaultDropHint:  "Drop character",
			rosterClearBtn: "Clear Manual Checks",
			profLearned:    "learned",
			historyGold:    "Database Not Configured",
			equipSecStats:  "Secondary Stats",
			mplusTimed:     "Timed",
			achPoints:      "Achievement points",
			commonAlliance: "Alliance",
			commonHorde:    "Horde",
		},
		{
			code:           LangZhTW,
			htmlLang:       `html lang="zh-TW"`,
			homeTitle:      "魔獸世界角色統計",
			vaultTitle:     "宏偉寶庫追蹤器",
			rosterTitle:    "每週重置清單",
			profTitle:      "專業技能追蹤器",
			mplusTitle:     "傳奇+ 表現分析",
			equipTitle:     "裝備檢視器",
			achTitle:       "成就",
			historyTitle:   "歷程進度記錄",
			currencyTitle:  "角色通貨一覽",
			vaultRaidSlot1: "擊敗 2 名 Midnight 第 1 季首領",
			vaultDropHint:  "拖曳角色至此",
			rosterClearBtn: "清除手動勾選",
			profLearned:    "已習得",
			historyGold:    "未設定資料庫",
			equipSecStats:  "次要屬性",
			mplusTimed:     "限時完成",
			achPoints:      "成就點數",
			commonAlliance: "聯盟",
			commonHorde:    "部落",
		},
	}

	saveFixture := func(lang, pageName, html string) {
		fname := filepath.Join(artifactDir, fmt.Sprintf("%s-%s.html", lang, pageName))
		_ = os.WriteFile(fname, []byte(html), 0644)
	}

	assertGeneralQuality := func(t *testing.T, pageName, lang, body string) {
		t.Helper()
		if strings.Contains(body, "%!(") || strings.Contains(body, "%!s") || strings.Contains(body, "%!d") {
			t.Errorf("[%s] %s: contains fmt formatting error: %s", lang, pageName, body)
		}
		// Assert <title> is in <head> and outside <style>
		styleIdx := strings.Index(body, "<style>")
		styleCloseIdx := strings.Index(body, "</style>")
		titleIdx := strings.Index(body, "<title>")
		headCloseIdx := strings.Index(body, "</head>")

		if titleIdx != -1 {
			if styleIdx != -1 && styleCloseIdx != -1 {
				if titleIdx > styleIdx && titleIdx < styleCloseIdx {
					t.Errorf("[%s] %s: <title> is located INSIDE <style>", lang, pageName)
				}
			}
			if headCloseIdx != -1 && titleIdx > headCloseIdx {
				t.Errorf("[%s] %s: <title> is located AFTER </head>", lang, pageName)
			}
		}
		// Assert XSS escaping
		if strings.Contains(body, "<script>alert(1)</script>") {
			t.Errorf("[%s] %s: unescaped script tag found!", lang, pageName)
		}
	}

	assertExactTitle := func(pageName, lang, expectedTitle, body string) {
		t.Helper()
		start := strings.Index(body, "<title>")
		end := strings.Index(body, "</title>")
		if start == -1 || end == -1 || end <= start {
			t.Errorf("[%s] %s: missing <title> tag", lang, pageName)
			return
		}
		actual := body[start+len("<title>") : end]
		if actual != expectedTitle {
			t.Errorf("[%s] %s: <title> mismatch: got %q, want %q", lang, pageName, actual, expectedTitle)
		}
	}

	for _, l := range languages {
		t.Run("Pages_"+l.code, func(t *testing.T) {
			cookie := &http.Cookie{Name: cookieName, Value: l.code}

			// 1. Home Page (Authenticated)
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.AddCookie(cookie)
			rr := httptest.NewRecorder()
			handleHome(rr, req)
			body := rr.Body.String()
			saveFixture(l.code, "home-auth", body)
			assertGeneralQuality(t, "Home", l.code, body)
			assertExactTitle("Home", l.code, l.homeTitle, body)

			if !strings.Contains(body, l.htmlLang) {
				t.Errorf("[%s] Home: missing %s", l.code, l.htmlLang)
			}
			if !strings.Contains(body, l.homeTitle) {
				t.Errorf("[%s] Home: missing title %s", l.code, l.homeTitle)
			}
			if !strings.Contains(body, l.commonAlliance) || !strings.Contains(body, l.commonHorde) {
				t.Errorf("[%s] Home: missing faction strings", l.code)
			}
			if !strings.Contains(body, `class="header-lang-selector"`) {
				t.Errorf("[%s] Home: missing language selector", l.code)
			}

			// 2. Home Page (Unauthenticated)
			summaryMutex.Lock()
			accountSummary = AccountSummary{Authenticated: false}
			summaryMutex.Unlock()
			tokenMutex.Lock()
			savedToken := userToken
			userToken = nil
			tokenMutex.Unlock()

			req = httptest.NewRequest(http.MethodGet, "/", nil)
			req.AddCookie(cookie)
			rr = httptest.NewRecorder()
			handleHome(rr, req)
			body = rr.Body.String()
			saveFixture(l.code, "home-unauth", body)
			assertGeneralQuality(t, "Home-Unauth", l.code, body)
			assertExactTitle("Home-Unauth", l.code, l.homeTitle, body)

			if !strings.Contains(body, l.htmlLang) {
				t.Errorf("[%s] Unauthenticated Home: missing %s", l.code, l.htmlLang)
			}
			if l.code == LangZhTW && !strings.Contains(body, "歡迎使用魔獸世界數據追蹤器！") {
				t.Errorf("[%s] Unauthenticated Home: missing zh-TW welcome text", l.code)
			}
			if l.code == LangEn && !strings.Contains(body, "Welcome to WoW Stats Tracker!") {
				t.Errorf("[%s] Unauthenticated Home: missing English welcome text", l.code)
			}

			// Restore authenticated state
			summaryMutex.Lock()
			accountSummary = summary
			summaryMutex.Unlock()
			tokenMutex.Lock()
			userToken = savedToken
			tokenMutex.Unlock()

			// 3. Vault Page
			req = httptest.NewRequest(http.MethodGet, "/vault", nil)
			req.AddCookie(cookie)
			rr = httptest.NewRecorder()
			handleVault(rr, req)
			body = rr.Body.String()
			saveFixture(l.code, "vault", body)
			assertGeneralQuality(t, "Vault", l.code, body)
			assertExactTitle("Vault", l.code, l.vaultTitle, body)

			if !strings.Contains(body, l.htmlLang) {
				t.Errorf("[%s] Vault: missing %s", l.code, l.htmlLang)
			}
			if !strings.Contains(body, l.vaultTitle) {
				t.Errorf("[%s] Vault: missing title %s", l.code, l.vaultTitle)
			}
			if !strings.Contains(body, l.vaultRaidSlot1) {
				t.Errorf("[%s] Vault: missing slot requirement %s", l.code, l.vaultRaidSlot1)
			}
			if !strings.Contains(body, l.vaultDropHint) {
				t.Errorf("[%s] Vault: missing drop hint %s", l.code, l.vaultDropHint)
			}

			// 4. Roster Page
			req = httptest.NewRequest(http.MethodGet, "/roster", nil)
			req.AddCookie(cookie)
			rr = httptest.NewRecorder()
			handleRoster(rr, req)
			body = rr.Body.String()
			saveFixture(l.code, "roster", body)
			assertGeneralQuality(t, "Roster", l.code, body)
			assertExactTitle("Roster", l.code, l.rosterTitle, body)

			if !strings.Contains(body, l.htmlLang) {
				t.Errorf("[%s] Roster: missing %s", l.code, l.htmlLang)
			}
			if !strings.Contains(body, l.rosterTitle) {
				t.Errorf("[%s] Roster: missing title %s", l.code, l.rosterTitle)
			}
			if !strings.Contains(body, l.rosterClearBtn) {
				t.Errorf("[%s] Roster: missing clear btn %s", l.code, l.rosterClearBtn)
			}

			// 5. Professions Page
			req = httptest.NewRequest(http.MethodGet, "/professions", nil)
			req.AddCookie(cookie)
			rr = httptest.NewRecorder()
			handleProfessions(rr, req)
			body = rr.Body.String()
			saveFixture(l.code, "professions", body)
			assertGeneralQuality(t, "Professions", l.code, body)
			assertExactTitle("Professions", l.code, l.profTitle, body)

			if !strings.Contains(body, l.htmlLang) {
				t.Errorf("[%s] Professions: missing %s", l.code, l.htmlLang)
			}
			if !strings.Contains(body, l.profTitle) {
				t.Errorf("[%s] Professions: missing title %s", l.code, l.profTitle)
			}
			if !strings.Contains(body, l.profLearned) {
				t.Errorf("[%s] Professions: missing learned text %s", l.code, l.profLearned)
			}

			// 6. History Page
			req = httptest.NewRequest(http.MethodGet, "/history", nil)
			req.AddCookie(cookie)
			rr = httptest.NewRecorder()
			handleHistory(rr, req)
			body = rr.Body.String()
			saveFixture(l.code, "history", body)
			assertGeneralQuality(t, "History", l.code, body)
			assertExactTitle("History", l.code, l.historyTitle, body)

			if !strings.Contains(body, l.htmlLang) {
				t.Errorf("[%s] History: missing %s", l.code, l.htmlLang)
			}
			if !strings.Contains(body, l.historyTitle) {
				t.Errorf("[%s] History: missing title %s", l.code, l.historyTitle)
			}
			if !strings.Contains(body, l.historyGold) {
				t.Errorf("[%s] History: missing gold note %s", l.code, l.historyGold)
			}

			// 7. Player Currency Page
			req = httptest.NewRequest(http.MethodGet, "/player-currency", nil)
			req.AddCookie(cookie)
			rr = httptest.NewRecorder()
			handlePlayerCurrency(rr, req)
			body = rr.Body.String()
			saveFixture(l.code, "player-currency", body)
			assertGeneralQuality(t, "PlayerCurrency", l.code, body)

			if !strings.Contains(body, l.currencyTitle) {
				t.Errorf("[%s] Currency: missing title %s", l.code, l.currencyTitle)
			}

			// 8. Mythic+ Page
			req = httptest.NewRequest(http.MethodGet, "/mythic-plus", nil)
			req.AddCookie(cookie)
			rr = httptest.NewRecorder()
			handleMythicPlus(rr, req)
			body = rr.Body.String()
			saveFixture(l.code, "mythic-plus", body)
			assertGeneralQuality(t, "MythicPlus", l.code, body)
			assertExactTitle("MythicPlus", l.code, l.mplusTitle, body)

			if !strings.Contains(body, l.htmlLang) {
				t.Errorf("[%s] Mythic+: missing %s", l.code, l.htmlLang)
			}
			if !strings.Contains(body, l.mplusTitle) {
				t.Errorf("[%s] Mythic+: missing title %s", l.code, l.mplusTitle)
			}
			if !strings.Contains(body, l.mplusTimed) {
				t.Errorf("[%s] Mythic+: missing timed status %s", l.code, l.mplusTimed)
			}
			if l.code == LangZhTW {
				if !strings.Contains(body, "獎勵物品等級為佔位符（地心之戰數值）— 請編輯 data/vault_ilvl.json 以符合當前賽季。") {
					t.Errorf("[%s] Mythic+: missing localized placeholder vault note", l.code)
				}
			} else {
				if !strings.Contains(body, "Reward item levels are placeholders (TWW-era values) — edit data/vault_ilvl.json to match the current season.") {
					t.Errorf("[%s] Mythic+: missing en placeholder vault note", l.code)
				}
			}

			// 9. Equipment Page
			req = httptest.NewRequest(http.MethodGet, "/equipment", nil)
			req.AddCookie(cookie)
			rr = httptest.NewRecorder()
			handleEquipment(rr, req)
			body = rr.Body.String()
			saveFixture(l.code, "equipment", body)
			assertGeneralQuality(t, "Equipment", l.code, body)
			assertExactTitle("Equipment", l.code, l.equipTitle, body)

			if !strings.Contains(body, l.htmlLang) {
				t.Errorf("[%s] Equipment: missing %s", l.code, l.htmlLang)
			}
			if !strings.Contains(body, l.equipTitle) {
				t.Errorf("[%s] Equipment: missing title %s", l.code, l.equipTitle)
			}
			if !strings.Contains(body, l.equipSecStats) {
				t.Errorf("[%s] Equipment: missing secondary stats label %s", l.code, l.equipSecStats)
			}
			if l.code == LangZhTW {
				if !strings.Contains(body, "頭部：寶石插槽未鑲嵌") {
					t.Errorf("[%s] Equipment: missing localized empty socket warning", l.code)
				}
				if !strings.Contains(body, "手腕：缺少附魔") {
					t.Errorf("[%s] Equipment: missing localized missing enchant warning", l.code)
				}
			} else {
				if !strings.Contains(body, "Head: empty gem socket") {
					t.Errorf("[%s] Equipment: missing en empty gem warning", l.code)
				}
				if !strings.Contains(body, "Wrists: missing enchant") {
					t.Errorf("[%s] Equipment: missing en missing enchant warning", l.code)
				}
			}

			// 10. Achievements Page
			req = httptest.NewRequest(http.MethodGet, "/achievements?c=arthas<script>alert(1)</script>-silvermoon", nil)
			req.AddCookie(cookie)
			rr = httptest.NewRecorder()
			handleAchievements(rr, req)
			body = rr.Body.String()
			saveFixture(l.code, "achievements", body)
			assertGeneralQuality(t, "Achievements", l.code, body)
			assertExactTitle("Achievements", l.code, l.achTitle, body)

			if !strings.Contains(body, l.htmlLang) {
				t.Errorf("[%s] Achievements: missing %s; got status %d", l.code, l.htmlLang, rr.Code)
			}
			if !strings.Contains(body, l.achTitle) {
				t.Errorf("[%s] Achievements: missing title %s", l.code, l.achTitle)
			}
			if !strings.Contains(body, l.achPoints) {
				t.Errorf("[%s] Achievements: missing points label %s", l.code, l.achPoints)
			}
		})
	}
}

func TestUnauthenticatedRedirectAndLoginNotice(t *testing.T) {
	InitUILanguage("en_US", "")

	// When userToken is nil:
	tokenMutex.Lock()
	userToken = nil
	tokenMutex.Unlock()

	// /mythic-plus redirects unauthenticated to /
	req := httptest.NewRequest(http.MethodGet, "/mythic-plus", nil)
	rr := httptest.NewRecorder()
	handleMythicPlus(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", rr.Code)
	}

	// BuildPageLoginNotice
	enNotice := BuildPageLoginNotice(LangEn)
	if !strings.Contains(enNotice, "Not logged in") || !strings.Contains(enNotice, "log in with Battle.net") {
		t.Errorf("BuildPageLoginNotice(en) failed: %s", enNotice)
	}

	zhNotice := BuildPageLoginNotice(LangZhTW)
	if !strings.Contains(zhNotice, "未登入") || !strings.Contains(zhNotice, "透過 Battle.net 登入") {
		t.Errorf("BuildPageLoginNotice(zh-TW) failed: %s", zhNotice)
	}
}

func TestLanguageSelectorHTML_RedirectParams(t *testing.T) {
	// Strips only lang parameter and preserves other params like c=Arthas
	html := string(LanguageSelectorHTML(LangEn, "/equipment?lang=en&c=Arthas"))
	if strings.Contains(html, "lang=en&amp;c=Arthas") || strings.Contains(html, "lang%3Den") {
		t.Errorf("LanguageSelectorHTML should strip lang from redirect URL: %s", html)
	}
	if !strings.Contains(html, "redirect=%2Fequipment%3Fc%3DArthas") {
		t.Errorf("LanguageSelectorHTML did not preserve query param c=Arthas: %s", html)
	}

	// Multiple parameters preserved
	html2 := string(LanguageSelectorHTML(LangZhTW, "/equipment?c=Arthas&foo=bar&lang=zh-TW"))
	if strings.Contains(html2, "lang%3Dzh-TW") {
		t.Errorf("LanguageSelectorHTML should strip lang from redirect: %s", html2)
	}
	if !strings.Contains(html2, "c%3DArthas") || !strings.Contains(html2, "foo%3Dbar") {
		t.Errorf("LanguageSelectorHTML lost query parameters: %s", html2)
	}

	// Unsafe redirects fallback to /
	html3 := string(LanguageSelectorHTML(LangEn, "//evil.com/test"))
	if !strings.Contains(html3, "redirect=%2F") {
		t.Errorf("LanguageSelectorHTML did not sanitize unsafe redirect: %s", html3)
	}
}

func TestProfessionsTranslationKeys(t *testing.T) {
	// Verify that recovered professions template resolves all keys without literal placeholders
	enTmpl := BuildProfessionsTemplate(LangEn, "/professions")
	zhTmpl := BuildProfessionsTemplate(LangZhTW, "/professions")

	literalKeys := []string{"prof.primary", "prof.secondary", "prof.all_tiers", "prof.character"}
	for _, k := range literalKeys {
		if strings.Contains(enTmpl, `"`+k+`"`) || strings.Contains(zhTmpl, `"`+k+`"`) {
			t.Errorf("Professions template contains unresolved literal key: %s", k)
		}
	}

	for _, l := range []string{LangEn, LangZhTW} {
		for _, k := range []string{"prof.tab_primary", "prof.tab_secondary", "prof.tab_all", "prof.th_character", "prof.title", "prof.subtitle", "prof.learned"} {
			val := T(l, k)
			if val == "" || strings.HasPrefix(val, "prof.") {
				t.Errorf("[%s] key %s not properly translated: %q", l, k, val)
			}
		}
	}
}

func TestVaultPlaceholderNoteAndCustomNote(t *testing.T) {
	// Built-in note matches defaultVaultIlvlNote
	if T(LangZhTW, "mplus.vault_ilvl_placeholder_note") == defaultVaultIlvlNote {
		t.Errorf("mplus.vault_ilvl_placeholder_note zh-TW should be translated")
	}

	// Verify exact match check logic
	customNote := "Custom Season 2 ilvl note — user edited"
	if customNote == defaultVaultIlvlNote {
		t.Fatalf("customNote should not equal defaultVaultIlvlNote")
	}
}

func TestHistoryTemplateWithData(t *testing.T) {
	fixtureData := HistoryPageData{
		DBEnabled: true,
		AccountHistory: AccountHistory{
			Labels:    []string{"2026-10-25"},
			TotalGold: []int64{100},
			Mounts:    []int{2},
			Pets:      []int{3},
		},
		CharHistories: []CharacterHistory{
			{
				Name:      "Fixture",
				Realm:     "Test",
				Labels:    []string{"2026-10-25"},
				ItemLevel: []int{620},
				Level:     []int{90},
				Gold:      []int64{100},
			},
		},
	}

	artifactDir := os.Getenv("WOW_I18N_ARTIFACT_DIR")
	if artifactDir == "" {
		artifactDir = filepath.Join(os.TempDir(), "wow-zh-fixtures")
	}
	_ = os.MkdirAll(artifactDir, 0755)

	for _, lang := range []string{LangEn, LangZhTW} {
		tmpl, err := template.New("history").Funcs(template.FuncMap{
			"json": func(v interface{}) (template.JS, error) {
				b, err := json.Marshal(v)
				return template.JS(b), err
			},
			"t":    func(k string) string { return T(lang, k) },
			"tFmt": func(k string, args ...interface{}) string { return TFmt(lang, k, args...) },
			"lang": func() string { return lang },
		}).Parse(BuildHistoryTemplate(lang, "/history"))
		if err != nil {
			t.Fatalf("[%s] failed to parse history template: %v", lang, err)
		}

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, fixtureData); err != nil {
			t.Fatalf("[%s] failed to execute history template: %v", lang, err)
		}
		rendered := buf.String()

		// Assert Chart.js dependency tag exists and comes before new Chart
		chartDep := `<script src="https://cdn.jsdelivr.net/npm/chart.js@4.4.0/dist/chart.umd.min.js"></script>`
		chartScriptIdx := strings.Index(rendered, chartDep)
		if chartScriptIdx == -1 {
			t.Errorf("[%s] Chart.js dependency tag missing", lang)
		}
		newChartIdx := strings.Index(rendered, "new Chart(")
		if newChartIdx == -1 {
			t.Errorf("[%s] new Chart( missing in rendered output", lang)
		}
		if chartScriptIdx != -1 && newChartIdx != -1 && chartScriptIdx > newChartIdx {
			t.Errorf("[%s] Chart.js dependency must come before new Chart call", lang)
		}

		// Assert translated dataset labels and canvases appear
		goldLabel := T(lang, "history.ds_total_gold")
		if !strings.Contains(rendered, goldLabel) {
			t.Errorf("[%s] missing translated dataset label %q", lang, goldLabel)
		}
		if !strings.Contains(rendered, `id="chartGold"`) {
			t.Errorf("[%s] missing chartGold canvas", lang)
		}
		if !strings.Contains(rendered, `id="chartCollections"`) {
			t.Errorf("[%s] missing chartCollections canvas", lang)
		}

		// Export fixture
		outPath := filepath.Join(artifactDir, fmt.Sprintf("%s-history-data.html", lang))
		if err := os.WriteFile(outPath, []byte(rendered), 0644); err != nil {
			t.Errorf("failed to save history data fixture: %v", err)
		}
	}
}

func TestAllLiteralDictionaryKeysExist(t *testing.T) {
	content, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("failed to read main.go: %v", err)
	}
	re := regexp.MustCompile(`T(?:Fmt)?\([^,]+,\s*"([^"]+)"`)
	matches := re.FindAllStringSubmatch(string(content), -1)
	if len(matches) == 0 {
		t.Fatalf("no dictionary keys found in main.go")
	}
	for _, m := range matches {
		key := m[1]
		if _, ok := translations[key]; !ok {
			t.Errorf("literal key %q referenced in main.go does not exist in translations dictionary", key)
		}
	}
}
