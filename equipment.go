package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ── Types ────────────────────────────────────────────────────────────────────

type SocketView struct {
	Gem   string
	Empty bool
}

type EquippedItem struct {
	ID             int64
	Name           string
	Ilvl           int
	Quality        string // lowercase quality type: epic, rare, ...
	Icon           string
	Enchant        string
	MissingEnchant bool
	Sockets        []SocketView
	SetName        string
}

type GearStats struct {
	Crit, Haste, Mastery, Vers float64
	CritRating, HasteRating    int
	MasteryRating, VersRating  int
	Primary                    int
	PrimaryName                string
	Stamina, Armor             int
}

type EQChar struct {
	Name, Realm string
	Level       int
	Ilvl        int
	Thumb       string
	Items       map[string]EquippedItem
	Stats       GearStats
	Warnings    []string
	Err         string
}

type SlotView struct {
	Label string
	Item  EquippedItem
	Has   bool
}

type EQPageData struct {
	Loading  bool
	Chars    []EQChar
	Selected *EQChar
	Left     []SlotView
	Right    []SlotView
	Bottom   []SlotView
	Updated  string
}

// ── Cache ────────────────────────────────────────────────────────────────────

var eqState struct {
	sync.Mutex
	data    []EQChar
	fetched time.Time
	loading bool
}

var (
	itemIconCache = map[int64]string{}
	iconMutex     sync.Mutex
)

// ── Gear rules ───────────────────────────────────────────────────────────────

type gearRules struct {
	MinLevel     int      `json:"min_level"`
	EnchantSlots []string `json:"enchant_slots"`
}

func loadGearRules() gearRules {
	r := gearRules{MinLevel: 80, EnchantSlots: []string{"CHEST", "LEGS", "FEET", "FINGER_1", "FINGER_2", "MAIN_HAND"}}
	if b, err := os.ReadFile("data/gear_rules.json"); err == nil {
		if err := json.Unmarshal(b, &r); err != nil {
			log.Printf("gear_rules.json parse error: %v", err)
		}
	}
	return r
}

// ── Fetching ─────────────────────────────────────────────────────────────────

func itemIcon(client *http.Client, id int64) string {
	iconMutex.Lock()
	if v, ok := itemIconCache[id]; ok {
		iconMutex.Unlock()
		return v
	}
	iconMutex.Unlock()

	var media struct {
		Assets []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"assets"`
	}
	url := fmt.Sprintf("https://%s.api.blizzard.com/data/wow/media/item/%d?namespace=static-%s&locale=%s",
		config.Region, id, config.Region, config.Locale)
	icon := ""
	if _, err := getJSON(client, url, &media); err == nil {
		for _, a := range media.Assets {
			if a.Key == "icon" {
				icon = a.Value
			}
		}
	}
	iconMutex.Lock()
	itemIconCache[id] = icon
	iconMutex.Unlock()
	return icon
}

func fetchEquipment(client *http.Client, c CharacterStats, rules gearRules) EQChar {
	out := EQChar{Name: c.Name, Realm: c.Realm, Level: c.Level, Ilvl: c.ItemLevel, Thumb: c.ThumbnailURL,
		Items: map[string]EquippedItem{}}

	var eq struct {
		Items []struct {
			Slot struct {
				Type string `json:"type"`
			} `json:"slot"`
			Item struct {
				ID int64 `json:"id"`
			} `json:"item"`
			Quality struct {
				Type string `json:"type"`
			} `json:"quality"`
			Name  string `json:"name"`
			Level struct {
				Value int `json:"value"`
			} `json:"level"`
			Enchantments []struct {
				Display string `json:"display_string"`
			} `json:"enchantments"`
			Sockets []struct {
				Item struct {
					Name string `json:"name"`
				} `json:"item"`
				Display string `json:"display_string"`
			} `json:"sockets"`
			Set struct {
				Name string `json:"display_string"`
				Set  struct {
					Name string `json:"name"`
				} `json:"item_set"`
			} `json:"set"`
		} `json:"equipped_items"`
	}
	if _, err := getJSON(client, characterURL(c.Realm, c.Name, "/equipment"), &eq); err != nil {
		out.Err = err.Error()
		return out
	}

	needEnchant := map[string]bool{}
	if c.Level >= rules.MinLevel {
		for _, s := range rules.EnchantSlots {
			needEnchant[s] = true
		}
	}

	for _, it := range eq.Items {
		item := EquippedItem{ID: it.Item.ID, Name: it.Name, Ilvl: it.Level.Value,
			Quality: strings.ToLower(it.Quality.Type), SetName: it.Set.Set.Name}
		item.Icon = itemIcon(client, it.Item.ID)
		for _, e := range it.Enchantments {
			if item.Enchant != "" {
				item.Enchant += "; "
			}
			item.Enchant += e.Display
		}
		if needEnchant[it.Slot.Type] && item.Enchant == "" {
			item.MissingEnchant = true
			out.Warnings = append(out.Warnings, fmt.Sprintf("%s: missing enchant", slotLabel(it.Slot.Type)))
		}
		for _, s := range it.Sockets {
			sv := SocketView{Gem: s.Item.Name, Empty: s.Item.Name == ""}
			if sv.Empty {
				out.Warnings = append(out.Warnings, fmt.Sprintf("%s: empty gem socket", slotLabel(it.Slot.Type)))
			}
			item.Sockets = append(item.Sockets, sv)
		}
		out.Items[it.Slot.Type] = item
	}

	var st map[string]interface{}
	if _, err := getJSON(client, characterURL(c.Realm, c.Name, "/statistics"), &st); err == nil {
		out.Stats = parseGearStats(st)
	}
	return out
}

func statNum(m map[string]interface{}, key, sub string) float64 {
	switch v := m[key].(type) {
	case map[string]interface{}:
		if f, ok := v[sub].(float64); ok {
			return f
		}
	case float64:
		return v
	}
	return 0
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func parseGearStats(st map[string]interface{}) GearStats {
	g := GearStats{
		Crit:          maxF(statNum(st, "melee_crit", "value"), statNum(st, "spell_crit", "value")),
		CritRating:    int(maxF(statNum(st, "melee_crit", "rating"), statNum(st, "spell_crit", "rating"))),
		Haste:         maxF(statNum(st, "melee_haste", "value"), statNum(st, "spell_haste", "value")),
		HasteRating:   int(maxF(statNum(st, "melee_haste", "rating"), statNum(st, "spell_haste", "rating"))),
		Mastery:       statNum(st, "mastery", "value"),
		MasteryRating: int(statNum(st, "mastery", "rating")),
		Vers:          statNum(st, "versatility_damage_done_bonus", ""),
		VersRating:    int(statNum(st, "versatility", "")),
		Stamina:       int(statNum(st, "stamina", "effective")),
		Armor:         int(statNum(st, "armor", "effective")),
	}
	for _, p := range []struct{ key, name string }{{"strength", "Strength"}, {"agility", "Agility"}, {"intellect", "Intellect"}} {
		if v := int(statNum(st, p.key, "effective")); v > g.Primary {
			g.Primary, g.PrimaryName = v, p.name
		}
	}
	return g
}

func fetchAllEquipment() {
	defer func() {
		eqState.Lock()
		eqState.loading = false
		eqState.Unlock()
	}()
	client := apiClient()
	if client == nil {
		return
	}
	rules := loadGearRules()
	chars := eligibleCharacters(60)

	results := make([]EQChar, len(chars))
	sem := make(chan struct{}, 5)
	var wg sync.WaitGroup
	for i, c := range chars {
		wg.Add(1)
		go func(i int, c CharacterStats) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = fetchEquipment(client, c, rules)
		}(i, c)
	}
	wg.Wait()

	eqState.Lock()
	eqState.data = results
	eqState.fetched = time.Now()
	eqState.Unlock()
	log.Printf("Equipment fetched for %d characters", len(results))
}

// ── Handler ──────────────────────────────────────────────────────────────────

var slotLabels = map[string]string{
	"HEAD": "Head", "NECK": "Neck", "SHOULDER": "Shoulders", "BACK": "Back", "CHEST": "Chest",
	"SHIRT": "Shirt", "TABARD": "Tabard", "WRIST": "Wrists", "HANDS": "Hands", "WAIST": "Waist",
	"LEGS": "Legs", "FEET": "Feet", "FINGER_1": "Ring 1", "FINGER_2": "Ring 2",
	"TRINKET_1": "Trinket 1", "TRINKET_2": "Trinket 2", "MAIN_HAND": "Main Hand", "OFF_HAND": "Off Hand",
}

func slotLabel(t string) string {
	if l, ok := slotLabels[t]; ok {
		return l
	}
	return t
}

func slotViews(c *EQChar, lang string, types ...string) []SlotView {
	var out []SlotView
	for _, t := range types {
		it, ok := c.Items[t]
		lbl := SlotLabel(lang, t)
		out = append(out, SlotView{Label: lbl, Item: it, Has: ok})
	}
	return out
}

func handleEquipment(w http.ResponseWriter, r *http.Request) {
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

	eqState.Lock()
	stale := eqState.fetched.IsZero() || time.Since(eqState.fetched) > cacheTTL
	if stale && !eqState.loading {
		eqState.loading = true
		go fetchAllEquipment()
	}
	data := eqState.data
	fetched := eqState.fetched
	eqState.Unlock()

	page := EQPageData{Loading: fetched.IsZero(), Chars: data}
	if !page.Loading && len(data) > 0 {
		page.Updated = fetched.Format("15:04:05")
		want := strings.ToLower(r.URL.Query().Get("c"))
		page.Selected = &data[0]
		for i := range data {
			if strings.ToLower(data[i].Name+"-"+data[i].Realm) == want {
				page.Selected = &data[i]
			}
		}
		lang := ResolveLanguage(r)
		page.Left = slotViews(page.Selected, lang, "HEAD", "NECK", "SHOULDER", "BACK", "CHEST", "SHIRT", "TABARD", "WRIST")
		page.Right = slotViews(page.Selected, lang, "HANDS", "WAIST", "LEGS", "FEET", "FINGER_1", "FINGER_2", "TRINKET_1", "TRINKET_2")
		page.Bottom = slotViews(page.Selected, lang, "MAIN_HAND", "OFF_HAND")
	}

	lang := ResolveLanguage(r)
	tmpl, err := template.New("equipment").Funcs(template.FuncMap{
		"key":   func(c EQChar) string { return c.Name + "-" + c.Realm },
		"lower": strings.ToLower,
		"pct":   func(v float64) string { return fmt.Sprintf("%.1f", v) },
		"bar": func(v float64) int {
			if v > 60 {
				v = 60
			}
			return int(v / 60 * 100)
		},
		"t": func(k string) string { return T(lang, k) },
		"tFmt": func(k string, args ...interface{}) string { return TFmt(lang, k, args...) },
		"statName": func(name string) string { return StatName(lang, name) },
		"lang": func() string { return lang },
		"formatWarn": func(w string) string {
			if lang != LangZhTW {
				return w
			}
			// w is "Slot: missing enchant" or "Slot: empty gem socket"
			parts := strings.SplitN(w, ": ", 2)
			if len(parts) == 2 {
				slot := parts[0]
				// Look up localized slot
				for _, st := range []string{"HEAD", "NECK", "SHOULDER", "BACK", "CHEST", "SHIRT", "TABARD", "WRIST", "HANDS", "WAIST", "LEGS", "FEET", "FINGER_1", "FINGER_2", "TRINKET_1", "TRINKET_2", "MAIN_HAND", "OFF_HAND"} {
					if slotLabels[st] == slot {
						slot = SlotLabel(lang, st)
						break
					}
				}
				if parts[1] == "missing enchant" {
					return TFmt(lang, "equip.warn_missing_enchant", slot)
				}
				if parts[1] == "empty gem socket" {
					return TFmt(lang, "equip.warn_empty_socket", slot)
				}
			}
			return w
		},
	}).Parse(BuildEquipmentTemplate(lang, r.URL.RequestURI()))
	if err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tmpl.Execute(w, page); err != nil {
		log.Printf("Equipment template error: %v", err)
	}
}

func BuildEquipmentTemplate(lang, currentPath string) string {
	title := T(lang, "equip.title")
	extraCSS := `
.page-header h1 { color: #ffc0e8; }
.chips { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 22px; }
.chip { padding: 6px 12px; border: 1px solid #4a4030; border-radius: 16px; color: #f8e6c8; text-decoration: none;
  font-size: .85em; text-transform: capitalize; background: rgba(0,0,0,.3); transition: all .15s; }
.chip:hover { border-color: #d050a0; }
.chip.active { background: rgba(208,80,160,.2); border-color: #d050a0; color: #ffc0e8; }
.chip .badge { background: #a04040; color: #fff; border-radius: 9px; padding: 0 6px; margin-left: 6px; font-size: .8em; }
.layout { display: grid; grid-template-columns: minmax(380px, 1fr) minmax(280px, 340px); gap: 24px; align-items: start; }
@media (max-width: 900px) { .layout { grid-template-columns: 1fr; } }
.char-title { display: flex; align-items: center; gap: 14px; margin-bottom: 18px; }
.char-title img { width: 64px; height: 64px; border-radius: 8px; border: 2px solid #3d3020; }
.char-title h2 { text-transform: capitalize; font-weight: 400; }
.char-title small { color: #9b8a6e; display: block; }
.paperdoll { display: grid; grid-template-columns: 1fr 1fr; gap: 10px 24px; }
.col { display: flex; flex-direction: column; gap: 8px; }
.bottom { grid-column: 1 / -1; display: flex; justify-content: center; gap: 24px; margin-top: 6px; }
.bottom .slot { flex: 1; max-width: 340px; }
.slot { display: flex; gap: 10px; align-items: center; padding: 6px 10px; border-radius: 6px; background: rgba(0,0,0,.3);
  border: 1px solid #2a2418; min-height: 54px; }
.slot.empty { opacity: .45; }
.icon { width: 40px; height: 40px; border-radius: 4px; border: 2px solid #666; flex-shrink: 0; background: #111 center/cover no-repeat; position: relative; }
.q-poor { border-color:#9d9d9d } .q-common { border-color:#fff } .q-uncommon { border-color:#1eff00 }
.q-rare { border-color:#0070dd } .q-epic { border-color:#a335ee } .q-legendary { border-color:#ff8000 }
.q-artifact { border-color:#e6cc80 } .q-heirloom { border-color:#00ccff }
.t-poor { color:#9d9d9d } .t-common { color:#fff } .t-uncommon { color:#1eff00 } .t-rare { color:#4aa0ff }
.t-epic { color:#c070ff } .t-legendary { color:#ff9020 } .t-artifact { color:#e6cc80 } .t-heirloom { color:#00ccff }
.ilvl { position: absolute; bottom: -2px; right: -2px; background: rgba(0,0,0,.85); font-size: .65em; padding: 0 3px; border-radius: 3px; color: #ffe680; }
.item-info { min-width: 0; font-size: .82em; }
.item-name { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.slot-label { color: #9b8a6e; font-size: .85em; }
.enchant { color: #1eff00; font-size: .85em; }
.missing { color: #ff7070; font-size: .85em; }
.gems { font-size: .8em; color: #80d0ff; }
.gems .empty { color: #ff7070; }
.stat-row { margin-bottom: 14px; }
.stat-row .lbl { display: flex; justify-content: space-between; font-size: .85em; margin-bottom: 4px; }
.stat-row .track { height: 8px; background: #1a1610; border-radius: 4px; overflow: hidden; }
.stat-row .fill { height: 100%; border-radius: 4px; }
.warn-list { list-style: none; }
.warn-list li { color: #ffaaaa; font-size: .88em; padding: 4px 0; }
.ok { color: #80ffcc; font-size: .9em; }
`
	return BuildPageHead(lang, extraCSS, title) +
		BuildPageNav(lang, currentPath) + `
<div class="page-wrap">
  <div class="page-header">
    <h1>🛡️ ` + template.HTMLEscapeString(T(lang, "equip.title")) + `</h1>
    <p>` + template.HTMLEscapeString(T(lang, "equip.subtitle")) + `</p>
  </div>
{{if .Loading}}
  <div class="card notice-box"><h2>` + template.HTMLEscapeString(T(lang, "equip.loading_title")) + `</h2>
  <p>` + template.HTMLEscapeString(T(lang, "equip.loading_desc")) + `</p></div>
  <script>setTimeout(function(){ location.reload(); }, 3000);</script>
{{else if not .Chars}}
  <div class="card notice-box"><h2>` + template.HTMLEscapeString(T(lang, "equip.no_chars_title")) + `</h2><p>` +
		fmt.Sprintf(T(lang, "equip.no_chars_desc"), `<a href="?refresh=1" style="color:#80c0ff">`+template.HTMLEscapeString(T(lang, "notice.try_refreshing"))+`</a>`) + `</p></div>
{{else}}
  {{$sel := .Selected}}
  <div class="chips">
  {{range .Chars}}
    <a class="chip {{if and $sel (eq .Name $sel.Name) (eq .Realm $sel.Realm)}}active{{end}}" href="/equipment?c={{key . | urlquery}}">{{.Name}}{{if .Warnings}}<span class="badge">{{len .Warnings}}</span>{{end}}</a>
  {{end}}
  </div>

  {{with .Selected}}
  <div class="layout">
    <div class="card">
      <div class="char-title">
        {{if .Thumb}}<img src="{{.Thumb}}" alt="">{{end}}
        <div><h2>{{.Name}}</h2><small>` + fmt.Sprintf(T(lang, "equip.char_meta"), "{{.Realm}}", "{{.Level}}", "{{.Ilvl}}") + `</small></div>
      </div>
      {{if .Err}}<p class="missing">` + template.HTMLEscapeString(T(lang, "equip.error_prefix")) + `{{.Err}}</p>{{else}}
      <div class="paperdoll">
        <div class="col">{{range $.Left}}{{template "slot" .}}{{end}}</div>
        <div class="col">{{range $.Right}}{{template "slot" .}}{{end}}</div>
        <div class="bottom">{{range $.Bottom}}{{template "slot" .}}{{end}}</div>
      </div>
      {{end}}
    </div>

    <div>
      <div class="card">
        <h3 class="section-title" style="margin-top:0">` + template.HTMLEscapeString(T(lang, "equip.sec_stats_title")) + `</h3>
        <div class="stat-row"><div class="lbl"><span>` + template.HTMLEscapeString(T(lang, "equip.stat_crit")) + `</span><span>{{pct .Stats.Crit}}% <span class="muted">({{.Stats.CritRating}})</span></span></div><div class="track"><div class="fill" style="width:{{bar .Stats.Crit}}%;background:#e05050"></div></div></div>
        <div class="stat-row"><div class="lbl"><span>` + template.HTMLEscapeString(T(lang, "equip.stat_haste")) + `</span><span>{{pct .Stats.Haste}}% <span class="muted">({{.Stats.HasteRating}})</span></span></div><div class="track"><div class="fill" style="width:{{bar .Stats.Haste}}%;background:#e0b030"></div></div></div>
        <div class="stat-row"><div class="lbl"><span>` + template.HTMLEscapeString(T(lang, "equip.stat_mastery")) + `</span><span>{{pct .Stats.Mastery}}% <span class="muted">({{.Stats.MasteryRating}})</span></span></div><div class="track"><div class="fill" style="width:{{bar .Stats.Mastery}}%;background:#a060e0"></div></div></div>
        <div class="stat-row"><div class="lbl"><span>` + template.HTMLEscapeString(T(lang, "equip.stat_vers")) + `</span><span>{{pct .Stats.Vers}}% <span class="muted">({{.Stats.VersRating}})</span></span></div><div class="track"><div class="fill" style="width:{{bar .Stats.Vers}}%;background:#30c080"></div></div></div>
        <div class="muted" style="font-size:.85em;margin-top:6px">
          {{if .Stats.PrimaryName}}{{statName .Stats.PrimaryName}}: {{.Stats.Primary}} · {{end}}` + template.HTMLEscapeString(T(lang, "equip.stat_stamina")) + `: {{.Stats.Stamina}} · ` + template.HTMLEscapeString(T(lang, "equip.stat_armor")) + `: {{.Stats.Armor}}
        </div>
      </div>
      <div class="card">
        <h3 class="section-title" style="margin-top:0">` + template.HTMLEscapeString(T(lang, "equip.gear_check_title")) + `</h3>
        {{if .Warnings}}<ul class="warn-list">{{range .Warnings}}<li>⚠️ {{formatWarn .}}</li>{{end}}</ul>
        {{else}}<p class="ok">` + template.HTMLEscapeString(T(lang, "equip.gear_check_ok")) + `</p>{{end}}
        <p class="muted" style="font-size:.75em;margin-top:10px">` + template.HTMLEscapeString(T(lang, "equip.gear_rules_note")) + `</p>
      </div>
    </div>
  </div>
  {{end}}
  <p class="muted" style="font-size:.8em">` + fmt.Sprintf(T(lang, "notice.data_refreshed_at"), "{{.Updated}}") + `<a href="?refresh=1" style="color:#80c0ff">` + template.HTMLEscapeString(T(lang, "notice.force_refresh")) + `</a></p>
{{end}}
</div>
</body></html>
{{define "slot"}}
  <div class="slot {{if not .Has}}empty{{end}}">
    {{if .Has}}
      <div class="icon q-{{.Item.Quality}}" {{if .Item.Icon}}style="background-image:url('{{.Item.Icon}}')"{{end}}><span class="ilvl">{{.Item.Ilvl}}</span></div>
      <div class="item-info">
        <div class="item-name t-{{.Item.Quality}}" title="{{.Item.Name}}">{{.Item.Name}}</div>
        {{if .Item.Enchant}}<div class="enchant" title="{{.Item.Enchant}}">✦ {{.Item.Enchant}}</div>{{end}}
        {{if .Item.MissingEnchant}}<div class="missing">` + template.HTMLEscapeString(T(lang, "equip.missing_enchant")) + `</div>{{end}}
        {{if .Item.Sockets}}<div class="gems">{{range .Item.Sockets}}{{if .Empty}}<span class="empty">` + template.HTMLEscapeString(T(lang, "equip.gem_empty")) + ` </span>{{else}}◆ {{.Gem}} {{end}}{{end}}</div>{{end}}
      </div>
    {{else}}
      <div class="icon"></div><div class="item-info slot-label">` + fmt.Sprintf(T(lang, "equip.slot_empty"), "{{.Label}}") + `</div>
    {{end}}
  </div>
{{end}}`
}

var equipmentTemplate = BuildEquipmentTemplate(LangEn, "/equipment")
