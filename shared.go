package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// cacheTTL controls how long lazily fetched per-page data (Mythic+, Equipment)
// is considered fresh before it is re-fetched in the background.
const cacheTTL = 10 * time.Minute

// apiClient returns an OAuth-authenticated HTTP client, or nil if the user has
// not logged in yet.
func apiClient() *http.Client {
	tokenMutex.RLock()
	token := userToken
	tokenMutex.RUnlock()
	if token == nil {
		return nil
	}
	return oauth2Config.Client(context.Background(), token)
}

// getJSON GETs a Blizzard API URL and decodes the JSON body into out.
// It retries once on HTTP 429 and returns the final HTTP status code.
func getJSON(client *http.Client, url string, out interface{}) (int, error) {
	for attempt := 0; attempt < 2; attempt++ {
		resp, err := client.Get(url)
		if err != nil {
			return 0, err
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt == 0 {
			resp.Body.Close()
			time.Sleep(1500 * time.Millisecond)
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, err
		}
		return resp.StatusCode, nil
	}
	return http.StatusTooManyRequests, fmt.Errorf("HTTP 429")
}

// profileURL builds a profile-namespace character URL. suffix may be "" or e.g. "/equipment".
func characterURL(realm, name, suffix string) string {
	return fmt.Sprintf("https://%s.api.blizzard.com/profile/wow/character/%s/%s%s?namespace=profile-%s&locale=%s",
		config.Region, strings.ToLower(realm), strings.ToLower(name), suffix, config.Region, config.Locale)
}

// eligibleCharacters returns characters that have real profile data (no errors, level >= minLevel).
func eligibleCharacters(minLevel int) []CharacterStats {
	summaryMutex.RLock()
	defer summaryMutex.RUnlock()
	var out []CharacterStats
	for _, c := range accountSummary.Characters {
		if c.Error == "" && c.Level >= minLevel {
			out = append(out, c)
		}
	}
	return out
}

// pageHead is shared <head>/CSS/header/nav markup used by the new pages.
// Page templates are: pageHead + pageExtraCSS + pageNav + body.
const pageHead = `<!DOCTYPE html>
<html lang="en">
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
` + navLayoutCSS

const pageNav = `</style>
</head>
<body>
<div class="container">
<header>
` + navHTML + `  <img src="/images/World-of-Warcraft-Logo-2001.png" alt="World of Warcraft" class="logo">
</header>
</div>
`

// Rendered when the user is not logged in.
const pageLoginNotice = `<div class="page-wrap"><div class="card notice-box"><h2>Not logged in</h2>
<p>Please <a href="/login" style="color:#80c0ff">log in with Battle.net</a> first.</p></div></div></body></html>`

// navHTML is the single source of truth for the header navigation. Every page
// template includes it via concatenation so the buttons are identical everywhere.
const navHTML = `<div class="header-nav-btns">
    <a href="/" class="nav-btn">🏠 Home</a>
    <a href="/vault" class="nav-btn nav-btn-vault"><img src="/images/vault-button.png" style="width:20px;height:20px;flex-shrink:0;filter:drop-shadow(0 1px 3px rgba(0,0,0,0.6));" alt="">Great Vault</a>
    <a href="/roster" class="nav-btn nav-btn-roster"><img src="/images/quest.png" style="width:20px;height:20px;flex-shrink:0;filter:drop-shadow(0 1px 3px rgba(0,0,0,0.6));" alt="">Weekly Roster</a>
    <a href="/professions" class="nav-btn nav-btn-prof"><img src="/images/professions.png" style="width:20px;height:20px;flex-shrink:0;filter:drop-shadow(0 1px 3px rgba(0,0,0,0.6));" alt="">Professions</a>
    <a href="/mythic-plus" class="nav-btn nav-btn-mplus"><img src="/images/dungeon.png" style="width:20px;height:20px;flex-shrink:0;filter:drop-shadow(0 1px 3px rgba(0,0,0,0.6));" alt="">Mythic+</a>
    <a href="/equipment" class="nav-btn nav-btn-gear">🛡️ Equipment</a>
    <a href="/achievements" class="nav-btn nav-btn-ach">🏆 Achievements</a>
    <a href="/history" class="nav-btn nav-btn-history"><img src="/images/progress.png" style="width:20px;height:20px;flex-shrink:0;filter:drop-shadow(0 1px 3px rgba(0,0,0,0.6));" alt="">History</a>
    <a href="/logout" class="nav-btn nav-btn-logout">🔓 Re-authenticate</a>
  </div>`

// navLayoutCSS turns the header nav into a centred horizontal bar beneath the
// logo (instead of a column pinned top-left that overlapped page content).
// It is appended after each page's own CSS so it overrides the old rules.
const navLayoutCSS = `
/* ── Horizontal header nav (overrides legacy top-left column) ── */
header { display: flex !important; flex-direction: column; align-items: center; }
header .logo { order: 1; }
header .header-nav-btns {
  order: 2; position: static !important; flex-direction: row !important; flex-wrap: wrap;
  justify-content: center; gap: 8px !important; width: auto !important; max-width: 1100px; margin-top: 10px; z-index: 5;
}
header .nav-btn-mplus { background: linear-gradient(135deg, rgba(110,50,20,0.85), rgba(80,30,10,0.85)); border-color: #e07830; color: #ffc890; }
header .nav-btn-gear { background: linear-gradient(135deg, rgba(90,30,60,0.85), rgba(60,15,40,0.85)); border-color: #d050a0; color: #ffc0e8; }
header .nav-btn-ach { background: linear-gradient(135deg, rgba(20,70,80,0.85), rgba(10,50,60,0.85)); border-color: #30b8c8; color: #a0f0ff; }
header .nav-btn img { width: 20px; height: 20px; }
header .nav-btn { width: auto !important; padding: 8px 14px !important; font-size: 0.85em !important; }
`
