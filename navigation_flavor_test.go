package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNavigationFlavor(t *testing.T) {
	// Save existing global config
	oldConfig := config
	defer func() {
		config = oldConfig
	}()

	locales := []struct {
		name       string
		lang       string
		homeLabel  string
		profLabel  string
		equipLabel string
		achLabel   string
		histLabel  string
		logoutLbl  string
		vaultLabel string
		rosterLbl  string
		mplusLabel string
	}{
		{
			name:       "English",
			lang:       LangEn,
			homeLabel:  "🏠 Home",
			profLabel:  "Professions",
			equipLabel: "🛡️ Equipment",
			achLabel:   "🏆 Achievements",
			histLabel:  "History",
			logoutLbl:  "🔓 Re-authenticate",
			vaultLabel: "Great Vault",
			rosterLbl:  "Weekly Roster",
			mplusLabel: "Mythic+",
		},
		{
			name:       "Traditional Chinese",
			lang:       LangZhTW,
			homeLabel:  "🏠 首頁",
			profLabel:  "專業技能",
			equipLabel: "🛡️ 裝備檢視",
			achLabel:   "🏆 成就",
			histLabel:  "歷程記錄",
			logoutLbl:  "🔓 重新驗證",
			vaultLabel: "宏偉寶庫",
			rosterLbl:  "每週重置清單",
			mplusLabel: "傳奇+",
		},
	}

	for _, tc := range locales {
		t.Run("Retail_"+tc.name, func(t *testing.T) {
			config = Config{
				GameVersion: "retail",
			}
			if config.IsForever() {
				t.Fatalf("expected IsForever to be false for retail")
			}

			nav := NavHTML(tc.lang)

			// Retail must include all links
			for _, href := range []string{"href=\"/\"", "href=\"/vault\"", "href=\"/roster\"", "href=\"/professions\"", "href=\"/mythic-plus\"", "href=\"/equipment\"", "href=\"/achievements\"", "href=\"/history\"", "href=\"/logout\""} {
				if !strings.Contains(nav, href) {
					t.Errorf("Retail nav (%s) missing expected link: %s", tc.name, href)
				}
			}

			// Check localized labels
			for _, label := range []string{tc.homeLabel, tc.vaultLabel, tc.rosterLbl, tc.profLabel, tc.mplusLabel, tc.equipLabel, tc.achLabel, tc.histLabel, tc.logoutLbl} {
				if !strings.Contains(nav, label) {
					t.Errorf("Retail nav (%s) missing label %q", tc.name, label)
				}
			}

			// Shared BuildPageNav
			pageNav := BuildPageNav(tc.lang, "/")
			if !strings.Contains(pageNav, "href=\"/vault\"") || !strings.Contains(pageNav, "href=\"/roster\"") || !strings.Contains(pageNav, "href=\"/mythic-plus\"") {
				t.Errorf("Retail BuildPageNav (%s) should contain retail links", tc.name)
			}
		})

		t.Run("Forever_"+tc.name, func(t *testing.T) {
			config = Config{
				GameVersion: "forever",
			}
			if !config.IsForever() {
				t.Fatalf("expected IsForever to be true for forever")
			}

			nav := NavHTML(tc.lang)

			// Forever must omit retail-specific links server-side
			for _, href := range []string{"href=\"/vault\"", "href=\"/roster\"", "href=\"/mythic-plus\""} {
				if strings.Contains(nav, href) {
					t.Errorf("Forever nav (%s) must NOT contain retail link: %s", tc.name, href)
				}
			}
			// Labels should not appear in Forever nav
			for _, label := range []string{tc.vaultLabel, tc.rosterLbl, tc.mplusLabel} {
				if strings.Contains(nav, label) {
					t.Errorf("Forever nav (%s) must NOT contain retail label: %s", tc.name, label)
				}
			}

			// Preserved links
			for _, href := range []string{"href=\"/\"", "href=\"/professions\"", "href=\"/equipment\"", "href=\"/achievements\"", "href=\"/history\"", "href=\"/logout\""} {
				if !strings.Contains(nav, href) {
					t.Errorf("Forever nav (%s) missing preserved link: %s", tc.name, href)
				}
			}

			// Preserved localized labels
			for _, label := range []string{tc.homeLabel, tc.profLabel, tc.equipLabel, tc.achLabel, tc.histLabel, tc.logoutLbl} {
				if !strings.Contains(nav, label) {
					t.Errorf("Forever nav (%s) missing preserved label %q", tc.name, label)
				}
			}

			// Shared BuildPageNav
			pageNav := BuildPageNav(tc.lang, "/")
			for _, href := range []string{"href=\"/vault\"", "href=\"/roster\"", "href=\"/mythic-plus\""} {
				if strings.Contains(pageNav, href) {
					t.Errorf("Forever BuildPageNav (%s) must NOT contain retail link: %s", tc.name, href)
				}
			}
			for _, href := range []string{"href=\"/\"", "href=\"/professions\"", "href=\"/equipment\"", "href=\"/achievements\"", "href=\"/history\"", "href=\"/logout\""} {
				if !strings.Contains(pageNav, href) {
					t.Errorf("Forever BuildPageNav (%s) missing preserved link: %s", tc.name, href)
				}
			}
		})
	}
}

func TestNavigationFlavorHomeIntegration(t *testing.T) {
	oldConfig := config
	defer func() {
		config = oldConfig
	}()

	summaryMutex.Lock()
	savedSummary := accountSummary
	accountSummary = AccountSummary{Authenticated: true}
	summaryMutex.Unlock()
	defer func() {
		summaryMutex.Lock()
		accountSummary = savedSummary
		summaryMutex.Unlock()
	}()

	for _, gv := range []string{"retail", "forever"} {
		t.Run("Home_"+gv, func(t *testing.T) {
			config = Config{
				GameVersion: gv,
			}

			for _, lang := range []string{LangEn, LangZhTW} {
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.AddCookie(&http.Cookie{Name: "lang", Value: lang})
				rr := httptest.NewRecorder()
				handleHome(rr, req)

				body := rr.Body.String()
				if gv == "forever" {
					for _, retailLink := range []string{"href=\"/vault\"", "href=\"/roster\"", "href=\"/mythic-plus\""} {
						if strings.Contains(body, retailLink) {
							t.Errorf("Forever home page (%s) must NOT contain %s", lang, retailLink)
						}
					}
					for _, preserved := range []string{"href=\"/\"", "href=\"/professions\"", "href=\"/equipment\"", "href=\"/achievements\"", "href=\"/history\"", "href=\"/logout\""} {
						if !strings.Contains(body, preserved) {
							t.Errorf("Forever home page (%s) missing preserved link %s", lang, preserved)
						}
					}
				} else {
					for _, link := range []string{"href=\"/\"", "href=\"/vault\"", "href=\"/roster\"", "href=\"/professions\"", "href=\"/mythic-plus\"", "href=\"/equipment\"", "href=\"/achievements\"", "href=\"/history\"", "href=\"/logout\""} {
						if !strings.Contains(body, link) {
							t.Errorf("Retail home page (%s) missing link %s", lang, link)
						}
					}
				}
			}
		})
	}
}
