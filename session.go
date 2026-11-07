package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

const (
	// SessionCookieName is the cookie key used for per-browser session tracking.
	SessionCookieName = "wow_session_id"
	// SessionTTL is the default maximum lifetime of an active session.
	SessionTTL = 7 * 24 * time.Hour
	// SessionIdleTimeout is the maximum duration an idle session remains valid without requests.
	SessionIdleTimeout = 24 * time.Hour
	// SessionCleanupInterval is the period between periodic session purge sweeps.
	SessionCleanupInterval = 15 * time.Minute
	// OAuthStateTTL is the maximum time a login OAuth state token remains valid before expiry.
	OAuthStateTTL = 10 * time.Minute
	// CanonicalBattleNetUserInfoURL is the official OpenID Connect UserInfo endpoint for the Battle.net Developer Platform.
	CanonicalBattleNetUserInfoURL = "https://oauth.battle.net/oauth/userinfo"
)

// EquipmentSessionState stores equipment progression data scoped to a specific user session.
type EquipmentSessionState struct {
	sync.RWMutex
	Data    []EQChar
	Fetched time.Time
	Loading bool
}

// MythicPlusSessionState stores Mythic+ run and rating data scoped to a specific user session.
type MythicPlusSessionState struct {
	sync.RWMutex
	Data    []MPChar
	Fetched time.Time
	Loading bool
}

// UserSession stores isolated credentials, summary data, and per-user caches.
type UserSession struct {
	ID                 string
	CreatedAt          time.Time
	LastAccessed       time.Time
	ExpiresAt          time.Time
	mu                 sync.RWMutex
	revoked            bool
	ctx                context.Context
	cancelFunc         context.CancelFunc
	Authenticated      bool
	AccountID          string // Battle.net Account ID (verified from Battle.net API)
	BattleTag          string // BattleTag (verified from Battle.net API)
	Token              *oauth2.Token
	Summary            AccountSummary
	Equipment          *EquipmentSessionState
	MythicPlus         *MythicPlusSessionState
	Achievements       map[string]*achProfileEntry
	AchievementsMu     sync.RWMutex
	PendingOAuthState  string
	PendingOAuthExpiry time.Time
	Refreshing         bool
}

func newSession(id string) *UserSession {
	now := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	return &UserSession{
		ID:           id,
		CreatedAt:    now,
		LastAccessed: now,
		ExpiresAt:    now.Add(SessionTTL),
		Equipment:    &EquipmentSessionState{},
		MythicPlus:   &MythicPlusSessionState{},
		Achievements: make(map[string]*achProfileEntry),
		ctx:          ctx,
		cancelFunc:   cancel,
	}
}

// Context returns the cancellable context bound to this session's lifecycle.
func (s *UserSession) Context() context.Context {
	if s == nil {
		return context.Background()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

// Invalidate safely revokes the session, cancels in-flight background tasks, and clears all data.
func (s *UserSession) Invalidate() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.revoked = true
	s.Authenticated = false
	s.Token = nil
	s.AccountID = ""
	s.BattleTag = ""
	s.Summary = AccountSummary{Authenticated: false}
	if s.cancelFunc != nil {
		s.cancelFunc()
	}
	s.mu.Unlock()
	s.ClearExtraCaches()
}

// IsRevoked reports whether this session has been revoked.
func (s *UserSession) IsRevoked() bool {
	if s == nil {
		return true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.revoked
}

// IsExpired reports whether the session has passed its expiration time or idle timeout.
func (s *UserSession) IsExpired() bool {
	if s == nil {
		return true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	if !s.ExpiresAt.IsZero() && now.After(s.ExpiresAt) {
		return true
	}
	if !s.LastAccessed.IsZero() && time.Since(s.LastAccessed) > SessionIdleTimeout {
		return true
	}
	return false
}

// GetAccountID returns the session's verified Battle.net Account ID in a thread-safe manner.
func (s *UserSession) GetAccountID() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.AccountID
}

// SetAccountID safely updates the Battle.net Account ID.
func (s *UserSession) SetAccountID(accountID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.AccountID = accountID
}

// GetBattleTag returns the session's verified BattleTag in a thread-safe manner.
func (s *UserSession) GetBattleTag() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.BattleTag
}

// SetBattleTag safely updates the BattleTag.
func (s *UserSession) SetBattleTag(battleTag string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.BattleTag = battleTag
}

// IsAuthenticated reports whether the session has valid authentication.
func (s *UserSession) IsAuthenticated() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.revoked && s.Authenticated && s.Token != nil && s.Token.Valid()
}

// GetToken returns the session's OAuth token.
func (s *UserSession) GetToken() *oauth2.Token {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.revoked {
		return nil
	}
	return s.Token
}

// GetSummary returns a copy of the session's current AccountSummary.
func (s *UserSession) GetSummary() AccountSummary {
	if s == nil {
		return AccountSummary{Authenticated: false}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.revoked {
		return AccountSummary{Authenticated: false}
	}
	return s.Summary
}

// Client returns an authenticated HTTP client for this session, or nil if unauthenticated.
func (s *UserSession) Client(ctx context.Context) *http.Client {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	tok := s.Token
	revoked := s.revoked
	s.mu.RUnlock()
	if revoked || tok == nil || oauth2Config == nil {
		return nil
	}
	return oauth2Config.Client(ctx, tok)
}

// SetPendingOAuthState stores a one-time OAuth state for an ongoing login attempt.
func (s *UserSession) SetPendingOAuthState(state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PendingOAuthState = state
	s.PendingOAuthExpiry = time.Now().Add(OAuthStateTTL)
}

// ConsumePendingOAuthState verifies and immediately consumes the OAuth state (one-time use).
// Returns true only if the state matches and has not expired.
func (s *UserSession) ConsumePendingOAuthState(state string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.PendingOAuthState == "" || state == "" {
		return false
	}
	if time.Now().After(s.PendingOAuthExpiry) {
		s.PendingOAuthState = ""
		s.PendingOAuthExpiry = time.Time{}
		return false
	}
	if s.PendingOAuthState != state {
		return false
	}
	// Consume state immediately so it cannot be replayed
	s.PendingOAuthState = ""
	s.PendingOAuthExpiry = time.Time{}
	return true
}

// ClearExtraCaches resets the session's lazy caches (Equipment, Mythic+, Achievements).
func (s *UserSession) ClearExtraCaches() {
	if s == nil {
		return
	}
	s.Equipment.Lock()
	s.Equipment.Data = nil
	s.Equipment.Fetched = time.Time{}
	s.Equipment.Loading = false
	s.Equipment.Unlock()

	s.MythicPlus.Lock()
	s.MythicPlus.Data = nil
	s.MythicPlus.Fetched = time.Time{}
	s.MythicPlus.Loading = false
	s.MythicPlus.Unlock()

	s.AchievementsMu.Lock()
	s.Achievements = make(map[string]*achProfileEntry)
	s.AchievementsMu.Unlock()
}

// EligibleCharacters returns characters that have valid profile data for this session.
func (s *UserSession) EligibleCharacters(minLevel int) []CharacterStats {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []CharacterStats
	for _, c := range s.Summary.Characters {
		if c.Error == "" && c.Level >= minLevel {
			out = append(out, c)
		}
	}
	return out
}

// SessionStore manages active in-memory user sessions.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*UserSession
}

var globalSessions = &SessionStore{
	sessions: make(map[string]*UserSession),
}

// Get looks up a session by ID.
func (store *SessionStore) Get(id string) *UserSession {
	if id == "" {
		return nil
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.sessions[id]
}

// Create generates and registers a new session with a random ID.
func (store *SessionStore) Create() *UserSession {
	store.mu.Lock()
	defer store.mu.Unlock()
	id := generateSessionID()
	s := newSession(id)
	store.sessions[id] = s
	return s
}

// Put adds or replaces a session in the store.
func (store *SessionStore) Put(s *UserSession) {
	if s == nil || s.ID == "" {
		return
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.sessions[s.ID] = s
}

// Delete removes a session by ID.
func (store *SessionStore) Delete(id string) {
	if id == "" {
		return
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.sessions, id)
}

// CleanExpired removes sessions that have expired, been revoked, or exceeded maxAge.
func (store *SessionStore) CleanExpired(maxAge ...time.Duration) {
	store.mu.Lock()
	defer store.mu.Unlock()
	cutoffDuration := SessionIdleTimeout
	if len(maxAge) > 0 && maxAge[0] > 0 {
		cutoffDuration = maxAge[0]
	}
	cutoff := time.Now().Add(-cutoffDuration)

	for id, s := range store.sessions {
		s.mu.RLock()
		revoked := s.revoked
		last := s.LastAccessed
		expiresAt := s.ExpiresAt
		s.mu.RUnlock()

		if revoked || last.Before(cutoff) || (!expiresAt.IsZero() && time.Now().After(expiresAt)) {
			s.Invalidate()
			delete(store.sessions, id)
		}
	}
}

// StartPeriodicCleanup launches a background ticker that periodically purges expired or revoked sessions.
// Returns a stop function to cancel the background cleaner upon server shutdown.
func (store *SessionStore) StartPeriodicCleanup(interval time.Duration) func() {
	if interval <= 0 {
		interval = SessionCleanupInterval
	}
	ticker := time.NewTicker(interval)
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				store.CleanExpired()
			case <-stop:
				ticker.Stop()
				return
			}
		}
	}()
	return func() {
		close(stop)
	}
}

func generateSessionID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// getSession retrieves the UserSession associated with the request cookie.
// Returns nil if the cookie is missing, invalid, expired, or revoked.
// No legacy global fallback is permitted for unauthenticated requests.
func getSession(r *http.Request) *UserSession {
	if r == nil {
		return nil
	}
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie == nil || cookie.Value == "" {
		return nil
	}
	s := globalSessions.Get(cookie.Value)
	if s == nil {
		return nil
	}
	if s.IsRevoked() || s.IsExpired() {
		s.Invalidate()
		globalSessions.Delete(cookie.Value)
		return nil
	}
	s.mu.Lock()
	s.LastAccessed = time.Now()
	s.mu.Unlock()
	return s
}

// getOrCreateSession returns the existing session from the request cookie, or creates and sets a new one.
// Performs an opportunistic sweep of expired sessions upon session creation.
func getOrCreateSession(w http.ResponseWriter, r *http.Request) *UserSession {
	s := getSession(r)
	if s != nil {
		return s
	}
	globalSessions.CleanExpired()
	s = globalSessions.Create()
	setSessionCookie(w, r, s.ID)
	return s
}

// setSessionCookie writes the HTTP cookie for the given session.
func setSessionCookie(w http.ResponseWriter, r *http.Request, sessionID string) {
	secure := isSecureRequest(r)
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    sessionID,
		Path:     "/",
		MaxAge:   int(SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearSessionCookie deletes the session cookie in the client browser.
func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	secure := isSecureRequest(r)
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// isSecureRequest determines if HTTPS cookie flags should be applied.
func isSecureRequest(r *http.Request) bool {
	if sec := os.Getenv("SESSION_COOKIE_SECURE"); sec != "" {
		return sec == "true" || sec == "1"
	}
	if r != nil {
		if r.TLS != nil {
			return true
		}
		if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			return true
		}
	}
	if strings.HasPrefix(strings.ToLower(config.RedirectURL), "https://") {
		return true
	}
	return false
}

// BattleNetIdentity stores the verified user identity from Blizzard's OAuth/API endpoints.
type BattleNetIdentity struct {
	AccountID string
	BattleTag string
}

// verifyBattleNetIdentity queries Battle.net's canonical OpenID Connect userinfo endpoint
// (https://oauth.battle.net/oauth/userinfo) directly using the access token to establish identity.
// Uses the stable unique OIDC 'sub' identifier. Never falls back to WoW game profiles, internal IDs,
// or unnamespaced values, avoiding identity mixing and account history splitting.
// Fails closed if identity cannot be verified.
func verifyBattleNetIdentity(ctx context.Context, client *http.Client, token *oauth2.Token) (BattleNetIdentity, error) {
	if client == nil || token == nil {
		return BattleNetIdentity{}, fmt.Errorf("client or token is nil")
	}
	if token.AccessToken == "" {
		return BattleNetIdentity{}, fmt.Errorf("token access token is empty")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", CanonicalBattleNetUserInfoURL, nil)
	if err != nil {
		return BattleNetIdentity{}, fmt.Errorf("failed to create userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	resp, err := client.Do(req)
	if err != nil {
		return BattleNetIdentity{}, fmt.Errorf("failed to query battlenet userinfo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return BattleNetIdentity{}, fmt.Errorf("battlenet userinfo returned HTTP %d", resp.StatusCode)
	}

	var info struct {
		Sub       string `json:"sub"`
		BattleTag string `json:"battletag"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return BattleNetIdentity{}, fmt.Errorf("failed to decode battlenet userinfo: %w", err)
	}

	sub := strings.TrimSpace(info.Sub)
	if sub == "" {
		return BattleNetIdentity{}, fmt.Errorf("battlenet userinfo missing canonical 'sub' identifier")
	}

	return BattleNetIdentity{
		AccountID: sub,
		BattleTag: strings.TrimSpace(info.BattleTag),
	}, nil
}

var (
	legacySession     *UserSession
	legacySessionOnce sync.Once
)

func getLegacySession() *UserSession {
	legacySessionOnce.Do(func() {
		legacySession = newSession("legacy-global-test-session")
	})

	tokenMutex.RLock()
	tok := userToken
	tokenMutex.RUnlock()

	summaryMutex.RLock()
	sum := accountSummary
	summaryMutex.RUnlock()

	legacySession.mu.Lock()
	legacySession.Token = tok
	legacySession.Summary = sum
	legacySession.Authenticated = (tok != nil && tok.Valid()) || sum.Authenticated
	if legacySession.AccountID == "" {
		legacySession.AccountID = "legacy"
	}
	legacySession.revoked = false
	legacySession.mu.Unlock()

	return legacySession
}
