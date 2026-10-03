package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"
)

// ── Types ────────────────────────────────────────────────────────────────────

type MPRun struct {
	Dungeon    string
	Level      int
	Timed      bool
	Rating     float64
	DurationMs int64
	Completed  int64 // unix ms
	Affixes    []string
}

type VaultSlot struct {
	Label    string // "1st run", "4th run", "8th run"
	Level    int
	Ilvl     int
	Unlocked bool
}

type MPChar struct {
	Name     string
	Realm    string
	Level    int
	Rating   float64
	Color    template.CSS
	Best     map[string]MPRun // best run per dungeon
	WeekRuns int
	Vault    []VaultSlot
	HasData  bool
}

type MPCell struct {
	Has     bool
	Level   int
	Timed   bool
	Tooltip string
}

type MPRow struct {
	Char  MPChar
	Cells []MPCell
}

type MPPageData struct {
	Loading   bool
	Dungeons  []string
	Rows      []MPRow
	Updated   string
	VaultNote string
}

// ── Cache ────────────────────────────────────────────────────────────────────

var mpState struct {
	sync.Mutex
	data    []MPChar
	fetched time.Time
	loading bool
}

// clearExtraCaches is called from /refresh so the lazy pages re-fetch.
func clearExtraCaches() {
	mpState.Lock()
	mpState.fetched = time.Time{}
	mpState.Unlock()
	eqState.Lock()
	eqState.fetched = time.Time{}
	eqState.Unlock()
	resetAchProfiles()
}

// ── Vault reward table ───────────────────────────────────────────────────────

type vaultTable struct {
	Note       string         `json:"note"`
	MythicPlus map[string]int `json:"mythic_plus"`
}

func loadVaultTable() vaultTable {
	var t vaultTable
	if b, err := os.ReadFile("data/vault_ilvl.json"); err == nil {
		if err := json.Unmarshal(b, &t); err != nil {
			log.Printf("vault_ilvl.json parse error: %v", err)
		}
	}
	return t
}

// vaultIlvl returns the reward item level for a key level (highest table entry <= level).
func (t vaultTable) vaultIlvl(level int) int {
	best, bestKey := 0, -1
	for k, v := range t.MythicPlus {
		kl, err := strconv.Atoi(k)
		if err == nil && kl <= level && kl > bestKey {
			best, bestKey = v, kl
		}
	}
	return best
}

// lastWeeklyReset returns the most recent weekly reset time for the configured region.
func lastWeeklyReset(now time.Time) time.Time {
	now = now.UTC()
	weekday, hour := time.Tuesday, 15 // US
	switch config.Region {
	case "eu":
		weekday, hour = time.Wednesday, 4
	case "kr", "tw":
		weekday, hour = time.Thursday, 1
	}
	reset := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, time.UTC)
	for reset.Weekday() != weekday || reset.After(now) {
		reset = reset.AddDate(0, 0, -1)
	}
	return reset
}

// ── Fetching ─────────────────────────────────────────────────────────────────

func fetchMythicProfile(client *http.Client, c CharacterStats, vt vaultTable, reset time.Time) MPChar {
	out := MPChar{Name: c.Name, Realm: c.Realm, Level: c.Level, Best: map[string]MPRun{}, Color: "#f8e6c8"}

	var prof struct {
		Rating *struct {
			Rating float64               `json:"rating"`
			Color  struct{ R, G, B int } `json:"color"`
		} `json:"current_mythic_rating"`
		Seasons []struct {
			ID int `json:"id"`
		} `json:"seasons"`
	}
	status, err := getJSON(client, characterURL(c.Realm, c.Name, "/mythic-keystone-profile"), &prof)
	if err != nil {
		if status != http.StatusNotFound {
			log.Printf("M+ profile %s-%s: %v", c.Realm, c.Name, err)
		}
		return out
	}
	if prof.Rating != nil {
		out.Rating = prof.Rating.Rating
		col := prof.Rating.Color
		out.Color = template.CSS(fmt.Sprintf("rgb(%d,%d,%d)", col.R, col.G, col.B))
	}
	if len(prof.Seasons) == 0 {
		return out
	}
	// Latest season = highest id
	seasonID := 0
	for _, s := range prof.Seasons {
		if s.ID > seasonID {
			seasonID = s.ID
		}
	}

	var season struct {
		BestRuns []struct {
			Completed int64 `json:"completed_timestamp"`
			Duration  int64 `json:"duration"`
			Level     int   `json:"keystone_level"`
			InTime    bool  `json:"is_completed_within_time"`
			Dungeon   struct {
				Name string `json:"name"`
			} `json:"dungeon"`
			Affixes []struct {
				Name string `json:"name"`
			} `json:"keystone_affixes"`
			Rating struct {
				Rating float64 `json:"rating"`
			} `json:"mythic_rating"`
		} `json:"best_runs"`
	}
	if _, err := getJSON(client, characterURL(c.Realm, c.Name, "/mythic-keystone-profile/season/"+strconv.Itoa(seasonID)), &season); err != nil {
		return out
	}

	var weekLevels []int
	for _, r := range season.BestRuns {
		run := MPRun{Dungeon: r.Dungeon.Name, Level: r.Level, Timed: r.InTime, Rating: r.Rating.Rating,
			DurationMs: r.Duration, Completed: r.Completed}
		for _, a := range r.Affixes {
			run.Affixes = append(run.Affixes, a.Name)
		}
		out.HasData = true
		if cur, ok := out.Best[run.Dungeon]; !ok || run.Level > cur.Level ||
			(run.Level == cur.Level && run.Rating > cur.Rating) {
			out.Best[run.Dungeon] = run
		}
		if time.UnixMilli(r.Completed).After(reset) {
			weekLevels = append(weekLevels, r.Level)
		}
	}
	out.WeekRuns = len(weekLevels)
	sort.Sort(sort.Reverse(sort.IntSlice(weekLevels)))
	for i, pos := range []int{1, 4, 8} {
		slot := VaultSlot{Label: fmt.Sprintf("%d%s run", pos, ordinal(pos))}
		if len(weekLevels) >= pos {
			slot.Unlocked = true
			slot.Level = weekLevels[pos-1]
			slot.Ilvl = vt.vaultIlvl(slot.Level)
		}
		_ = i
		out.Vault = append(out.Vault, slot)
	}
	return out
}

func ordinal(n int) string {
	switch n {
	case 1:
		return "st"
	case 2:
		return "nd"
	case 3:
		return "rd"
	}
	return "th"
}

func fetchAllMythic() {
	defer func() {
		mpState.Lock()
		mpState.loading = false
		mpState.Unlock()
	}()
	client := apiClient()
	if client == nil {
		return
	}
	vt := loadVaultTable()
	reset := lastWeeklyReset(time.Now())
	chars := eligibleCharacters(10)

	results := make([]MPChar, len(chars))
	sem := make(chan struct{}, 5)
	var wg sync.WaitGroup
	for i, c := range chars {
		wg.Add(1)
		go func(i int, c CharacterStats) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = fetchMythicProfile(client, c, vt, reset)
		}(i, c)
	}
	wg.Wait()

	// Highest rating first, then level
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Rating != results[j].Rating {
			return results[i].Rating > results[j].Rating
		}
		return results[i].Level > results[j].Level
	})

	mpState.Lock()
	mpState.data = results
	mpState.fetched = time.Now()
	mpState.Unlock()
	log.Printf("Mythic+ data fetched for %d characters", len(results))
}

// ── Handler ──────────────────────────────────────────────────────────────────

func fmtDuration(ms int64) string {
	s := ms / 1000
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func handleMythicPlus(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("refresh") == "1" {
		clearExtraCaches()
		http.Redirect(w, r, r.URL.Path, http.StatusSeeOther)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if apiClient() == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther) // home shows the Battle.net login screen
		return
	}

	mpState.Lock()
	stale := mpState.fetched.IsZero() || time.Since(mpState.fetched) > cacheTTL
	if stale && !mpState.loading {
		mpState.loading = true
		go fetchAllMythic()
	}
	data := mpState.data
	fetched := mpState.fetched
	mpState.Unlock()

	page := MPPageData{Loading: fetched.IsZero()}
	if !page.Loading {
		page.Updated = fetched.Format("15:04:05")
		vt := loadVaultTable()
		page.VaultNote = vt.Note

		dset := map[string]bool{}
		for _, c := range data {
			for d := range c.Best {
				dset[d] = true
			}
		}
		for d := range dset {
			page.Dungeons = append(page.Dungeons, d)
		}
		sort.Strings(page.Dungeons)

		for _, c := range data {
			row := MPRow{Char: c}
			for _, d := range page.Dungeons {
				run, ok := c.Best[d]
				cell := MPCell{Has: ok}
				if ok {
					cell.Level, cell.Timed = run.Level, run.Timed
					status := "Depleted"
					if run.Timed {
						status = "Timed"
					}
					cell.Tooltip = fmt.Sprintf("%s +%d — %s in %s (rating %.0f)", d, run.Level, status, fmtDuration(run.DurationMs), run.Rating)
					if len(run.Affixes) > 0 {
						cell.Tooltip += " — affixes: "
						for i, a := range run.Affixes {
							if i > 0 {
								cell.Tooltip += ", "
							}
							cell.Tooltip += a
						}
					}
				}
				row.Cells = append(row.Cells, cell)
			}
			page.Rows = append(page.Rows, row)
		}
	}

	tmpl, err := template.New("mplus").Parse(mythicPlusTemplate)
	if err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tmpl.Execute(w, page); err != nil {
		log.Printf("Mythic+ template error: %v", err)
	}
}

var mythicPlusTemplate = pageHead + `
.page-header h1 { color: #ffc890; }
.mp-table { width: 100%; border-collapse: collapse; font-size: .9em; }
.mp-table th, .mp-table td { padding: 8px 10px; text-align: center; border-bottom: 1px solid #2a2418; }
.mp-table th { color: #c8a050; font-weight: 400; font-size: .78em; letter-spacing: 1px; text-transform: uppercase; vertical-align: bottom; }
.mp-table td.name { text-align: left; white-space: nowrap; }
.mp-table td.name small { color: #9b8a6e; display: block; font-size: .78em; text-transform: capitalize; }
.mp-table tr:hover td { background: rgba(255,255,255,0.03); }
.score { font-weight: bold; font-size: 1.1em; text-shadow: 1px 1px 3px rgba(0,0,0,.8); }
.key { display: inline-block; min-width: 42px; padding: 3px 8px; border-radius: 4px; font-weight: bold; cursor: help; }
.key.timed { background: rgba(32,200,122,.15); border: 1px solid #20c87a; color: #80ffcc; }
.key.depleted { background: rgba(160,64,64,.15); border: 1px solid #a04040; color: #ffaaaa; }
.none { color: #4a4030; }
.vault-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 14px; }
.vault-card { border: 1px solid #3d3020; border-radius: 8px; padding: 12px 16px; background: rgba(0,0,0,.25); }
.vault-card h4 { margin-bottom: 8px; text-transform: capitalize; font-weight: 400; }
.vault-card h4 small { color: #9b8a6e; }
.slots { display: flex; gap: 8px; }
.slot { flex: 1; text-align: center; padding: 8px 4px; border-radius: 6px; border: 1px dashed #4a4030; color: #4a4030; font-size: .8em; }
.slot.on { border: 1px solid #a060e0; background: rgba(160,96,224,.12); color: #e8d0ff; }
.slot b { display: block; font-size: 1.15em; color: #fff; }
</style>
<title>Mythic+ Breakdown</title>
` + pageNav[len("</style>"):] + `
<div class="page-wrap">
  <div class="page-header">
    <h1>🗝️ Mythic+ Breakdown</h1>
    <p>Best key per dungeon for the current season, across your whole roster</p>
  </div>
{{if .Loading}}
  <div class="card notice-box"><h2>Fetching Mythic+ data…</h2>
  <p>Loading from Battle.net — this page will refresh automatically.</p></div>
  <script>setTimeout(function(){ location.reload(); }, 3000);</script>
{{else if not .Rows}}
  <div class="card notice-box"><h2>No characters found</h2><p>No eligible characters. Try <a href="?refresh=1" style="color:#80c0ff">refreshing</a>.</p></div>
{{else}}
  <div class="card" style="overflow-x:auto">
    <table class="mp-table">
      <thead><tr><th style="text-align:left">Character</th><th>M+ Rating</th>
      {{range .Dungeons}}<th>{{.}}</th>{{end}}</tr></thead>
      <tbody>
      {{range .Rows}}
        <tr>
          <td class="name">{{.Char.Name}}<small>{{.Char.Realm}} · {{.Char.Level}}</small></td>
          <td>{{if .Char.Rating}}<span class="score" style="color:{{.Char.Color}}">{{printf "%.0f" .Char.Rating}}</span>{{else}}<span class="none">—</span>{{end}}</td>
          {{range .Cells}}
            <td>{{if .Has}}<span class="key {{if .Timed}}timed{{else}}depleted{{end}}" title="{{.Tooltip}}">+{{.Level}}</span>{{else}}<span class="none">·</span>{{end}}</td>
          {{end}}
        </tr>
      {{end}}
      </tbody>
    </table>
  </div>

  <h2 class="section-title">Great Vault — Mythic+ Rewards This Week</h2>
  <p class="muted" style="margin-bottom:14px;font-size:.85em">
    Estimated from this week's runs that Blizzard reports as best-in-dungeon. Runs that were
    overwritten by a higher key in the same dungeon are not visible to the API, so the real vault may be higher.
    {{.VaultNote}}</p>
  <div class="vault-grid">
  {{range .Rows}}{{if .Char.WeekRuns}}
    <div class="vault-card">
      <h4>{{.Char.Name}} <small>{{.Char.WeekRuns}} run(s) this week</small></h4>
      <div class="slots">
      {{range .Char.Vault}}
        <div class="slot {{if .Unlocked}}on{{end}}">{{.Label}}
          {{if .Unlocked}}<b>{{if .Ilvl}}{{.Ilvl}}{{else}}?{{end}}</b>+{{.Level}}{{else}}locked{{end}}</div>
      {{end}}
      </div>
    </div>
  {{end}}{{end}}
  </div>
  <p class="muted" style="margin-top:20px;font-size:.8em">Data refreshed at {{.Updated}} · cached for 10 minutes · <a href="?refresh=1" style="color:#80c0ff">force refresh</a></p>
{{end}}
</div>
</body></html>`
