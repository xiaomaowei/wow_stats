package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

type mockTransport struct {
	roundTrip func(req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTrip(req)
}

func TestNamespaces(t *testing.T) {
	tests := []struct {
		name        string
		gameVersion string
		region      string
		wantProfile string
		wantStatic  string
		wantIsForvr bool
	}{
		{
			name:        "retail default us",
			gameVersion: "",
			region:      "us",
			wantProfile: "profile-us",
			wantStatic:  "static-us",
			wantIsForvr: false,
		},
		{
			name:        "retail explicit tw",
			gameVersion: "retail",
			region:      "tw",
			wantProfile: "profile-tw",
			wantStatic:  "static-tw",
			wantIsForvr: false,
		},
		{
			name:        "retail eu",
			gameVersion: "retail",
			region:      "eu",
			wantProfile: "profile-eu",
			wantStatic:  "static-eu",
			wantIsForvr: false,
		},
		{
			name:        "retail kr",
			gameVersion: "retail",
			region:      "kr",
			wantProfile: "profile-kr",
			wantStatic:  "static-kr",
			wantIsForvr: false,
		},
		{
			name:        "forever tw",
			gameVersion: "forever",
			region:      "tw",
			wantProfile: "profile-forever-tw",
			wantStatic:  "static-forever-tw",
			wantIsForvr: true,
		},
		{
			name:        "forever us",
			gameVersion: "forever",
			region:      "us",
			wantProfile: "profile-forever-us",
			wantStatic:  "static-forever-us",
			wantIsForvr: true,
		},
		{
			name:        "forever eu",
			gameVersion: "forever",
			region:      "eu",
			wantProfile: "profile-forever-eu",
			wantStatic:  "static-forever-eu",
			wantIsForvr: true,
		},
		{
			name:        "forever kr",
			gameVersion: "forever",
			region:      "kr",
			wantProfile: "profile-forever-kr",
			wantStatic:  "static-forever-kr",
			wantIsForvr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Region:      tt.region,
				GameVersion: tt.gameVersion,
			}
			if cfg.IsForever() != tt.wantIsForvr {
				t.Errorf("IsForever() = %v, want %v", cfg.IsForever(), tt.wantIsForvr)
			}
			if got := cfg.ProfileNamespace(); got != tt.wantProfile {
				t.Errorf("ProfileNamespace() = %q, want %q", got, tt.wantProfile)
			}
			if got := cfg.StaticNamespace(); got != tt.wantStatic {
				t.Errorf("StaticNamespace() = %q, want %q", got, tt.wantStatic)
			}
		})
	}
}

func TestParseGameVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "retail"},
		{"retail", "retail"},
		{"RETAIL", "retail"},
		{"  retail  ", "retail"},
		{"unknown", "retail"},
		{"forever", "forever"},
		{"FOREVER", "forever"},
		{"  forever  ", "forever"},
		{"classic_forever", "forever"},
		{"wow_forever", "forever"},
	}

	for _, tt := range tests {
		t.Run("input_"+tt.input, func(t *testing.T) {
			if got := parseGameVersion(tt.input); got != tt.want {
				t.Errorf("parseGameVersion(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestAccountMapping(t *testing.T) {
	t.Run("Retail payload mapping", func(t *testing.T) {
		retailJSON := []byte(`{
			"wow_accounts": [
				{
					"id": 1001,
					"characters": [
						{
							"name": "Cesard",
							"id": 5001,
							"level": 70,
							"realm": {
								"name": "The Maelstrom",
								"slug": "the-maelstrom"
							},
							"playable_class": {"name": "Warrior"},
							"playable_race": {"name": "Human"},
							"faction": {"type": "ALLIANCE", "name": "Alliance"},
							"protected_character": {"href": "https://eu.api.blizzard.com/profile/user/wow/protected-character/1001-5001"}
						}
					]
				}
			]
		}`)

		chars, err := parseAccountCharacters(retailJSON, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(chars) != 1 {
			t.Fatalf("expected 1 character, got %d", len(chars))
		}
		c := chars[0]
		if c.Name != "Cesard" {
			t.Errorf("Name = %q, want 'Cesard'", c.Name)
		}
		if c.FullName != "Cesard" {
			t.Errorf("FullName = %q, want 'Cesard'", c.FullName)
		}
		if c.RealmSlug != "the-maelstrom" {
			t.Errorf("RealmSlug = %q, want 'the-maelstrom'", c.RealmSlug)
		}
		if c.RealmName != "The Maelstrom" {
			t.Errorf("RealmName = %q, want 'The Maelstrom'", c.RealmName)
		}
		if c.Level != 70 {
			t.Errorf("Level = %d, want 70", c.Level)
		}
		if c.Class != "Warrior" || c.Race != "Human" || c.Faction != "ALLIANCE" {
			t.Errorf("unexpected class/race/faction: %s/%s/%s", c.Class, c.Race, c.Faction)
		}
	})

	t.Run("Forever payload with surname and ruleset", func(t *testing.T) {
		foreverJSON := []byte(`{
			"wow_accounts": [
				{
					"id": 2001,
					"characters": [
						{
							"name": "Arthas",
							"surname": "Menethil",
							"id": 6001,
							"level": 60,
							"ruleset": {
								"name": "Normal",
								"slug": "normal"
							},
							"playable_class": {"name": "Paladin"},
							"playable_race": {"name": "Human"},
							"faction": {"type": "ALLIANCE", "name": "Alliance"}
						}
					]
				}
			]
		}`)

		chars, err := parseAccountCharacters(foreverJSON, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(chars) != 1 {
			t.Fatalf("expected 1 character, got %d", len(chars))
		}
		c := chars[0]
		if c.Name != "Arthas" {
			t.Errorf("Name = %q, want 'Arthas'", c.Name)
		}
		if c.Surname != "Menethil" {
			t.Errorf("Surname = %q, want 'Menethil'", c.Surname)
		}
		if c.FullName != "Arthas Menethil" {
			t.Errorf("FullName = %q, want 'Arthas Menethil'", c.FullName)
		}
		if c.RealmName != "Normal" {
			t.Errorf("RealmName = %q, want 'Normal'", c.RealmName)
		}
		if c.RealmSlug != "normal" {
			t.Errorf("RealmSlug = %q, want 'normal'", c.RealmSlug)
		}
		if c.Level != 60 {
			t.Errorf("Level = %d, want 60", c.Level)
		}
	})

	t.Run("Forever payload with root characters and last_name", func(t *testing.T) {
		rootCharsJSON := []byte(`{
			"characters": [
				{
					"name": "Jaina",
					"last_name": "Proudmoore",
					"level": 60,
					"ruleset": {
						"name": "PvP",
						"slug": "pvp"
					},
					"playable_class": {"name": "Mage"},
					"faction": {"type": "ALLIANCE"}
				}
			]
		}`)

		chars, err := parseAccountCharacters(rootCharsJSON, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(chars) != 1 {
			t.Fatalf("expected 1 character, got %d", len(chars))
		}
		c := chars[0]
		if c.FullName != "Jaina Proudmoore" {
			t.Errorf("FullName = %q, want 'Jaina Proudmoore'", c.FullName)
		}
		if c.RealmName != "PvP" {
			t.Errorf("RealmName = %q, want 'PvP'", c.RealmName)
		}
	})

	t.Run("Forever payload concatenation does not duplicate surname", func(t *testing.T) {
		tests := []struct {
			name     string
			payload  string
			wantName string
		}{
			{
				name: "pre-combined name with identical surname",
				payload: `{
					"characters": [{"name": "Illidan Stormrage", "surname": "Stormrage", "level": 70}]
				}`,
				wantName: "Illidan Stormrage",
			},
			{
				name: "identical first and last name",
				payload: `{
					"characters": [{"name": "Menethil", "surname": "Menethil", "level": 60}]
				}`,
				wantName: "Menethil",
			},
			{
				name: "hyphenated name already containing surname",
				payload: `{
					"characters": [{"name": "Arthas-Menethil", "surname": "Menethil", "level": 60}]
				}`,
				wantName: "Arthas-Menethil",
			},
			{
				name: "case insensitive duplicate surname",
				payload: `{
					"characters": [{"name": "Arthas menethil", "surname": "Menethil", "level": 60}]
				}`,
				wantName: "Arthas menethil",
			},
			{
				name: "standard separate first and surname",
				payload: `{
					"characters": [{"name": "Uther", "surname": "Lightbringer", "level": 60}]
				}`,
				wantName: "Uther Lightbringer",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				chars, err := parseAccountCharacters([]byte(tt.payload), true)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(chars) != 1 {
					t.Fatalf("expected 1 character, got %d", len(chars))
				}
				if chars[0].FullName != tt.wantName {
					t.Errorf("FullName = %q, want %q", chars[0].FullName, tt.wantName)
				}
			})
		}
	})

	t.Run("Empty and invalid JSON payloads", func(t *testing.T) {
		emptyChars, err := parseAccountCharacters([]byte(`{}`), true)
		if err != nil {
			t.Errorf("unexpected error for empty json: %v", err)
		}
		if len(emptyChars) != 0 {
			t.Errorf("expected 0 characters, got %d", len(emptyChars))
		}

		_, err = parseAccountCharacters([]byte(`<html><body>502 Bad Gateway</body></html>`), true)
		if err == nil {
			t.Error("expected error for invalid JSON, got nil")
		}
	})
}

func TestProfileURLs(t *testing.T) {
	origConfig := config
	defer func() { config = origConfig }()

	t.Run("Retail URLs include realm and profile-{region}", func(t *testing.T) {
		config = Config{
			Region:      "eu",
			Locale:      "en_GB",
			GameVersion: "retail",
		}

		urlProfile := characterURL("the-maelstrom", "cesard", "")
		wantProfile := "https://eu.api.blizzard.com/profile/wow/character/the-maelstrom/cesard?namespace=profile-eu&locale=en_GB"
		if urlProfile != wantProfile {
			t.Errorf("profile URL = %q, want %q", urlProfile, wantProfile)
		}

		urlEquip := characterURL("the-maelstrom", "cesard", "/equipment")
		wantEquip := "https://eu.api.blizzard.com/profile/wow/character/the-maelstrom/cesard/equipment?namespace=profile-eu&locale=en_GB"
		if urlEquip != wantEquip {
			t.Errorf("equipment URL = %q, want %q", urlEquip, wantEquip)
		}

		urlRaids := characterURL("the-maelstrom", "cesard", "/encounters/raids")
		wantRaids := "https://eu.api.blizzard.com/profile/wow/character/the-maelstrom/cesard/encounters/raids?namespace=profile-eu&locale=en_GB"
		if urlRaids != wantRaids {
			t.Errorf("raids URL = %q, want %q", urlRaids, wantRaids)
		}
	})

	t.Run("Forever URLs use {name}-{surname} and never append ruleset label as surname", func(t *testing.T) {
		config = Config{
			Region:      "tw",
			Locale:      "zh_TW",
			GameVersion: "forever",
		}

		urlSlugDirect := characterURL("", "arthas-menethil", "")
		wantProfile := "https://tw.api.blizzard.com/profile/wow/character/arthas-menethil?namespace=profile-forever-tw&locale=zh_TW"
		if urlSlugDirect != wantProfile {
			t.Errorf("direct slug URL = %q, want %q", urlSlugDirect, wantProfile)
		}

		urlFullName := characterURL("", "Arthas Menethil", "")
		if urlFullName != wantProfile {
			t.Errorf("full name URL = %q, want %q", urlFullName, wantProfile)
		}

		// When ruleset labels (Normal, PvP, Hardcore, custom) are passed, they must NEVER be appended as surname
		urlWithRulesetNormal := characterURL("Normal", "Arthas Menethil", "/equipment")
		wantEquip := "https://tw.api.blizzard.com/profile/wow/character/arthas-menethil/equipment?namespace=profile-forever-tw&locale=zh_TW"
		if urlWithRulesetNormal != wantEquip {
			t.Errorf("equipment URL with Normal ruleset = %q, want %q", urlWithRulesetNormal, wantEquip)
		}

		urlSingleNameRulesetNormal := characterURL("Normal", "Arthas", "")
		wantSingleProfile := "https://tw.api.blizzard.com/profile/wow/character/arthas?namespace=profile-forever-tw&locale=zh_TW"
		if urlSingleNameRulesetNormal != wantSingleProfile {
			t.Errorf("single name with Normal ruleset = %q, want %q", urlSingleNameRulesetNormal, wantSingleProfile)
		}

		urlSingleNameRulesetPvP := characterURL("PvP", "Arthas", "")
		if urlSingleNameRulesetPvP != wantSingleProfile {
			t.Errorf("single name with PvP ruleset = %q, want %q", urlSingleNameRulesetPvP, wantSingleProfile)
		}

		urlSingleNameRulesetHardcore := characterURL("Hardcore", "Arthas", "")
		if urlSingleNameRulesetHardcore != wantSingleProfile {
			t.Errorf("single name with Hardcore ruleset = %q, want %q", urlSingleNameRulesetHardcore, wantSingleProfile)
		}

		urlSingleNameCustomRuleset := characterURL("Roleplaying PvP (Seasonal)", "Arthas", "")
		if urlSingleNameCustomRuleset != wantSingleProfile {
			t.Errorf("single name with custom ruleset = %q, want %q", urlSingleNameCustomRuleset, wantSingleProfile)
		}

		// Full name with duplicate surname in slug is sanitized
		urlDedupSlug := characterURL("Normal", "Arthas Menethil Menethil", "")
		if urlDedupSlug != wantProfile {
			t.Errorf("duplicate surname slug = %q, want %q", urlDedupSlug, wantProfile)
		}

		urlRaids := characterURL("", "Arthas Menethil", "/encounters/raids")
		wantRaids := "https://tw.api.blizzard.com/profile/wow/character/arthas-menethil/encounters/raids?namespace=profile-forever-tw&locale=zh_TW"
		if urlRaids != wantRaids {
			t.Errorf("raids URL = %q, want %q", urlRaids, wantRaids)
		}

		urlCurrencies := characterURL("", "Arthas Menethil", "/currencies")
		wantCurr := "https://tw.api.blizzard.com/profile/wow/character/arthas-menethil/currencies?namespace=profile-forever-tw&locale=zh_TW"
		if urlCurrencies != wantCurr {
			t.Errorf("currencies URL = %q, want %q", urlCurrencies, wantCurr)
		}

		urlAch := characterURL("", "Arthas Menethil", "/achievements")
		wantAch := "https://tw.api.blizzard.com/profile/wow/character/arthas-menethil/achievements?namespace=profile-forever-tw&locale=zh_TW"
		if urlAch != wantAch {
			t.Errorf("achievements URL = %q, want %q", urlAch, wantAch)
		}
	})
}

func TestErrorHandling(t *testing.T) {
	origConfig := config
	origOAuth2Config := oauth2Config
	origUserToken := userToken
	origSummary := accountSummary
	origTransport := http.DefaultTransport
	defer func() {
		config = origConfig
		oauth2Config = origOAuth2Config
		tokenMutex.Lock()
		userToken = origUserToken
		tokenMutex.Unlock()
		summaryMutex.Lock()
		accountSummary = origSummary
		summaryMutex.Unlock()
		http.DefaultTransport = origTransport
	}()

	testCases := []struct {
		name        string
		statusCode  int
		gameVersion string
		region      string
	}{
		{
			name:        "Forever HTTP 403 Forbidden asserts AccountSummary.Error and no collection calls",
			statusCode:  http.StatusForbidden,
			gameVersion: "forever",
			region:      "tw",
		},
		{
			name:        "Forever HTTP 404 Not Found asserts AccountSummary.Error and no collection calls",
			statusCode:  http.StatusNotFound,
			gameVersion: "forever",
			region:      "tw",
		},
		{
			name:        "Retail HTTP 403 Forbidden asserts AccountSummary.Error and no collection calls",
			statusCode:  http.StatusForbidden,
			gameVersion: "retail",
			region:      "eu",
		},
		{
			name:        "Retail HTTP 404 Not Found asserts AccountSummary.Error and no collection calls",
			statusCode:  http.StatusNotFound,
			gameVersion: "retail",
			region:      "eu",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var collectionCalls int32
			var accountsCalls int32
			var sawAuthHeader bool

			config = Config{
				Region:      tc.region,
				Locale:      "en_GB",
				GameVersion: tc.gameVersion,
			}
			oauth2Config = &oauth2.Config{
				ClientID:     "mock-client-id",
				ClientSecret: "mock-client-secret",
				Endpoint: oauth2.Endpoint{
					AuthURL:  "https://oauth.battle.net/authorize",
					TokenURL: "https://oauth.battle.net/token",
				},
			}
			tokenMutex.Lock()
			userToken = &oauth2.Token{
				AccessToken: "fake-oauth-token-xyz-123",
				TokenType:   "Bearer",
				Expiry:      time.Now().Add(1 * time.Hour),
			}
			tokenMutex.Unlock()

			summaryMutex.Lock()
			accountSummary = AccountSummary{}
			summaryMutex.Unlock()

			http.DefaultTransport = &mockTransport{
				roundTrip: func(req *http.Request) (*http.Response, error) {
					urlStr := req.URL.String()
					if strings.Contains(urlStr, "/collections/") {
						atomic.AddInt32(&collectionCalls, 1)
					}
					if req.Header.Get("Authorization") == "Bearer fake-oauth-token-xyz-123" {
						sawAuthHeader = true
					}
					if strings.Contains(urlStr, "/profile/user/wow") {
						atomic.AddInt32(&accountsCalls, 1)
						return &http.Response{
							StatusCode: tc.statusCode,
							Status:     fmt.Sprintf("%d %s", tc.statusCode, http.StatusText(tc.statusCode)),
							Header:     make(http.Header),
							Body:       io.NopCloser(strings.NewReader(fmt.Sprintf(`{"code":%d,"detail":"mock api error"}`, tc.statusCode))),
							Request:    req,
						}, nil
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Status:     "200 OK",
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader(`{}`)),
						Request:    req,
					}, nil
				},
			}

			// Call real fetchCharacterData()
			fetchCharacterData()

			summaryMutex.RLock()
			summary := accountSummary
			summaryMutex.RUnlock()

			wantErr := fmt.Sprintf("HTTP %d", tc.statusCode)
			if summary.Error != wantErr {
				t.Errorf("accountSummary.Error = %q, want %q", summary.Error, wantErr)
			}
			if len(summary.Characters) != 0 {
				t.Errorf("expected 0 characters, got %d", len(summary.Characters))
			}
			if collectionCalls != 0 {
				t.Errorf("expected 0 collection calls, got %d", collectionCalls)
			}
			if accountsCalls != 1 {
				t.Errorf("expected 1 accounts endpoint call, got %d", accountsCalls)
			}
			if !sawAuthHeader {
				t.Error("expected mock transport to receive fake OAuth token in Authorization header")
			}
		})
	}
}

func TestRetailRegression(t *testing.T) {
	origConfig := config
	defer func() { config = origConfig }()

	// Verify default config has GameVersion=retail behavior
	config = Config{
		Region: "eu",
		Locale: "en_GB",
	}

	if config.IsForever() {
		t.Error("default config should not be Forever mode")
	}
	if config.ProfileNamespace() != "profile-eu" {
		t.Errorf("default profile namespace = %q, want 'profile-eu'", config.ProfileNamespace())
	}
	if config.StaticNamespace() != "static-eu" {
		t.Errorf("default static namespace = %q, want 'static-eu'", config.StaticNamespace())
	}

	url := characterURL("silvermoon", "testchar", "")
	if !strings.Contains(url, "/profile/wow/character/silvermoon/testchar") {
		t.Errorf("expected retail URL format with realm and name, got %q", url)
	}
	if !strings.Contains(url, "namespace=profile-eu") {
		t.Errorf("expected retail namespace, got %q", url)
	}

	// Verify Retail retains Realm as realm SLUG ("the-maelstrom") rather than display name ("The Maelstrom")
	t.Run("Retail preserves CharacterStats.Realm as realm slug", func(t *testing.T) {
		retailChar := DiscoveredCharacter{
			Name:      "Cesard",
			RealmSlug: "the-maelstrom",
			RealmName: "The Maelstrom",
			Level:     70,
		}

		var stats CharacterStats
		stats.Name = retailChar.Name
		if config.IsForever() {
			if retailChar.RealmName != "" {
				stats.Realm = retailChar.RealmName
			} else {
				stats.Realm = retailChar.RealmSlug
			}
		} else {
			if retailChar.RealmSlug != "" {
				stats.Realm = retailChar.RealmSlug
			} else {
				stats.Realm = retailChar.RealmName
			}
		}

		if stats.Realm != "the-maelstrom" {
			t.Errorf("stats.Realm = %q, want realm slug 'the-maelstrom'", stats.Realm)
		}

		// Ensure downstream characterURL produces slug-based URL without spaces
		charLink := characterURL(stats.Realm, stats.Name, "/equipment")
		wantLink := "https://eu.api.blizzard.com/profile/wow/character/the-maelstrom/cesard/equipment?namespace=profile-eu&locale=en_GB"
		if charLink != wantLink {
			t.Errorf("downstream equipment URL = %q, want %q", charLink, wantLink)
		}
	})
}
