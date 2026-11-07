package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// ── Test 1: Independent Sessions & Data Isolation ─────────────────────────────
// Tests that two independent clients cannot access each other's data across all endpoints.
func TestIndependentSessions_DataIsolation(t *testing.T) {
	// Initialize global OAuth config
	oauth2Config = &oauth2.Config{
		ClientID: "test-client-id",
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://oauth.battle.net/authorize",
			TokenURL: "https://oauth.battle.net/token",
		},
	}

	// Create Session A
	sessionA := globalSessions.Create()
	sessionA.mu.Lock()
	sessionA.AccountID = "bnet-account-111"
	sessionA.BattleTag = "Thrall#1111"
	sessionA.Authenticated = true
	sessionA.Token = &oauth2.Token{
		AccessToken: "token-client-a",
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(24 * time.Hour),
	}
	sessionA.Summary = AccountSummary{
		Authenticated: true,
		TotalGold:     500000000, // 50,000g
		Characters: []CharacterStats{
			{Name: "Thrall", Realm: "Orgrimmar", Level: 80, ItemLevel: 620, Gold: 500000000, Faction: "HORDE"},
		},
	}
	sessionA.Equipment = &EquipmentSessionState{
		Data: []EQChar{
			{Name: "Thrall", Realm: "Orgrimmar", Level: 80, Ilvl: 620, Items: map[string]EquippedItem{
				"MAIN_HAND": {Name: "Doomhammer", Quality: "artifact"},
			}},
		},
		Fetched: time.Now(),
	}
	sessionA.MythicPlus = &MythicPlusSessionState{
		Data: []MPChar{
			{Name: "Thrall", Realm: "Orgrimmar", Rating: 2800.5, HasData: true},
		},
		Fetched: time.Now(),
	}
	sessionA.Achievements = map[string]*achProfileEntry{
		"thrall-orgrimmar": {
			fetched: time.Now(),
			view: &achView{
				Points: 25000,
				Groups: []AchGroup{
					{Name: "Warchief Achievements", Total: 10, Done: 10, Items: []AchItem{{Name: "Defender of the Horde", Status: "done"}}},
				},
			},
		},
	}
	sessionA.mu.Unlock()

	// Create Session B
	sessionB := globalSessions.Create()
	sessionB.mu.Lock()
	sessionB.AccountID = "bnet-account-222"
	sessionB.BattleTag = "Jaina#2222"
	sessionB.Authenticated = true
	sessionB.Token = &oauth2.Token{
		AccessToken: "token-client-b",
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(24 * time.Hour),
	}
	sessionB.Summary = AccountSummary{
		Authenticated: true,
		TotalGold:     990000000, // 99,000g
		Characters: []CharacterStats{
			{Name: "Jaina", Realm: "Dalaran", Level: 80, ItemLevel: 630, Gold: 990000000, Faction: "ALLIANCE"},
		},
	}
	sessionB.Equipment = &EquipmentSessionState{
		Data: []EQChar{
			{Name: "Jaina", Realm: "Dalaran", Level: 80, Ilvl: 630, Items: map[string]EquippedItem{
				"MAIN_HAND": {Name: "Staff of Antonidas", Quality: "epic"},
			}},
		},
		Fetched: time.Now(),
	}
	sessionB.MythicPlus = &MythicPlusSessionState{
		Data: []MPChar{
			{Name: "Jaina", Realm: "Dalaran", Rating: 3200.0, HasData: true},
		},
		Fetched: time.Now(),
	}
	sessionB.Achievements = map[string]*achProfileEntry{
		"jaina-dalaran": {
			fetched: time.Now(),
			view: &achView{
				Points: 31000,
				Groups: []AchGroup{
					{Name: "Archmage Achievements", Total: 15, Done: 15, Items: []AchItem{{Name: "Lady of Theramore", Status: "done"}}},
				},
			},
		},
	}
	sessionB.mu.Unlock()

	defer func() {
		globalSessions.Delete(sessionA.ID)
		globalSessions.Delete(sessionB.ID)
	}()

	// Helper to send request with cookie
	makeReq := func(handler http.HandlerFunc, method, path string, sessionID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		if sessionID != "" {
			req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sessionID})
		}
		rec := httptest.NewRecorder()
		handler(rec, req)
		return rec
	}

	// 1. Home page isolation
	recA := makeReq(handleHome, "GET", "/", sessionA.ID)
	bodyA := recA.Body.String()
	if !strings.Contains(bodyA, "Thrall") || strings.Contains(bodyA, "Jaina") {
		t.Errorf("Home page A isolation failed: expected Thrall, found Jaina or missing Thrall")
	}

	recB := makeReq(handleHome, "GET", "/", sessionB.ID)
	bodyB := recB.Body.String()
	if !strings.Contains(bodyB, "Jaina") || strings.Contains(bodyB, "Thrall") {
		t.Errorf("Home page B isolation failed: expected Jaina, found Thrall or missing Jaina")
	}

	// Unauthenticated client should see login prompt and neither character
	recAnon := makeReq(handleHome, "GET", "/", "")
	bodyAnon := recAnon.Body.String()
	if strings.Contains(bodyAnon, "Thrall") || strings.Contains(bodyAnon, "Jaina") {
		t.Errorf("Home page anonymous isolation failed: showed characters to unauthenticated client")
	}
	if !strings.Contains(bodyAnon, "/login") {
		t.Errorf("Home page anonymous should contain login link")
	}

	// 2. /api/stats isolation
	recStatsA := makeReq(handleAPIStats, "GET", "/api/stats", sessionA.ID)
	var summaryRespA AccountSummary
	if err := json.NewDecoder(recStatsA.Body).Decode(&summaryRespA); err != nil {
		t.Fatalf("failed to decode stats A: %v", err)
	}
	if len(summaryRespA.Characters) != 1 || summaryRespA.Characters[0].Name != "Thrall" {
		t.Errorf("API stats A: expected Thrall, got %+v", summaryRespA.Characters)
	}

	recStatsB := makeReq(handleAPIStats, "GET", "/api/stats", sessionB.ID)
	var summaryRespB AccountSummary
	if err := json.NewDecoder(recStatsB.Body).Decode(&summaryRespB); err != nil {
		t.Fatalf("failed to decode stats B: %v", err)
	}
	if len(summaryRespB.Characters) != 1 || summaryRespB.Characters[0].Name != "Jaina" {
		t.Errorf("API stats B: expected Jaina, got %+v", summaryRespB.Characters)
	}

	// 3. Vault & Roster & Professions isolation
	for _, page := range []struct {
		name    string
		handler http.HandlerFunc
		path    string
	}{
		{"Vault", handleVault, "/vault"},
		{"Roster", handleRoster, "/roster"},
		{"Professions", handleProfessions, "/professions"},
	} {
		respA := makeReq(page.handler, "GET", page.path, sessionA.ID).Body.String()
		if !strings.Contains(respA, "Thrall") || strings.Contains(respA, "Jaina") {
			t.Errorf("%s A isolation failed: expected Thrall, got Jaina", page.name)
		}
		respB := makeReq(page.handler, "GET", page.path, sessionB.ID).Body.String()
		if !strings.Contains(respB, "Jaina") || strings.Contains(respB, "Thrall") {
			t.Errorf("%s B isolation failed: expected Jaina, got Thrall", page.name)
		}
	}

	// 4. Equipment isolation
	eqA := makeReq(handleEquipment, "GET", "/equipment?c=thrall-orgrimmar", sessionA.ID).Body.String()
	if !strings.Contains(eqA, "Doomhammer") || strings.Contains(eqA, "Staff of Antonidas") {
		t.Errorf("Equipment A isolation failed: expected Doomhammer, found Antonidas")
	}

	eqB := makeReq(handleEquipment, "GET", "/equipment?c=jaina-dalaran", sessionB.ID).Body.String()
	if !strings.Contains(eqB, "Staff of Antonidas") || strings.Contains(eqB, "Doomhammer") {
		t.Errorf("Equipment B isolation failed: expected Staff of Antonidas, found Doomhammer")
	}

	// 5. Mythic+ isolation
	mpA := makeReq(handleMythicPlus, "GET", "/mythic-plus", sessionA.ID).Body.String()
	if !strings.Contains(mpA, "Thrall") || strings.Contains(mpA, "Jaina") {
		t.Errorf("Mythic+ A isolation failed: expected Thrall, found Jaina")
	}

	mpB := makeReq(handleMythicPlus, "GET", "/mythic-plus", sessionB.ID).Body.String()
	if !strings.Contains(mpB, "Jaina") || strings.Contains(mpB, "Thrall") {
		t.Errorf("Mythic+ B isolation failed: expected Jaina, found Thrall")
	}

	// 6. Debug endpoints unauthorized for unauthenticated client
	anonDebug := makeReq(handleDebugAccounts, "GET", "/debug/accounts", "")
	if anonDebug.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for anonymous debug access, got %d", anonDebug.Code)
	}
}

// ── Test 2: OAuth State Isolation & One-Time Use ─────────────────────────────
// Tests that OAuth states are generated uniquely per login, isolated per session,
// and strictly single-use (cannot be replayed or shared).
func TestOAuthStateIsolation_AndOneTimeUse(t *testing.T) {
	oauth2Config = &oauth2.Config{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://localhost:8080/callback",
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://oauth.battle.net/authorize",
			TokenURL: "https://oauth.battle.net/token",
		},
	}

	// Client 1 initiates login
	recLogin1 := httptest.NewRecorder()
	reqLogin1 := httptest.NewRequest("GET", "/login", nil)
	handleLogin(recLogin1, reqLogin1)

	cookie1 := recLogin1.Result().Cookies()
	var sessionID1 string
	for _, c := range cookie1 {
		if c.Name == SessionCookieName {
			sessionID1 = c.Value
		}
	}
	if sessionID1 == "" {
		t.Fatalf("Client 1 /login did not set session cookie")
	}

	loc1, err := url.Parse(recLogin1.Header().Get("Location"))
	if err != nil {
		t.Fatalf("invalid redirect location: %v", err)
	}
	state1 := loc1.Query().Get("state")
	if state1 == "" {
		t.Fatalf("Client 1 /login missing state query parameter")
	}

	// Client 2 initiates login
	recLogin2 := httptest.NewRecorder()
	reqLogin2 := httptest.NewRequest("GET", "/login", nil)
	handleLogin(recLogin2, reqLogin2)

	cookie2 := recLogin2.Result().Cookies()
	var sessionID2 string
	for _, c := range cookie2 {
		if c.Name == SessionCookieName {
			sessionID2 = c.Value
		}
	}
	if sessionID2 == "" {
		t.Fatalf("Client 2 /login did not set session cookie")
	}

	loc2, err := url.Parse(recLogin2.Header().Get("Location"))
	if err != nil {
		t.Fatalf("invalid redirect location: %v", err)
	}
	state2 := loc2.Query().Get("state")
	if state2 == "" {
		t.Fatalf("Client 2 /login missing state query parameter")
	}

	// Verify states are distinct
	if state1 == state2 {
		t.Fatalf("OAuth states must be unique per login, got identical states: %s", state1)
	}

	// Cross-session state attack: Client 2 tries to callback using Client 1's state
	recCross := httptest.NewRecorder()
	reqCross := httptest.NewRequest("GET", "/callback?code=mock_code&state="+url.QueryEscape(state1), nil)
	reqCross.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sessionID2})
	handleCallback(recCross, reqCross)

	if recCross.Code != http.StatusBadRequest {
		t.Errorf("Cross-session OAuth state should be rejected with 400 Bad Request, got %d", recCross.Code)
	}

	// Client without session cookie trying to callback
	recNoCookie := httptest.NewRecorder()
	reqNoCookie := httptest.NewRequest("GET", "/callback?code=mock_code&state="+url.QueryEscape(state1), nil)
	handleCallback(recNoCookie, reqNoCookie)
	if recNoCookie.Code != http.StatusBadRequest {
		t.Errorf("Callback without cookie should be rejected with 400, got %d", recNoCookie.Code)
	}

	// Test One-Time Use / Replay Attack on session 1
	session1 := globalSessions.Get(sessionID1)
	if session1 == nil {
		t.Fatalf("session 1 not found in store")
	}

	// First consumption: should succeed
	validFirst := session1.ConsumePendingOAuthState(state1)
	if !validFirst {
		t.Errorf("First consumption of pending OAuth state should succeed")
	}

	// Second consumption (replay): MUST fail
	validSecond := session1.ConsumePendingOAuthState(state1)
	if validSecond {
		t.Errorf("Replaying same OAuth state MUST fail")
	}

	// Expired state test
	session1.SetPendingOAuthState("test-expired-state")
	session1.mu.Lock()
	session1.PendingOAuthExpiry = time.Now().Add(-1 * time.Minute) // expired
	session1.mu.Unlock()

	if session1.ConsumePendingOAuthState("test-expired-state") {
		t.Errorf("Expired OAuth state MUST NOT be accepted")
	}
}

// ── Test 3: Logout Isolation ──────────────────────────────────────────────────
// Tests that logging out Client A does not log out or alter Client B's session.
func TestLogoutIsolation(t *testing.T) {
	// Create Session A
	sessionA := globalSessions.Create()
	sessionA.mu.Lock()
	sessionA.AccountID = "acc-A"
	sessionA.Authenticated = true
	sessionA.Token = &oauth2.Token{AccessToken: "tok-A", Expiry: time.Now().Add(time.Hour)}
	sessionA.Summary = AccountSummary{Authenticated: true, Characters: []CharacterStats{{Name: "UserA"}}}
	sessionA.mu.Unlock()

	// Create Session B
	sessionB := globalSessions.Create()
	sessionB.mu.Lock()
	sessionB.AccountID = "acc-B"
	sessionB.Authenticated = true
	sessionB.Token = &oauth2.Token{AccessToken: "tok-B", Expiry: time.Now().Add(time.Hour)}
	sessionB.Summary = AccountSummary{Authenticated: true, Characters: []CharacterStats{{Name: "UserB"}}}
	sessionB.mu.Unlock()

	defer func() {
		globalSessions.Delete(sessionA.ID)
		globalSessions.Delete(sessionB.ID)
	}()

	// Client A logs out
	recLogoutA := httptest.NewRecorder()
	reqLogoutA := httptest.NewRequest("GET", "/logout", nil)
	reqLogoutA.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sessionA.ID})
	handleLogout(recLogoutA, reqLogoutA)

	if recLogoutA.Code != http.StatusSeeOther {
		t.Errorf("Logout should redirect with 303, got %d", recLogoutA.Code)
	}

	// Client A session should now be deleted from store
	if globalSessions.Get(sessionA.ID) != nil {
		t.Errorf("Session A should be removed from session store after logout")
	}

	// Cookie for Client A should be cleared (MaxAge < 0)
	var cookieCleared bool
	for _, c := range recLogoutA.Result().Cookies() {
		if c.Name == SessionCookieName && c.MaxAge < 0 {
			cookieCleared = true
		}
	}
	if !cookieCleared {
		t.Errorf("Logout response should expire the session cookie")
	}

	// Client B MUST STILL BE FULLY AUTHENTICATED
	sessionBFromStore := globalSessions.Get(sessionB.ID)
	if sessionBFromStore == nil {
		t.Fatalf("Client B session was improperly deleted")
	}
	if !sessionBFromStore.IsAuthenticated() {
		t.Errorf("Client B session should still be authenticated")
	}

	// Verify Client B home page still renders UserB data
	recHomeB := httptest.NewRecorder()
	reqHomeB := httptest.NewRequest("GET", "/", nil)
	reqHomeB.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sessionB.ID})
	handleHome(recHomeB, reqHomeB)

	if !strings.Contains(recHomeB.Body.String(), "UserB") {
		t.Errorf("Client B home page should still display UserB data after Client A logged out")
	}
}

// ── Test 4: Refresh Isolation ─────────────────────────────────────────────────
// Tests that refreshing Client A does not touch or clear Client B's caches.
func TestRefreshIsolation(t *testing.T) {
	// Setup Session A with cached equipment
	sessionA := globalSessions.Create()
	sessionA.mu.Lock()
	sessionA.Authenticated = true
	sessionA.Token = &oauth2.Token{AccessToken: "tok-A", Expiry: time.Now().Add(time.Hour)}
	sessionA.Summary = AccountSummary{Authenticated: true}
	sessionA.Equipment = &EquipmentSessionState{
		Data:    []EQChar{{Name: "CharA"}},
		Fetched: time.Now(),
	}
	sessionA.mu.Unlock()

	// Setup Session B with cached equipment
	cachedTimeB := time.Now().Add(-5 * time.Minute)
	sessionB := globalSessions.Create()
	sessionB.mu.Lock()
	sessionB.Authenticated = true
	sessionB.Token = &oauth2.Token{AccessToken: "tok-B", Expiry: time.Now().Add(time.Hour)}
	sessionB.Summary = AccountSummary{Authenticated: true}
	sessionB.Equipment = &EquipmentSessionState{
		Data:    []EQChar{{Name: "CharB"}},
		Fetched: cachedTimeB,
	}
	sessionB.mu.Unlock()

	defer func() {
		globalSessions.Delete(sessionA.ID)
		globalSessions.Delete(sessionB.ID)
	}()

	// Client A performs refresh
	recRefreshA := httptest.NewRecorder()
	reqRefreshA := httptest.NewRequest("POST", "/refresh", nil)
	reqRefreshA.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sessionA.ID})
	handleRefresh(recRefreshA, reqRefreshA)

	if recRefreshA.Code != http.StatusSeeOther {
		t.Errorf("Refresh should redirect, got %d", recRefreshA.Code)
	}

	// Client A's equipment cache should be marked cleared (fetched is zero)
	sessionA.Equipment.RLock()
	fetchedA := sessionA.Equipment.Fetched
	sessionA.Equipment.RUnlock()
	if !fetchedA.IsZero() {
		t.Errorf("Client A cache should be cleared on refresh")
	}

	// Client B's equipment cache MUST REMAIN UNTOUCHED
	sessionB.Equipment.RLock()
	fetchedB := sessionB.Equipment.Fetched
	dataB := sessionB.Equipment.Data
	sessionB.Equipment.RUnlock()
	if !fetchedB.Equal(cachedTimeB) {
		t.Errorf("Client B cache was altered during Client A's refresh: got %v, want %v", fetchedB, cachedTimeB)
	}
	if len(dataB) != 1 || dataB[0].Name != "CharB" {
		t.Errorf("Client B cached data was altered")
	}
}

// ── Test 5: Identity Verification from Battle.net ────────────────────────────
// Tests that user identity is strictly verified from Battle.net API responses and never user input.
func TestIdentityVerification_BattleNetServerOnly(t *testing.T) {
	origTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = origTransport }()

	// 1. Success case: userinfo returns sub and battletag
	mockClient := &http.Client{
		Transport: &mockTransport{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.Path, "userinfo") {
					// Verify that Authorization header was sent with Bearer token
					auth := req.Header.Get("Authorization")
					if auth != "Bearer valid-token-123" {
						return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader("unauthorized"))}, nil
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader(`{"sub":"987654321","battletag":"Thrall#1234"}`)),
					}, nil
				}
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("{}"))}, nil
			},
		},
	}

	token := &oauth2.Token{AccessToken: "valid-token-123"}
	ident, err := verifyBattleNetIdentity(context.Background(), mockClient, token)
	if err != nil {
		t.Fatalf("verifyBattleNetIdentity failed: %v", err)
	}
	if ident.AccountID != "987654321" {
		t.Errorf("expected AccountID '987654321', got %q", ident.AccountID)
	}
	if ident.BattleTag != "Thrall#1234" {
		t.Errorf("expected BattleTag 'Thrall#1234', got %q", ident.BattleTag)
	}

	// 2. Regression: Profile fallback is strictly prohibited.
	// When userinfo fails, /profile/user/wow MUST NOT authenticate the session and MUST fail closed.
	mockClientFallback := &http.Client{
		Transport: &mockTransport{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.Path, "userinfo") {
					return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("{}"))}, nil
				}
				if strings.Contains(req.URL.Path, "/profile/user/wow") {
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader(`{"id":555444333,"wow_accounts":[]}`)),
					}, nil
				}
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("{}"))}, nil
			},
		},
	}

	_, errFallback := verifyBattleNetIdentity(context.Background(), mockClientFallback, token)
	if errFallback == nil {
		t.Fatalf("expected error when userinfo fails: profile fallback MUST NOT authenticate user")
	}

	// 3. Userinfo missing required 'sub' identifier: MUST fail closed even if 'id' number is present
	mockClientNoSub := &http.Client{
		Transport: &mockTransport{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.Path, "userinfo") {
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader(`{"id":123456,"battletag":"NoSub#1234"}`)),
					}, nil
				}
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("{}"))}, nil
			},
		},
	}
	_, errNoSub := verifyBattleNetIdentity(context.Background(), mockClientNoSub, token)
	if errNoSub == nil {
		t.Errorf("userinfo without 'sub' claim MUST fail closed, got success")
	}

	// 4. Identity consistency: verified AccountID must exactly match canonical 'sub' string
	// and never be contaminated by game profile IDs.
	mockClientConsistent := &http.Client{
		Transport: &mockTransport{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.Path, "userinfo") {
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader(`{"sub":"canonical-sub-999","battletag":"Consistent#1234"}`)),
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"id":7777777,"wow_accounts":[]}`)),
				}, nil
			},
		},
	}
	identConsistent, err := verifyBattleNetIdentity(context.Background(), mockClientConsistent, token)
	if err != nil {
		t.Fatalf("unexpected error for valid userinfo: %v", err)
	}
	if identConsistent.AccountID != "canonical-sub-999" {
		t.Errorf("expected canonical sub AccountID 'canonical-sub-999', got %q", identConsistent.AccountID)
	}

	// 3. Failure case: Battle.net rejects authentication
	mockClientFail := &http.Client{
		Transport: &mockTransport{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader("bad token"))}, nil
			},
		},
	}
	_, err = verifyBattleNetIdentity(context.Background(), mockClientFail, token)
	if err == nil {
		t.Errorf("expected error when Battle.net rejects identity verification, got nil")
	}
}

// ── Test 6: Database History Account Isolation & Unowned Privacy ─────────────
// Tests that:
// 1. Account A sees only Account A's history.
// 2. Account B sees only Account B's history.
// 3. Existing unowned history (account_id IS NULL) is NEVER returned to any account.
// 4. Missing/unauthenticated query returns empty history.
// 5. If db == nil, functions return empty history gracefully without error.
func TestHistoryAccountIsolation_AndUnownedHistoryPrivacy(t *testing.T) {
	// Part A: db == nil (Optional database absent)
	origDB := db
	db = nil
	defer func() { db = origDB }()

	hNil, errNil := getAccountHistory("acc-123")
	if errNil != nil || len(hNil.Labels) != 0 {
		t.Errorf("getAccountHistory with nil db should return empty without error, got err=%v", errNil)
	}
	cNil, errCNil := getCharacterHistories("acc-123")
	if errCNil != nil || cNil != nil {
		t.Errorf("getCharacterHistories with nil db should return nil without error, got err=%v", errCNil)
	}
	// Calling saveSnapshot with nil db should not panic
	saveSnapshot(AccountSummary{}, "acc-123")

	// Part B: Mock SQL Database to verify queries and unowned history isolation
	mockDB := setupMockHistoryDB(t)
	db = mockDB
	defer mockDB.Close()

	// 1. Unauthenticated query (empty accountID) -> returns empty
	hAnon, err := getAccountHistory("")
	if err != nil || len(hAnon.Labels) != 0 {
		t.Errorf("anonymous getAccountHistory should return empty, got %v", hAnon)
	}
	cAnon, err := getCharacterHistories("")
	if err != nil || len(cAnon) != 0 {
		t.Errorf("anonymous getCharacterHistories should return nil, got %v", cAnon)
	}

	// 2. Account A query
	hA, err := getAccountHistory("account-A")
	if err != nil {
		t.Fatalf("getAccountHistory account-A failed: %v", err)
	}
	if len(hA.TotalGold) != 1 || hA.TotalGold[0] != 1000 {
		t.Errorf("Account A should only see Account A history (1000g), got %v", hA.TotalGold)
	}

	// 3. Account B query
	hB, err := getAccountHistory("account-B")
	if err != nil {
		t.Fatalf("getAccountHistory account-B failed: %v", err)
	}
	if len(hB.TotalGold) != 1 || hB.TotalGold[0] != 2000 {
		t.Errorf("Account B should only see Account B history (2000g), got %v", hB.TotalGold)
	}

	// 4. Verify unowned history (gold=999999) is NEVER returned to Account A or Account B
	for _, gold := range hA.TotalGold {
		if gold == 999999 {
			t.Errorf("SECURITY LEAK: Unowned history (999999) leaked to Account A")
		}
	}
	for _, gold := range hB.TotalGold {
		if gold == 999999 {
			t.Errorf("SECURITY LEAK: Unowned history (999999) leaked to Account B")
		}
	}

	// 5. Character history isolation
	charsA, err := getCharacterHistories("account-A")
	if err != nil {
		t.Fatalf("getCharacterHistories account-A failed: %v", err)
	}
	if len(charsA) != 1 || charsA[0].Name != "CharA" {
		t.Errorf("Account A character history should only include CharA, got %+v", charsA)
	}

	charsB, err := getCharacterHistories("account-B")
	if err != nil {
		t.Fatalf("getCharacterHistories account-B failed: %v", err)
	}
	if len(charsB) != 1 || charsB[0].Name != "CharB" {
		t.Errorf("Account B character history should only include CharB, got %+v", charsB)
	}
}

// ── Test 7: Concurrent Session Safety (Race / Concurrency Check) ─────────────
// Tests concurrent operations on session store and user sessions across multiple threads.
func TestConcurrentSessions_ThreadSafety(t *testing.T) {
	const numGoroutines = 20
	const numOps = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(gid int) {
			defer wg.Done()
			session := globalSessions.Create()
			defer globalSessions.Delete(session.ID)

			for i := 0; i < numOps; i++ {
				// Concurrent state setting and consumption
				state := fmt.Sprintf("state-%d-%d", gid, i)
				session.SetPendingOAuthState(state)
				if !session.ConsumePendingOAuthState(state) {
					t.Errorf("goroutine %d op %d: failed to consume valid state", gid, i)
				}

				// Concurrent summary read/write
				session.mu.Lock()
				session.Summary = AccountSummary{
					Authenticated: true,
					TotalGold:     int64(gid*1000 + i),
				}
				session.mu.Unlock()

				sum := session.GetSummary()
				if sum.TotalGold != int64(gid*1000+i) {
					t.Errorf("goroutine %d: summary corrupted", gid)
				}

				// Concurrent cache resets
				session.ClearExtraCaches()
			}
		}(g)
	}

	wg.Wait()
}

// ── Mock SQL Driver for DB History Isolation Testing ──────────────────────────

type mockHistoryDriver struct{}

func (m *mockHistoryDriver) Open(name string) (driver.Conn, error) {
	return &mockHistoryConn{}, nil
}

type mockHistoryConn struct{}

func (c *mockHistoryConn) Prepare(query string) (driver.Stmt, error) {
	return &mockHistoryStmt{query: query}, nil
}

func (c *mockHistoryConn) Close() error              { return nil }
func (c *mockHistoryConn) Begin() (driver.Tx, error) { return &mockHistoryTx{}, nil }

type mockHistoryTx struct{}

func (tx *mockHistoryTx) Commit() error   { return nil }
func (tx *mockHistoryTx) Rollback() error { return nil }

type mockHistoryStmt struct {
	query string
}

func (s *mockHistoryStmt) Close() error  { return nil }
func (s *mockHistoryStmt) NumInput() int { return -1 }
func (s *mockHistoryStmt) Exec(args []driver.Value) (driver.Result, error) {
	return driver.RowsAffected(1), nil
}

func (s *mockHistoryStmt) Query(args []driver.Value) (driver.Rows, error) {
	q := s.query
	var accountID string
	if len(args) > 0 {
		if v, ok := args[0].(string); ok {
			accountID = v
		}
	}

	// 1. SELECT recorded_at, total_gold, mounts, pets FROM account_snapshots WHERE account_id = $1
	if strings.Contains(q, "account_snapshots") && strings.Contains(q, "WHERE account_id = $1") {
		// Mock data table containing:
		// Row 1: account_id = NULL (legacy unowned history) -> gold = 999999
		// Row 2: account_id = "account-A" -> gold = 1000
		// Row 3: account_id = "account-B" -> gold = 2000
		type row struct {
			acc  string
			t    time.Time
			gold int64
			mnt  int
			pets int
		}
		table := []row{
			{acc: "", t: time.Now().Add(-10 * time.Hour), gold: 999999, mnt: 100, pets: 100}, // UNOWNED
			{acc: "account-A", t: time.Now().Add(-1 * time.Hour), gold: 1000, mnt: 10, pets: 5},
			{acc: "account-B", t: time.Now().Add(-1 * time.Hour), gold: 2000, mnt: 20, pets: 15},
		}

		var matched [][]driver.Value
		for _, r := range table {
			// In SQL, WHERE account_id = $1 NEVER matches NULL/empty unowned rows
			if r.acc != "" && r.acc == accountID {
				matched = append(matched, []driver.Value{r.t, r.gold, r.mnt, r.pets})
			}
		}
		return &mockRows{
			columns: []string{"recorded_at", "total_gold", "mounts", "pets"},
			rows:    matched,
		}, nil
	}

	// 2. SELECT DISTINCT character_name, realm FROM character_snapshots WHERE account_id = $1
	if strings.Contains(q, "DISTINCT character_name") && strings.Contains(q, "WHERE account_id = $1") {
		var matched [][]driver.Value
		if accountID == "account-A" {
			matched = append(matched, []driver.Value{"CharA", "RealmA"})
		} else if accountID == "account-B" {
			matched = append(matched, []driver.Value{"CharB", "RealmB"})
		}
		return &mockRows{
			columns: []string{"character_name", "realm"},
			rows:    matched,
		}, nil
	}

	// 3. Per-character snapshots query:
	// SELECT recorded_at, item_level, level, gold FROM character_snapshots WHERE account_id = $1 ...
	if strings.Contains(q, "character_snapshots") && strings.Contains(q, "item_level, level, gold") {
		var matched [][]driver.Value
		if accountID == "account-A" {
			// Return 2 snapshots so character is included
			matched = append(matched, []driver.Value{time.Now().Add(-2 * time.Hour), 610, 80, int64(500)})
			matched = append(matched, []driver.Value{time.Now().Add(-1 * time.Hour), 620, 80, int64(1000)})
		} else if accountID == "account-B" {
			matched = append(matched, []driver.Value{time.Now().Add(-2 * time.Hour), 625, 80, int64(1500)})
			matched = append(matched, []driver.Value{time.Now().Add(-1 * time.Hour), 630, 80, int64(2000)})
		}
		return &mockRows{
			columns: []string{"recorded_at", "item_level", "level", "gold"},
			rows:    matched,
		}, nil
	}

	return &mockRows{columns: []string{"dummy"}}, nil
}

type mockRows struct {
	columns []string
	rows    [][]driver.Value
	idx     int
}

func (r *mockRows) Columns() []string { return r.columns }
func (r *mockRows) Close() error      { return nil }
func (r *mockRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.rows) {
		return io.EOF
	}
	row := r.rows[r.idx]
	r.idx++
	copy(dest, row)
	return nil
}

var mockDriverRegistered sync.Once

func setupMockHistoryDB(t *testing.T) *sql.DB {
	mockDriverRegistered.Do(func() {
		sql.Register("mock_history_driver", &mockHistoryDriver{})
	})
	db, err := sql.Open("mock_history_driver", "mock_dsn")
	if err != nil {
		t.Fatalf("failed to open mock db: %v", err)
	}
	return db
}

// ── Test 8: Account Switch With In-Flight Jobs ────────────────────────────────
// Tests that an in-flight background job from a previous account does not
// overwrite or contaminate the new account session after account rotation.
func TestAccountSwitch_InFlightJobsDoNotOverwriteNewAccount(t *testing.T) {
	origOAuth2Config := oauth2Config
	origTransport := http.DefaultTransport
	defer func() {
		oauth2Config = origOAuth2Config
		http.DefaultTransport = origTransport
	}()

	oauth2Config = &oauth2.Config{
		ClientID: "test-client",
		Endpoint: oauth2.Endpoint{TokenURL: "https://mock.battle.net/token"},
	}

	blockOldJob := make(chan struct{})
	jobStarted := make(chan struct{})

	http.DefaultTransport = &mockTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			urlStr := req.URL.String()
			if strings.Contains(urlStr, "/profile/user/wow") {
				// Signal that job has reached Blizzard API call
				select {
				case jobStarted <- struct{}{}:
				default:
				}
				// Block until account switch is complete
				<-blockOldJob
				if req.Context().Err() != nil {
					return nil, req.Context().Err()
				}
				mockAcc := `{"wow_accounts":[{"characters":[{"name":"OldHero","realm":{"slug":"oldrealm"},"level":80}]}]}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(mockAcc)),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		},
	}

	// Session A begins for Account A
	sessionA := globalSessions.Create()
	sessionA.mu.Lock()
	sessionA.AccountID = "account-old"
	sessionA.Authenticated = true
	sessionA.Token = &oauth2.Token{AccessToken: "token-old", Expiry: time.Now().Add(time.Hour)}
	sessionA.mu.Unlock()

	// Launch background job for Session A
	jobDone := make(chan struct{})
	go func() {
		fetchCharacterDataForSession(sessionA)
		close(jobDone)
	}()

	// Wait until job is in-flight at Blizzard API call
	<-jobStarted

	// Now user switches accounts!
	// Invalidate Session A (as done during handleCallback / handleLogout)
	sessionA.Invalidate()
	globalSessions.Delete(sessionA.ID)

	// Create Session B for Account B
	sessionB := globalSessions.Create()
	sessionB.mu.Lock()
	sessionB.AccountID = "account-new"
	sessionB.Authenticated = true
	sessionB.Token = &oauth2.Token{AccessToken: "token-new", Expiry: time.Now().Add(time.Hour)}
	sessionB.Summary = AccountSummary{
		Authenticated: true,
		Characters: []CharacterStats{
			{Name: "NewHero", Realm: "newrealm", Level: 80},
		},
	}
	sessionB.mu.Unlock()
	defer globalSessions.Delete(sessionB.ID)

	// Release blocked job for Session A
	close(blockOldJob)
	<-jobDone

	// Assert Session A was not updated with character data because it was revoked
	sumA := sessionA.GetSummary()
	for _, c := range sumA.Characters {
		if c.Name == "OldHero" {
			t.Errorf("Revoked session A should not store fetched character data")
		}
	}

	// Assert Session B was NOT touched or corrupted by Job A
	sumB := sessionB.GetSummary()
	if len(sumB.Characters) != 1 || sumB.Characters[0].Name != "NewHero" {
		t.Errorf("Session B was corrupted by in-flight job A! Got: %+v", sumB.Characters)
	}
}

// ── Test 9: Stale Caches Isolation on Account Switch ──────────────────────────
// Tests that newly rotated sessions have fresh empty caches and never expose
// prior account equipment, mythic+, or achievement data.
func TestStaleCaches_NotExposedOnAccountSwitch(t *testing.T) {
	// Account A session with populated caches
	sessionA := globalSessions.Create()
	sessionA.mu.Lock()
	sessionA.AccountID = "account-A"
	sessionA.Authenticated = true
	sessionA.Token = &oauth2.Token{AccessToken: "token-A", Expiry: time.Now().Add(time.Hour)}
	sessionA.Equipment.Data = []EQChar{
		{Name: "OldChar", Realm: "OldRealm", Level: 80, Ilvl: 630},
	}
	sessionA.Equipment.Fetched = time.Now()
	sessionA.MythicPlus.Data = []MPChar{
		{Name: "OldChar", Realm: "OldRealm", Rating: 3200},
	}
	sessionA.MythicPlus.Fetched = time.Now()
	sessionA.Achievements["oldchar-oldrealm"] = &achProfileEntry{
		fetched: time.Now(),
		view:    &achView{Points: 99999},
	}
	sessionA.mu.Unlock()

	// Simulate login callback rotation: invalidate old session
	sessionA.Invalidate()
	globalSessions.Delete(sessionA.ID)

	// New rotated session for Account B
	sessionB := globalSessions.Create()
	sessionB.mu.Lock()
	sessionB.AccountID = "account-B"
	sessionB.Authenticated = true
	sessionB.Token = &oauth2.Token{AccessToken: "token-B", Expiry: time.Now().Add(time.Hour)}
	sessionB.mu.Unlock()
	defer globalSessions.Delete(sessionB.ID)

	// Verify Session B lazy caches are fresh and completely empty
	sessionB.Equipment.RLock()
	if len(sessionB.Equipment.Data) != 0 || !sessionB.Equipment.Fetched.IsZero() {
		t.Errorf("Session B Equipment cache must be fresh and empty, got %d items", len(sessionB.Equipment.Data))
	}
	sessionB.Equipment.RUnlock()

	sessionB.MythicPlus.RLock()
	if len(sessionB.MythicPlus.Data) != 0 || !sessionB.MythicPlus.Fetched.IsZero() {
		t.Errorf("Session B MythicPlus cache must be fresh and empty, got %d items", len(sessionB.MythicPlus.Data))
	}
	sessionB.MythicPlus.RUnlock()

	sessionB.AchievementsMu.RLock()
	if len(sessionB.Achievements) != 0 {
		t.Errorf("Session B Achievements cache must be fresh and empty, got %d entries", len(sessionB.Achievements))
	}
	sessionB.AchievementsMu.RUnlock()

	// Also verify that sessionA's Invalidate() cleared sessionA's caches
	sessionA.Equipment.RLock()
	if len(sessionA.Equipment.Data) != 0 {
		t.Errorf("Session A Equipment cache should be cleared on Invalidate()")
	}
	sessionA.Equipment.RUnlock()
}

// ── Test 10: Expired or Revoked Sessions Rejected ─────────────────────────────
// Tests that expired or revoked session cookies are rejected, return unauthenticated,
// and are purged from store.
func TestExpiredOrRevokedSessions_Rejected(t *testing.T) {
	// 1. Revoked session
	sessRevoked := globalSessions.Create()
	sessRevoked.mu.Lock()
	sessRevoked.Authenticated = true
	sessRevoked.Token = &oauth2.Token{AccessToken: "tok-revoked", Expiry: time.Now().Add(time.Hour)}
	sessRevoked.mu.Unlock()
	revokedID := sessRevoked.ID

	// Invalidate session
	sessRevoked.Invalidate()

	reqRevoked := httptest.NewRequest("GET", "/", nil)
	reqRevoked.AddCookie(&http.Cookie{Name: SessionCookieName, Value: revokedID})
	got := getSession(reqRevoked)
	if got != nil {
		t.Errorf("Revoked session MUST return nil from getSession(), got non-nil")
	}
	if globalSessions.Get(revokedID) != nil {
		t.Errorf("Revoked session should be deleted from globalSessions upon access")
	}

	// 2. Expired session by TTL
	sessExpiredTTL := globalSessions.Create()
	sessExpiredTTL.mu.Lock()
	sessExpiredTTL.Authenticated = true
	sessExpiredTTL.Token = &oauth2.Token{AccessToken: "tok-ttl", Expiry: time.Now().Add(time.Hour)}
	sessExpiredTTL.ExpiresAt = time.Now().Add(-1 * time.Hour) // expired
	sessExpiredTTL.mu.Unlock()
	expiredTTLID := sessExpiredTTL.ID

	reqExpiredTTL := httptest.NewRequest("GET", "/", nil)
	reqExpiredTTL.AddCookie(&http.Cookie{Name: SessionCookieName, Value: expiredTTLID})
	if s := getSession(reqExpiredTTL); s != nil {
		t.Errorf("Expired session (TTL) MUST return nil from getSession(), got non-nil")
	}
	if globalSessions.Get(expiredTTLID) != nil {
		t.Errorf("Expired session should be deleted from globalSessions upon access")
	}

	// 3. Expired session by Idle Timeout
	sessExpiredIdle := globalSessions.Create()
	sessExpiredIdle.mu.Lock()
	sessExpiredIdle.Authenticated = true
	sessExpiredIdle.Token = &oauth2.Token{AccessToken: "tok-idle", Expiry: time.Now().Add(time.Hour)}
	sessExpiredIdle.LastAccessed = time.Now().Add(-25 * time.Hour) // past SessionIdleTimeout
	sessExpiredIdle.mu.Unlock()
	expiredIdleID := sessExpiredIdle.ID

	reqExpiredIdle := httptest.NewRequest("GET", "/", nil)
	reqExpiredIdle.AddCookie(&http.Cookie{Name: SessionCookieName, Value: expiredIdleID})
	if s := getSession(reqExpiredIdle); s != nil {
		t.Errorf("Expired session (idle) MUST return nil from getSession(), got non-nil")
	}

	// 4. CleanExpired purges unaccessed expired sessions
	sessStale := globalSessions.Create()
	sessStale.mu.Lock()
	sessStale.LastAccessed = time.Now().Add(-48 * time.Hour)
	sessStale.mu.Unlock()
	staleID := sessStale.ID

	globalSessions.CleanExpired(24 * time.Hour)
	if globalSessions.Get(staleID) != nil {
		t.Errorf("CleanExpired should remove sessions older than maxAge")
	}
}

// ── Test 11: No Bearer Session IDs or Tokens in Logs ──────────────────────────
// Tests that bearer session IDs and access tokens are never logged.
func TestNoSessionIDsOrTokensInLogs(t *testing.T) {
	origOAuth2Config := oauth2Config
	origTransport := http.DefaultTransport
	defer func() {
		oauth2Config = origOAuth2Config
		http.DefaultTransport = origTransport
	}()

	secretToken := "SECRET_BEARER_TOKEN_987654321"

	var logBuf strings.Builder
	origLogWriter := log.Writer()
	origLogFlags := log.Flags()
	log.SetOutput(&logBuf)
	defer func() {
		log.SetOutput(origLogWriter)
		log.SetFlags(origLogFlags)
	}()

	oauth2Config = &oauth2.Config{
		ClientID: "test-client",
		Endpoint: oauth2.Endpoint{
			TokenURL: "https://mock.battle.net/token",
		},
	}

	http.DefaultTransport = &mockTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			urlStr := req.URL.String()
			if strings.Contains(urlStr, "/token") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(fmt.Sprintf(`{"access_token":"%s","token_type":"bearer","expires_in":3600}`, secretToken))),
				}, nil
			}
			if strings.Contains(urlStr, "/oauth/userinfo") || strings.Contains(urlStr, "/userinfo") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"sub":"bnet-test-123","battletag":"Tester#1234"}`)),
				}, nil
			}
			if strings.Contains(urlStr, "/profile/user/wow") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"wow_accounts":[]}`)),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		},
	}

	// 1. Login flow
	loginRec := httptest.NewRecorder()
	loginReq := httptest.NewRequest("GET", "/login", nil)
	handleLogin(loginRec, loginReq)

	var preAuthSessionID string
	for _, c := range loginRec.Result().Cookies() {
		if c.Name == SessionCookieName {
			preAuthSessionID = c.Value
		}
	}
	if preAuthSessionID == "" {
		t.Fatalf("handleLogin did not set session cookie")
	}

	preAuthSess := globalSessions.Get(preAuthSessionID)
	if preAuthSess == nil {
		t.Fatalf("pre-auth session not found")
	}
	state := preAuthSess.PendingOAuthState

	// 2. Callback flow (session rotation)
	cbRec := httptest.NewRecorder()
	cbReq := httptest.NewRequest("GET", "/callback?code=mock_code&state="+url.QueryEscape(state), nil)
	cbReq.AddCookie(&http.Cookie{Name: SessionCookieName, Value: preAuthSessionID})
	handleCallback(cbRec, cbReq)

	var newSessionID string
	for _, c := range cbRec.Result().Cookies() {
		if c.Name == SessionCookieName {
			newSessionID = c.Value
		}
	}
	if newSessionID == "" {
		t.Fatalf("handleCallback did not rotate and set new session cookie")
	}

	newSess := globalSessions.Get(newSessionID)
	if newSess != nil {
		// 3. Exercise equipment, mythic, achievements, refresh
		fetchAllEquipmentForSession(newSess)
		fetchAllMythicForSession(newSess)
		fetchAchProfileForSession(newSess, "dummy-key")

		refRec := httptest.NewRecorder()
		refReq := httptest.NewRequest("POST", "/refresh", nil)
		refReq.AddCookie(&http.Cookie{Name: SessionCookieName, Value: newSessionID})
		handleRefresh(refRec, refReq)

		// 4. Logout
		outRec := httptest.NewRecorder()
		outReq := httptest.NewRequest("GET", "/logout", nil)
		outReq.AddCookie(&http.Cookie{Name: SessionCookieName, Value: newSessionID})
		handleLogout(outRec, outReq)
	}

	capturedLogs := logBuf.String()

	// Assert pre-auth session ID is NEVER in logs
	if strings.Contains(capturedLogs, preAuthSessionID) {
		t.Errorf("Pre-auth session ID was leaked in logs! Found: %s", preAuthSessionID)
	}

	// Assert new session ID is NEVER in logs
	if strings.Contains(capturedLogs, newSessionID) {
		t.Errorf("Rotated bearer session ID was leaked in logs! Found: %s", newSessionID)
	}

	// Assert secret token is NEVER in logs
	if strings.Contains(capturedLogs, secretToken) {
		t.Errorf("OAuth token was leaked in logs! Found: %s", secretToken)
	}
}

// ── Test 12: Regression — Callback Rejects Profile Fallback ───────────────────
// Tests that handleCallback fails closed with HTTP 401 when the canonical
// UserInfo endpoint fails, even if /profile/user/wow returns an account id.
func TestCallback_ProfileFallbackProhibitedCannotAuthenticate(t *testing.T) {
	origOAuth2Config := oauth2Config
	origTransport := http.DefaultTransport
	defer func() {
		oauth2Config = origOAuth2Config
		http.DefaultTransport = origTransport
	}()

	oauth2Config = &oauth2.Config{
		ClientID: "test-client",
		Endpoint: oauth2.Endpoint{
			TokenURL: "https://mock.battle.net/token",
		},
	}

	http.DefaultTransport = &mockTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			urlStr := req.URL.String()
			if strings.Contains(urlStr, "/token") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"access_token":"mock-token","token_type":"bearer","expires_in":3600}`)),
				}, nil
			}
			// Canonical UserInfo endpoint returns HTTP 404 / 401 error
			if strings.Contains(urlStr, "/oauth/userinfo") {
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"error":"not_found"}`)),
				}, nil
			}
			// Even if /profile/user/wow returns a 200 OK with ID
			if strings.Contains(urlStr, "/profile/user/wow") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"id":99887766,"wow_accounts":[]}`)),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		},
	}

	// 1. Create pre-auth session
	preSess := globalSessions.Create()
	preSess.SetPendingOAuthState("state-fb-test")
	preID := preSess.ID

	// 2. Trigger callback with valid code and matching state
	cbRec := httptest.NewRecorder()
	cbReq := httptest.NewRequest("GET", "/callback?code=mock_code&state=state-fb-test", nil)
	cbReq.AddCookie(&http.Cookie{Name: SessionCookieName, Value: preID})
	handleCallback(cbRec, cbReq)

	// MUST fail closed with 401 Unauthorized
	if cbRec.Code != http.StatusUnauthorized {
		t.Errorf("handleCallback must fail closed with 401 Unauthorized when UserInfo fails, got %d", cbRec.Code)
	}

	// Pre-auth session must be invalidated and deleted
	if globalSessions.Get(preID) != nil {
		t.Errorf("Pre-auth session must be deleted on authentication failure")
	}

	// No authenticated session should have been created with the profile ID
	globalSessions.mu.RLock()
	for _, s := range globalSessions.sessions {
		if s.GetAccountID() == "99887766" {
			t.Errorf("Profile ID 99887766 MUST NOT be used to authenticate a session")
		}
	}
	globalSessions.mu.RUnlock()
}

// ── Test 13: Periodic Session Cleanup ─────────────────────────────────────────
// Tests that StartPeriodicCleanup automatically removes expired and revoked sessions from store.
func TestPeriodicSessionCleanup_CleansStore(t *testing.T) {
	// Start periodic cleaner with 15ms interval
	stop := globalSessions.StartPeriodicCleanup(15 * time.Millisecond)
	defer stop()

	// Create an expired session
	expiredSess := globalSessions.Create()
	expiredSess.mu.Lock()
	expiredSess.ExpiresAt = time.Now().Add(-1 * time.Hour) // expired
	expiredSess.mu.Unlock()
	expiredID := expiredSess.ID

	// Create an active valid session
	activeSess := globalSessions.Create()
	activeSess.mu.Lock()
	activeSess.ExpiresAt = time.Now().Add(time.Hour)
	activeSess.mu.Unlock()
	activeID := activeSess.ID
	defer globalSessions.Delete(activeID)

	// Wait for cleaner to run (at least 2 ticks)
	time.Sleep(50 * time.Millisecond)

	// Assert expired session is purged
	if globalSessions.Get(expiredID) != nil {
		t.Errorf("Expired session was not automatically purged by periodic cleaner")
	}

	// Assert active session remains intact
	if globalSessions.Get(activeID) == nil {
		t.Errorf("Active session was incorrectly purged by periodic cleaner")
	}
}
