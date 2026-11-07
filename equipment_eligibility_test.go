package main

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestEquipmentEligibility(t *testing.T) {
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
		eqState.Lock()
		eqState.data = nil
		eqState.fetched = time.Time{}
		eqState.loading = false
		eqState.Unlock()
	}()

	config = Config{
		Region:      "tw",
		Locale:      "zh_TW",
		GameVersion: "forever",
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
		AccessToken: "fake-oauth-token-equipment-test",
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(1 * time.Hour),
	}
	tokenMutex.Unlock()

	// Setup AccountSummary fixtures:
	// 1. Level 16 Forever character with successful profile (eligible)
	// 2. Below level 10 character (level 8) (excluded)
	// 3. Level 20 character with profile error (excluded)
	summaryMutex.Lock()
	accountSummary = AccountSummary{
		Characters: []CharacterStats{
			{
				Name:      "Arthas Menethil",
				Realm:     "Normal",
				Level:     16,
				ItemLevel: 25,
			},
			{
				Name:      "Lowlevel Scout",
				Realm:     "Normal",
				Level:     8,
				ItemLevel: 10,
			},
			{
				Name:      "Errored Champion",
				Realm:     "Normal",
				Level:     20,
				ItemLevel: 30,
				Error:     "character profile not found",
			},
		},
	}
	summaryMutex.Unlock()

	var equipmentCalls int32
	var mediaCalls int32
	var statsCalls int32
	var sawAuthHeader bool
	var requestedEquipmentURL string

	mockEqJSON := `{
		"character": {
			"name": "Arthas Menethil",
			"id": 12345
		},
		"equipped_items": [
			{
				"item": { "id": 123456 },
				"slot": { "type": "HEAD", "name": "Head" },
				"quality": { "type": "RARE", "name": "Rare" },
				"name": "Forever Iron Helm",
				"level": { "value": 25 }
			}
		]
	}`

	mockMediaJSON := `{
		"assets": [
			{ "key": "icon", "value": "https://render.worldofwarcraft.com/icons/56/inv_helmet_01.jpg" }
		]
	}`

	mockStatsJSON := `{
		"melee_crit": { "value": 5.0, "rating": 10 },
		"stamina": { "effective": 40 },
		"strength": { "effective": 35 }
	}`

	http.DefaultTransport = &mockTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			urlStr := req.URL.String()
			if req.Header.Get("Authorization") == "Bearer fake-oauth-token-equipment-test" {
				sawAuthHeader = true
			}

			if strings.Contains(urlStr, "/equipment") {
				atomic.AddInt32(&equipmentCalls, 1)
				requestedEquipmentURL = urlStr
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(mockEqJSON)),
					Request:    req,
				}, nil
			}

			if strings.Contains(urlStr, "/media/item/") {
				atomic.AddInt32(&mediaCalls, 1)
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(mockMediaJSON)),
					Request:    req,
				}, nil
			}

			if strings.Contains(urlStr, "/statistics") {
				atomic.AddInt32(&statsCalls, 1)
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(mockStatsJSON)),
					Request:    req,
				}, nil
			}

			return &http.Response{
				StatusCode: http.StatusNotFound,
				Status:     "404 Not Found",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"code":404}`)),
				Request:    req,
			}, nil
		},
	}

	// Reset eqState
	eqState.Lock()
	eqState.data = nil
	eqState.fetched = time.Time{}
	eqState.loading = false
	eqState.Unlock()

	// Execute actual fetchAllEquipment()
	fetchAllEquipment()

	if !sawAuthHeader {
		t.Error("expected mock transport to receive Bearer authorization header")
	}

	// Assert endpoint requested
	if atomic.LoadInt32(&equipmentCalls) != 1 {
		t.Fatalf("expected 1 equipment call, got %d", equipmentCalls)
	}

	wantEndpoint := "https://tw.api.blizzard.com/profile/wow/character/arthas-menethil/equipment?namespace=profile-forever-tw&locale=zh_TW"
	if requestedEquipmentURL != wantEndpoint {
		t.Errorf("equipment URL = %q, want %q", requestedEquipmentURL, wantEndpoint)
	}

	// Assert eqState.data is nonempty and contains exactly the level 16 character
	eqState.Lock()
	defer eqState.Unlock()

	if len(eqState.data) != 1 {
		t.Fatalf("expected eqState.data length 1, got %d", len(eqState.data))
	}

	gotChar := eqState.data[0]
	if gotChar.Name != "Arthas Menethil" {
		t.Errorf("character name = %q, want 'Arthas Menethil'", gotChar.Name)
	}
	if gotChar.Level != 16 {
		t.Errorf("character level = %d, want 16", gotChar.Level)
	}
	if gotChar.Err != "" {
		t.Errorf("character err = %q, want empty", gotChar.Err)
	}
	if len(gotChar.Items) != 1 {
		t.Fatalf("expected 1 equipped item, got %d", len(gotChar.Items))
	}
	item, ok := gotChar.Items["HEAD"]
	if !ok {
		t.Fatal("expected HEAD item in Items map")
	}
	if item.Name != "Forever Iron Helm" {
		t.Errorf("item name = %q, want 'Forever Iron Helm'", item.Name)
	}
	if item.Ilvl != 25 {
		t.Errorf("item ilvl = %d, want 25", item.Ilvl)
	}

	// Assert that endgame enchant checks did not flag level 16 character as missing enchant
	// because level 16 is below rules.MinLevel (default 80)
	for _, w := range gotChar.Warnings {
		if strings.Contains(w, "missing enchant") {
			t.Errorf("unexpected enchant warning for level 16 character: %q", w)
		}
	}
}
