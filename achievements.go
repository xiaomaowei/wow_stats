package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ── Scope configuration ──────────────────────────────────────────────────────

type achScope struct {
	// Category names treated as the "current expansion" (matched case-insensitively
	// at any depth; their sub-categories are included automatically).
	ExpansionNames []string `json:"expansion_names"`
	// Achievements outside the expansion are shown too once progress reaches this %.
	AlmostThreshold int `json:"almost_threshold"`
	// Root categories never shown in the "almost there" list.
	ExcludedRoots []string `json:"excluded_roots"`
}

func loadAchScope() achScope {
	sc := achScope{ExpansionNames: []string{"Midnight"}, AlmostThreshold: 80, ExcludedRoots: []string{"Feats of Strength"}}
	if b, err := os.ReadFile("data/achievement_scope.json"); err == nil {
		if err := json.Unmarshal(b, &sc); err != nil {
			log.Printf("achievement_scope.json parse error: %v", err)
		}
	}
	return sc
}

// ── Static catalogue (categories) ────────────────────────────────────────────

type achCategory struct {
	ID           int
	Name         string
	Parent       int
	Guild        bool
	Achievements []struct {
		ID   int
		Name string
	}
}

var achCats struct {
	sync.Mutex
	byID     map[int]*achCategory
	achToCat map[int]int
	fetched  time.Time
	loading  bool
	err      string
}

const catTTL = 24 * time.Hour

func loadAchCategories() {
	defer func() {
		achCats.Lock()
		achCats.loading = false
		achCats.Unlock()
	}()
	client := apiClient()
	if client == nil {
		return
	}
	base := fmt.Sprintf("https://%s.api.blizzard.com/data/wow/achievement-category", config.Region)
	suffix := fmt.Sprintf("?namespace=static-%s&locale=%s", config.Region, config.Locale)

	var idx struct {
		Categories []struct {
			ID int `json:"id"`
		} `json:"categories"`
	}
	if _, err := getJSON(client, base+"/index"+suffix, &idx); err != nil {
		achCats.Lock()
		achCats.err = "could not load achievement categories: " + err.Error()
		achCats.Unlock()
		log.Printf("Achievement categories: %v", err)
		return
	}

	byID := map[int]*achCategory{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, c := range idx.Categories {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var d struct {
				ID        int    `json:"id"`
				Name      string `json:"name"`
				IsGuild   bool   `json:"is_guild_category"`
				ParentCat *struct {
					ID int `json:"id"`
				} `json:"parent_category"`
				Achievements []struct {
					ID   int    `json:"id"`
					Name string `json:"name"`
				} `json:"achievements"`
			}
			if _, err := getJSON(client, fmt.Sprintf("%s/%d%s", base, id, suffix), &d); err != nil {
				log.Printf("Achievement category %d: %v", id, err)
				return
			}
			cat := &achCategory{ID: d.ID, Name: d.Name, Guild: d.IsGuild}
			if d.ParentCat != nil {
				cat.Parent = d.ParentCat.ID
			}
			for _, a := range d.Achievements {
				cat.Achievements = append(cat.Achievements, struct {
					ID   int
					Name string
				}{a.ID, a.Name})
			}
			mu.Lock()
			byID[id] = cat
			mu.Unlock()
		}(c.ID)
	}
	wg.Wait()

	achToCat := map[int]int{}
	for id, c := range byID {
		for _, a := range c.Achievements {
			achToCat[a.ID] = id
		}
	}
	achCats.Lock()
	achCats.byID, achCats.achToCat = byID, achToCat
	achCats.fetched = time.Now()
	achCats.err = ""
	achCats.Unlock()
	log.Printf("Achievement catalogue loaded: %d categories, %d achievements", len(byID), len(achToCat))
}

// catPath returns "Root › Child › Leaf" for a category id. Caller must hold no lock on achCats
// (it is passed the map snapshot).
func catPath(byID map[int]*achCategory, id int) string {
	var parts []string
	for i := 0; i < 8 && id != 0; i++ {
		c, ok := byID[id]
		if !ok {
			break
		}
		parts = append([]string{c.Name}, parts...)
		id = c.Parent
	}
	return strings.Join(parts, " › ")
}

func catRoot(byID map[int]*achCategory, id int) *achCategory {
	var root *achCategory
	for i := 0; i < 8 && id != 0; i++ {
		c, ok := byID[id]
		if !ok {
			break
		}
		root = c
		id = c.Parent
	}
	return root
}

// ── Static achievement details (lazy, cached) ────────────────────────────────

type staticAch struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Reward      string `json:"reward"`
	Points      int    `json:"points"`
	Required    int64  `json:"-"`
}

var (
	staticAchCache = map[int]*staticAch{}
	staticAchMu    sync.Mutex
)

func getStaticAch(client *http.Client, id int) *staticAch {
	staticAchMu.Lock()
	if v, ok := staticAchCache[id]; ok {
		staticAchMu.Unlock()
		return v
	}
	staticAchMu.Unlock()

	var d struct {
		ID          int    `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Reward      string `json:"reward_description"`
		Points      int    `json:"points"`
		Criteria    struct {
			Amount int64 `json:"amount"`
		} `json:"criteria"`
	}
	u := fmt.Sprintf("https://%s.api.blizzard.com/data/wow/achievement/%d?namespace=static-%s&locale=%s",
		config.Region, id, config.Region, config.Locale)
	if _, err := getJSON(client, u, &d); err != nil {
		return nil // not cached so a later request can retry
	}
	v := &staticAch{ID: d.ID, Name: d.Name, Description: d.Description, Reward: d.Reward, Points: d.Points, Required: d.Criteria.Amount}
	staticAchMu.Lock()
	staticAchCache[id] = v
	staticAchMu.Unlock()
	return v
}

// GET /api/achievement?id=123 — name/description/reward for the expand-on-click detail.
func handleAPIAchievement(w http.ResponseWriter, r *http.Request) {
	client := apiClient()
	if client == nil {
		http.Error(w, "Not authenticated", http.StatusUnauthorized)
		return
	}
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id <= 0 {
		http.Error(w, "require ?id=N", http.StatusBadRequest)
		return
	}
	a := getStaticAch(client, id)
	if a == nil {
		http.Error(w, "achievement not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(a)
}

// ── Per-selection view ───────────────────────────────────────────────────────

type AchItem struct {
	ID     int
	Name   string
	Status string // done | prog | none
	Pct    int
	Path   string // category path (for almost / recent lists)
	When   string // completion date (recent list)
}

type AchGroup struct {
	Name        string
	Items       []AchItem
	Done, Total int
}

type achView struct {
	Points   int
	Overall  int // total completed achievements
	Groups   []AchGroup
	Almost   []AchItem
	Recent   []AchItem
	Matched  []string
	ExpDone  int
	ExpTotal int
	Updated  string
}

type achProfileEntry struct {
	view    *achView
	fetched time.Time
	loading bool
	err     string
}

var achProfiles = struct {
	sync.Mutex
	m map[string]*achProfileEntry
}{m: map[string]*achProfileEntry{}}

func resetAchProfiles() {
	achProfiles.Lock()
	achProfiles.m = map[string]*achProfileEntry{}
	achProfiles.Unlock()
}

type apiCrit struct {
	Amount      int64     `json:"amount"`
	IsCompleted bool      `json:"is_completed"`
	Child       []apiCrit `json:"child_criteria"`
}

func critLeaves(c apiCrit) (done, total int) {
	if len(c.Child) == 0 {
		if c.IsCompleted {
			return 1, 1
		}
		return 0, 1
	}
	for _, ch := range c.Child {
		d, t := critLeaves(ch)
		done += d
		total += t
	}
	return
}

func buildAchView(client *http.Client, key string) (*achView, error) {
	// Resolve the URL for this selection
	var u string
	for _, c := range eligibleCharacters(10) {
		if strings.ToLower(c.Name+"-"+c.Realm) == key {
			u = characterURL(c.Realm, c.Name, "/achievements")
			break
		}
	}
	if u == "" {
		return nil, fmt.Errorf("unknown character %q", key)
	}

	var resp struct {
		TotalPoints   int `json:"total_points"`
		TotalQuantity int `json:"total_quantity"`
		Achievements  []struct {
			ID          int `json:"id"`
			Achievement struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"achievement"`
			Criteria           *apiCrit `json:"criteria"`
			CompletedTimestamp int64    `json:"completed_timestamp"`
		} `json:"achievements"`
	}
	if _, err := getJSON(client, u, &resp); err != nil {
		return nil, err
	}

	type prog struct {
		name   string
		done   bool
		ts     int64
		pct    int
		amount int64
		needs  bool // needs static lookup to compute pct
	}
	state := map[int]*prog{}
	var needStatic []int
	for _, a := range resp.Achievements {
		id := a.Achievement.ID
		if id == 0 {
			id = a.ID
		}
		p := &prog{name: a.Achievement.Name, ts: a.CompletedTimestamp}
		switch {
		case a.CompletedTimestamp > 0:
			p.done, p.pct = true, 100
		case a.Criteria == nil:
		default:
			if len(a.Criteria.Child) > 0 {
				d, t := critLeaves(*a.Criteria)
				if t > 0 {
					p.pct = d * 100 / t
				}
			} else if a.Criteria.IsCompleted {
				p.pct = 100
			} else if a.Criteria.Amount > 0 {
				p.amount, p.needs = a.Criteria.Amount, true
				needStatic = append(needStatic, id)
			}
		}
		state[id] = p
	}

	// Single-counter achievements need the required amount from the static definition.
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, id := range needStatic {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if s := getStaticAch(client, id); s != nil && s.Required > 0 {
				p := state[id]
				pct := int(p.amount * 100 / s.Required)
				if pct > 99 {
					pct = 99
				}
				p.pct = pct
			}
		}(id)
	}
	wg.Wait()

	// Snapshot catalogue
	achCats.Lock()
	byID, achToCat := achCats.byID, achCats.achToCat
	achCats.Unlock()

	scope := loadAchScope()
	names := map[string]bool{}
	for _, n := range scope.ExpansionNames {
		names[strings.ToLower(strings.TrimSpace(n))] = true
	}
	excluded := map[string]bool{}
	for _, n := range scope.ExcludedRoots {
		excluded[strings.ToLower(n)] = true
	}

	inExpansion := func(id int) bool {
		for i := 0; i < 8 && id != 0; i++ {
			c, ok := byID[id]
			if !ok {
				return false
			}
			if names[strings.ToLower(c.Name)] {
				return true
			}
			id = c.Parent
		}
		return false
	}

	view := &achView{Points: resp.TotalPoints, Overall: resp.TotalQuantity, Updated: time.Now().Format("15:04:05")}
	expSet := map[int]bool{}

	item := func(id int, fallbackName string) AchItem {
		it := AchItem{ID: id, Name: fallbackName, Status: "none"}
		if p, ok := state[id]; ok {
			if it.Name == "" {
				it.Name = p.name
			}
			it.Pct = p.pct
			switch {
			case p.done:
				it.Status = "done"
			case p.pct > 0:
				it.Status = "prog"
			}
		}
		return it
	}

	var catIDs []int
	for id := range byID {
		catIDs = append(catIDs, id)
	}
	sort.Ints(catIDs)
	for _, cid := range catIDs {
		c := byID[cid]
		if c.Guild || len(c.Achievements) == 0 || !inExpansion(cid) {
			continue
		}
		g := AchGroup{Name: catPath(byID, cid)}
		for _, a := range c.Achievements {
			it := item(a.ID, a.Name)
			expSet[a.ID] = true
			g.Items = append(g.Items, it)
			g.Total++
			if it.Status == "done" {
				g.Done++
			}
		}
		sort.SliceStable(g.Items, func(i, j int) bool {
			ri, rj := g.Items[i].Status == "done", g.Items[j].Status == "done"
			if ri != rj {
				return !ri
			}
			if g.Items[i].Pct != g.Items[j].Pct {
				return g.Items[i].Pct > g.Items[j].Pct
			}
			return g.Items[i].Name < g.Items[j].Name
		})
		view.Groups = append(view.Groups, g)
		view.ExpDone += g.Done
		view.ExpTotal += g.Total
		view.Matched = append(view.Matched, g.Name)
	}
	sort.Slice(view.Groups, func(i, j int) bool { return view.Groups[i].Name < view.Groups[j].Name })

	// ≥ threshold elsewhere
	for id, p := range state {
		if p.done || p.pct < scope.AlmostThreshold || expSet[id] {
			continue
		}
		cid, ok := achToCat[id]
		if ok {
			if c := byID[cid]; c != nil && c.Guild {
				continue
			}
			if root := catRoot(byID, cid); root != nil && excluded[strings.ToLower(root.Name)] {
				continue
			}
		}
		it := item(id, "")
		if ok {
			it.Path = catPath(byID, cid)
			for _, a := range byID[cid].Achievements {
				if a.ID == id {
					it.Name = a.Name
				}
			}
		}
		if it.Name == "" {
			it.Name = fmt.Sprintf("Achievement #%d", id)
		}
		view.Almost = append(view.Almost, it)
	}
	sort.Slice(view.Almost, func(i, j int) bool {
		if view.Almost[i].Pct != view.Almost[j].Pct {
			return view.Almost[i].Pct > view.Almost[j].Pct
		}
		return view.Almost[i].Name < view.Almost[j].Name
	})

	// Recently completed
	type rec struct {
		id int
		ts int64
	}
	var recs []rec
	for id, p := range state {
		if p.done {
			recs = append(recs, rec{id, p.ts})
		}
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].ts > recs[j].ts })
	for i := 0; i < len(recs) && i < 10; i++ {
		it := item(recs[i].id, "")
		it.When = time.UnixMilli(recs[i].ts).Format("02 Jan 2006")
		if cid, ok := achToCat[recs[i].id]; ok {
			it.Path = catPath(byID, cid)
			for _, a := range byID[cid].Achievements {
				if a.ID == recs[i].id {
					it.Name = a.Name
				}
			}
		}
		if it.Name == "" {
			it.Name = fmt.Sprintf("Achievement #%d", recs[i].id)
		}
		view.Recent = append(view.Recent, it)
	}
	return view, nil
}

func fetchAchProfile(key string) {
	view, err := func() (*achView, error) {
		client := apiClient()
		if client == nil {
			return nil, fmt.Errorf("not logged in")
		}
		return buildAchView(client, key)
	}()
	achProfiles.Lock()
	defer achProfiles.Unlock()
	e := achProfiles.m[key]
	if e == nil {
		return
	}
	e.loading = false
	if err != nil {
		e.err = err.Error()
		log.Printf("Achievements %s: %v", key, err)
		return
	}
	e.view, e.err, e.fetched = view, "", time.Now()
	log.Printf("Achievements built for %s: %d expansion groups, %d almost, %d recent", key, len(view.Groups), len(view.Almost), len(view.Recent))
}

// ── Page ─────────────────────────────────────────────────────────────────────

type AchChip struct {
	Key, Label string
	Active     bool
}

type AchPage struct {
	Loading   bool
	Err       string
	Chips     []AchChip
	Selected  string
	View      *achView
	Scope     achScope
	NoMatches bool
}

func handleAchievements(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if apiClient() == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther) // home shows the Battle.net login screen
		return
	}
	if r.URL.Query().Get("refresh") == "1" {
		resetAchProfiles()
		achCats.Lock()
		if achCats.err != "" {
			achCats.fetched = time.Time{}
		}
		achCats.Unlock()
		http.Redirect(w, r, "/achievements?c="+url.QueryEscape(r.URL.Query().Get("c")), http.StatusSeeOther)
		return
	}

	chars := eligibleCharacters(10)
	page := AchPage{Scope: loadAchScope()}
	sel := strings.ToLower(r.URL.Query().Get("c"))
	valid := false
	for _, c := range chars {
		if strings.ToLower(c.Name+"-"+c.Realm) == sel {
			valid = true
		}
	}
	if !valid && len(chars) > 0 {
		sel = strings.ToLower(chars[0].Name + "-" + chars[0].Realm)
	}
	page.Selected = sel
	for _, c := range chars {
		k := strings.ToLower(c.Name + "-" + c.Realm)
		page.Chips = append(page.Chips, AchChip{Key: k, Label: c.Name, Active: k == sel})
	}

	// Catalogue (24h cache, shared by everyone)
	achCats.Lock()
	catStale := achCats.fetched.IsZero() || time.Since(achCats.fetched) > catTTL
	catErr := achCats.err
	if catStale && !achCats.loading && achCats.err == "" {
		achCats.loading = true
		go loadAchCategories()
	}
	catReady := !achCats.fetched.IsZero()
	achCats.Unlock()

	if catErr != "" && !catReady {
		page.Err = catErr
	} else if catReady {
		achProfiles.Lock()
		e := achProfiles.m[sel]
		if e == nil {
			e = &achProfileEntry{}
			achProfiles.m[sel] = e
		}
		if (e.view == nil || time.Since(e.fetched) > cacheTTL) && !e.loading && e.err == "" {
			e.loading = true
			go fetchAchProfile(sel)
		}
		page.View = e.view
		page.Err = e.err
		achProfiles.Unlock()
	}
	page.Loading = page.Err == "" && page.View == nil
	if page.View != nil && len(page.View.Groups) == 0 {
		page.NoMatches = true
	}

	tmpl, err := template.New("achievements-page").Funcs(template.FuncMap{
		"join":  strings.Join,
		"lower": strings.ToLower,
	}).Parse(achievementsTemplate)
	if err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tmpl.Execute(w, page); err != nil {
		log.Printf("Achievements template error: %v", err)
	}
}

var achievementsTemplate = pageHead + `
.page-header h1 { color: #a0f0ff; }
.chips { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 20px; }
.chip { padding: 6px 12px; border: 1px solid #4a4030; border-radius: 16px; color: #f8e6c8; text-decoration: none;
  font-size: .85em; text-transform: capitalize; background: rgba(0,0,0,.3); transition: all .15s; }
.chip:hover { border-color: #30b8c8; }
.chip.active { background: rgba(48,184,200,.2); border-color: #30b8c8; color: #a0f0ff; }
.summary { display: flex; gap: 28px; flex-wrap: wrap; }
.summary .stat b { display: block; font-size: 1.7em; color: #ffe680; }
.summary .stat span { color: #9b8a6e; font-size: .8em; text-transform: uppercase; letter-spacing: 1px; }
.toolbar { display: flex; gap: 10px; flex-wrap: wrap; align-items: center; margin-bottom: 18px; }
.toolbar input { padding: 8px 12px; border-radius: 6px; border: 1px solid #5a4020; background: rgba(0,0,0,.3); color: #f8e6c8; min-width: 240px; font-family: inherit; }
.fbtn { padding: 6px 12px; border-radius: 6px; border: 1px solid #5a4020; background: rgba(58,42,16,.6); color: #f8e6c8; cursor: pointer; font-family: inherit; font-size: .85em; }
.fbtn.on { border-color: #30b8c8; background: rgba(48,184,200,.2); color: #a0f0ff; }
details.grp { margin-bottom: 12px; }
details.grp > summary { cursor: pointer; padding: 10px 14px; background: rgba(0,0,0,.3); border: 1px solid #3d3020; border-radius: 8px;
  list-style: none; display: flex; justify-content: space-between; gap: 14px; align-items: center; }
details.grp > summary::-webkit-details-marker { display: none; }
details.grp[open] > summary { border-color: #30b8c8; }
.grp-meta { color: #9b8a6e; font-size: .85em; white-space: nowrap; }
.list { display: grid; grid-template-columns: repeat(auto-fill, minmax(320px, 1fr)); gap: 8px; padding: 10px 2px; }
.ach { background: rgba(20,20,20,.45); border: 1px solid rgba(60,50,30,.5); border-radius: 6px; padding: 8px 10px; font-size: .9em; }
.ach.done { border-color: rgba(32,200,122,.45); }
.ach .top { display: flex; align-items: center; gap: 8px; }
.ach .nm { flex: 1; cursor: pointer; min-width: 0; }
.ach.done .nm { color: #80ffcc; }
.ach.none .nm { color: #b8a888; }
.ach .pin { background: none; border: none; color: #6a5a3a; cursor: pointer; font-size: 1.1em; }
.ach .pin.on { color: #ffd040; }
.ach .path { color: #7a6a50; font-size: .75em; }
.ach .pct { color: #ffe680; font-size: .8em; white-space: nowrap; }
.ach .bar { display: block; height: 5px; background: #1a1610; border-radius: 3px; margin-top: 6px; overflow: hidden; }
.ach .bar i { display: block; height: 100%; background: linear-gradient(90deg, #30b8c8, #80ffcc); }
.ach.done .bar i { background: #20c87a; }
.desc { margin-top: 6px; color: #9b8a6e; font-size: .85em; line-height: 1.4; }
.hidden-by-filter { display: none !important; }
</style>
<title>Achievements</title>
` + pageNav[len("</style>"):] + `
<div class="page-wrap">
  <div class="page-header">
    <h1>🏆 Achievements</h1>
    <p>{{join .Scope.ExpansionNames ", "}} achievements, plus anything else you're {{.Scope.AlmostThreshold}}%+ of the way through</p>
  </div>

  <div class="chips">
  {{range .Chips}}<a class="chip {{if .Active}}active{{end}}" data-key="{{.Key}}" href="/achievements?c={{.Key | urlquery}}">{{.Label}}</a>{{end}}
  </div>

{{if .Err}}
  <div class="card notice-box"><h2>Couldn't load achievements</h2><p>{{.Err}}</p>
  <p><a href="/achievements?refresh=1&c={{.Selected | urlquery}}" style="color:#80c0ff">Try again</a></p></div>
{{else if .Loading}}
  <div class="card notice-box"><h2>Loading achievements…</h2>
  <p>The first load fetches Blizzard's achievement catalogue and may take up to a minute. This page refreshes automatically.</p></div>
  <script>setTimeout(function(){ location.reload(); }, 3000);</script>
{{else}}
  {{with .View}}
  <div class="card summary">
    <div class="stat"><b>{{.Points}}</b><span>Achievement points</span></div>
    <div class="stat"><b>{{.Overall}}</b><span>Completed overall</span></div>
    <div class="stat"><b>{{.ExpDone}} / {{.ExpTotal}}</b><span>Current expansion</span></div>
  </div>

  <div class="toolbar">
    <input id="search" type="search" placeholder="Search achievements…">
    <button class="fbtn on" data-f="all">All</button>
    <button class="fbtn" data-f="prog">In progress</button>
    <button class="fbtn" data-f="done">Completed</button>
    <button class="fbtn" data-f="none">Not started</button>
  </div>

  <div id="pinned-wrap" style="display:none">
    <h2 class="section-title">⭐ Pinned</h2>
    <div class="list" id="pinned"></div>
  </div>

  {{if $.NoMatches}}
  <div class="card notice-box"><h2>No current-expansion categories matched</h2>
  <p>No achievement category is named “{{join $.Scope.ExpansionNames ", "}}”. Edit <code>data/achievement_scope.json</code> to set the right category name(s).</p></div>
  {{end}}

  {{if .Almost}}
  <h2 class="section-title">🔥 Almost there — {{$.Scope.AlmostThreshold}}%+ (other expansions)</h2>
  <div class="list" id="almost">
  {{range .Almost}}{{template "ach" .}}{{end}}
  </div>
  {{end}}

  {{if .Groups}}<h2 class="section-title">Current expansion</h2>{{end}}
  <div id="groups">
  {{range .Groups}}
    <details class="grp" {{if lt .Done .Total}}open{{end}}>
      <summary><span>{{.Name}}</span><span class="grp-meta">{{.Done}} / {{.Total}}</span></summary>
      <div class="list">{{range .Items}}{{template "ach" .}}{{end}}</div>
    </details>
  {{end}}
  </div>

  {{if .Recent}}
  <h2 class="section-title">Recently completed</h2>
  <div class="list" id="recent">{{range .Recent}}{{template "ach" .}}{{end}}</div>
  {{end}}

  <p class="muted" style="margin-top:20px;font-size:.8em">Refreshed at {{.Updated}} · cached for 10 minutes · <a href="/achievements?refresh=1&c={{$.Selected | urlquery}}" style="color:#80c0ff">force refresh</a> · scope is editable in data/achievement_scope.json</p>
  {{end}}
{{end}}
</div>

<script>
(function(){
  var sel = document.querySelector('.chip.active');
  // Remember the selected character; restore it when visiting without ?c=
  if (!location.search.match(/[?&]c=/)) {
    var last = localStorage.getItem('achSelected');
    if (last && document.querySelector('.chip[data-key="' + last + '"]') && (!sel || sel.dataset.key !== last)) {
      location.replace('/achievements?c=' + encodeURIComponent(last));
      return;
    }
  }
  if (sel) localStorage.setItem('achSelected', sel.dataset.key);

  var filter = 'all', pins = [];
  try { pins = JSON.parse(localStorage.getItem('achPins') || '[]'); } catch(e) {}
  var search = document.getElementById('search');

  function applyFilter() {
    var q = search ? search.value.toLowerCase().trim() : '';
    document.querySelectorAll('.ach').forEach(function(el) {
      var ok = (filter === 'all' || el.dataset.status === filter) && (!q || el.dataset.name.indexOf(q) !== -1);
      el.classList.toggle('hidden-by-filter', !ok);
    });
    document.querySelectorAll('details.grp').forEach(function(d) {
      var any = d.querySelector('.ach:not(.hidden-by-filter)');
      d.classList.toggle('hidden-by-filter', !any);
      if (q && any) d.open = true;
    });
  }
  document.querySelectorAll('.fbtn').forEach(function(b) {
    b.addEventListener('click', function() {
      filter = b.dataset.f;
      document.querySelectorAll('.fbtn').forEach(function(x) { x.classList.toggle('on', x === b); });
      applyFilter();
    });
  });
  if (search) search.addEventListener('input', applyFilter);

  function renderPins() {
    var wrap = document.getElementById('pinned-wrap'), box = document.getElementById('pinned');
    if (!box) return;
    box.innerHTML = '';
    pins.forEach(function(id) {
      var src = document.querySelector('#groups .ach[data-id="' + id + '"], #almost .ach[data-id="' + id + '"], #recent .ach[data-id="' + id + '"]');
      if (src) box.appendChild(src.cloneNode(true));
    });
    wrap.style.display = box.children.length ? '' : 'none';
    document.querySelectorAll('.ach .pin').forEach(function(p) {
      p.classList.toggle('on', pins.indexOf(p.closest('.ach').dataset.id) !== -1);
    });
    applyFilter();
  }

  document.addEventListener('click', function(e) {
    var pin = e.target.closest('.pin');
    if (pin) {
      var id = pin.closest('.ach').dataset.id, i = pins.indexOf(id);
      if (i === -1) pins.push(id); else pins.splice(i, 1);
      localStorage.setItem('achPins', JSON.stringify(pins));
      renderPins();
      return;
    }
    var nm = e.target.closest('.nm');
    if (nm) {
      var card = nm.closest('.ach'), d = card.querySelector('.desc');
      if (d.dataset.loaded) { d.style.display = d.style.display === 'none' ? '' : 'none'; return; }
      d.dataset.loaded = '1'; d.textContent = 'Loading…';
      fetch('/api/achievement?id=' + card.dataset.id).then(function(r) { return r.json(); }).then(function(a) {
        d.textContent = (a.description || 'No description') + (a.reward ? '  🎁 ' + a.reward : '') + (a.points ? '  (' + a.points + ' pts)' : '');
      }).catch(function() { d.textContent = 'Could not load details.'; delete d.dataset.loaded; });
    }
  });
  renderPins();
})();
</script>
</body></html>
{{define "ach"}}
<div class="ach {{.Status}}" data-id="{{.ID}}" data-status="{{.Status}}" data-name="{{.Name | lower}}">
  <div class="top">
    <span class="nm">{{if eq .Status "done"}}✔ {{end}}{{.Name}}</span>
    {{if eq .Status "prog"}}<span class="pct">{{.Pct}}%</span>{{end}}
    {{if .When}}<span class="pct">{{.When}}</span>{{end}}
    <button class="pin" title="Pin">★</button>
  </div>
  {{if .Path}}<div class="path">{{.Path}}</div>{{end}}
  {{if or (eq .Status "prog") (eq .Status "done")}}<span class="bar"><i style="width:{{.Pct}}%"></i></span>{{end}}
  <div class="desc"></div>
</div>
{{end}}`
