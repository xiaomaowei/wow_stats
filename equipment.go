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

type ItemSetEffectView struct {
	Display  string
	Required int
	Active   bool
}

type ItemSetView struct {
	Name    string
	Effects []ItemSetEffectView
}

type EquippedItem struct {
	ID             int64
	Name           string
	Ilvl           int
	Quality        string // lowercase quality type: epic, rare, ...
	Icon           string
	SlotName       string // localized slot name or raw slot
	InventoryType  string // e.g. "One-Hand", "Head", etc.
	ItemSubclass   string // e.g. "Cloth", "Sword", etc.
	Binding        string // e.g. "Binds when picked up"
	UniqueEquipped string // e.g. "Unique-Equipped"
	ArmorDisplay   string // e.g. "120 Armor"
	WeaponDamage   string // e.g. "150 - 280 Damage"
	WeaponSpeed    string // e.g. "Speed 2.60"
	WeaponDPS      string // e.g. "(81.3 damage per second)"
	Stats          []string // e.g. ["+81 Intellect", "+25 Stamina"]
	Spells         []string // e.g. ["Equip: Increases critical strike by 25."]
	Requirements   []string // e.g. ["Requires Level 70", "Classes: Druid"]
	Enchant        string
	MissingEnchant bool
	Sockets        []SocketView
	SetName        string
	ItemSet        *ItemSetView
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
	SlotType string
	Label    string
	Item     EquippedItem
	Has      bool
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
	url := fmt.Sprintf("https://%s.api.blizzard.com/data/wow/media/item/%d?namespace=%s&locale=%s",
		config.Region, id, config.StaticNamespace(), config.Locale)
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

type blizzardEquippedItemsPayload struct {
	Items []struct {
		Slot struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"slot"`
		Item struct {
			ID int64 `json:"id"`
		} `json:"item"`
		Quality struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"quality"`
		Name  string `json:"name"`
		Level struct {
			Value         int    `json:"value"`
			DisplayString string `json:"display_string"`
		} `json:"level"`
		InventoryType struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"inventory_type"`
		ItemSubclass struct {
			Name string `json:"name"`
		} `json:"item_subclass"`
		Binding struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"binding"`
		UniqueEquipped string `json:"unique_equipped"`
		Armor          struct {
			Value   int `json:"value"`
			Display struct {
				DisplayString string `json:"display_string"`
			} `json:"display"`
			DisplayString string `json:"display_string"`
		} `json:"armor"`
		Weapon struct {
			Damage struct {
				MinValue      int    `json:"min_value"`
				MaxValue      int    `json:"max_value"`
				DisplayString string `json:"display_string"`
			} `json:"damage"`
			AttackSpeed struct {
				Value         int    `json:"value"`
				DisplayString string `json:"display_string"`
			} `json:"attack_speed"`
			Dps struct {
				Value         float64 `json:"value"`
				DisplayString string  `json:"display_string"`
			} `json:"dps"`
		} `json:"weapon"`
		Stats []struct {
			Type struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"type"`
			Value   int `json:"value"`
			Display struct {
				DisplayString string `json:"display_string"`
			} `json:"display"`
			DisplayString string `json:"display_string"`
		} `json:"stats"`
		Requirements struct {
			Level struct {
				Value         int    `json:"value"`
				DisplayString string `json:"display_string"`
			} `json:"level"`
			PlayableClasses struct {
				DisplayString string `json:"display_string"`
				Links         []struct {
					Name string `json:"name"`
				} `json:"links"`
			} `json:"playable_classes"`
			Skill struct {
				Profession struct {
					Name string `json:"name"`
				} `json:"profession"`
				Level         int    `json:"level"`
				DisplayString string `json:"display_string"`
			} `json:"skill"`
			Reputation struct {
				Faction struct {
					Name string `json:"name"`
				} `json:"faction"`
				DisplayString string `json:"display_string"`
			} `json:"reputation"`
		} `json:"requirements"`
		Spells []struct {
			Spell struct {
				Name string `json:"name"`
			} `json:"spell"`
			Description   string `json:"description"`
			DisplayString string `json:"display_string"`
		} `json:"spells"`
		Enchantments []struct {
			Display string `json:"display_string"`
		} `json:"enchantments"`
		Sockets []struct {
			Item struct {
				Name string `json:"name"`
			} `json:"item"`
			SocketType struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"socket_type"`
			Display string `json:"display_string"`
		} `json:"sockets"`
		Set struct {
			Name string `json:"display_string"`
			Set  struct {
				Name string `json:"name"`
			} `json:"item_set"`
			Effects []struct {
				DisplayString string `json:"display_string"`
				RequiredCount int    `json:"required_count"`
				IsActive      bool   `json:"is_active"`
			} `json:"effects"`
		} `json:"set"`
	} `json:"equipped_items"`
}

func parseEquippedItem(it *blizzardEquippedItemsPayload, rules gearRules, needEnchant map[string]bool, iconResolver func(int64) string) (map[string]EquippedItem, []string) {
	items := make(map[string]EquippedItem, len(it.Items))
	var warnings []string

	for _, itemData := range it.Items {
		slotKey := itemData.Slot.Type
		if slotKey == "" {
			continue
		}

		item := EquippedItem{
			ID:             itemData.Item.ID,
			Name:           itemData.Name,
			Ilvl:           itemData.Level.Value,
			Quality:        strings.ToLower(itemData.Quality.Type),
			SlotName:       itemData.Slot.Name,
			InventoryType:  itemData.InventoryType.Name,
			ItemSubclass:   itemData.ItemSubclass.Name,
			Binding:        itemData.Binding.Name,
			UniqueEquipped: itemData.UniqueEquipped,
			SetName:        itemData.Set.Set.Name,
		}
		if item.SetName == "" && itemData.Set.Name != "" {
			item.SetName = itemData.Set.Name
		}

		if iconResolver != nil && item.ID > 0 {
			item.Icon = iconResolver(item.ID)
		}

		// Armor display string
		if itemData.Armor.Display.DisplayString != "" {
			item.ArmorDisplay = itemData.Armor.Display.DisplayString
		} else if itemData.Armor.DisplayString != "" {
			item.ArmorDisplay = itemData.Armor.DisplayString
		}

		// Weapon stats
		if itemData.Weapon.Damage.DisplayString != "" {
			item.WeaponDamage = itemData.Weapon.Damage.DisplayString
		}
		if itemData.Weapon.AttackSpeed.DisplayString != "" {
			item.WeaponSpeed = itemData.Weapon.AttackSpeed.DisplayString
		}
		if itemData.Weapon.Dps.DisplayString != "" {
			item.WeaponDPS = itemData.Weapon.Dps.DisplayString
		}

		// Item stats display strings
		for _, s := range itemData.Stats {
			ds := s.Display.DisplayString
			if ds == "" {
				ds = s.DisplayString
			}
			if ds != "" {
				item.Stats = append(item.Stats, ds)
			}
		}

		// Spells / equip effects
		for _, sp := range itemData.Spells {
			desc := sp.Description
			if desc == "" {
				desc = sp.DisplayString
			}
			if desc != "" {
				item.Spells = append(item.Spells, desc)
			}
		}

		// Requirements
		if itemData.Requirements.Level.DisplayString != "" {
			item.Requirements = append(item.Requirements, itemData.Requirements.Level.DisplayString)
		}
		if itemData.Requirements.PlayableClasses.DisplayString != "" {
			item.Requirements = append(item.Requirements, itemData.Requirements.PlayableClasses.DisplayString)
		} else if len(itemData.Requirements.PlayableClasses.Links) > 0 {
			var classNames []string
			for _, l := range itemData.Requirements.PlayableClasses.Links {
				if l.Name != "" {
					classNames = append(classNames, l.Name)
				}
			}
			if len(classNames) > 0 {
				item.Requirements = append(item.Requirements, "Classes: "+strings.Join(classNames, ", "))
			}
		}
		if itemData.Requirements.Skill.DisplayString != "" {
			item.Requirements = append(item.Requirements, itemData.Requirements.Skill.DisplayString)
		}
		if itemData.Requirements.Reputation.DisplayString != "" {
			item.Requirements = append(item.Requirements, itemData.Requirements.Reputation.DisplayString)
		}

		// Enchantments
		for _, e := range itemData.Enchantments {
			if e.Display != "" {
				if item.Enchant != "" {
					item.Enchant += "; "
				}
				item.Enchant += e.Display
			}
		}
		if needEnchant != nil && needEnchant[slotKey] && item.Enchant == "" {
			item.MissingEnchant = true
			warnings = append(warnings, fmt.Sprintf("%s: missing enchant", slotLabel(slotKey)))
		}

		// Sockets
		for _, s := range itemData.Sockets {
			sv := SocketView{Gem: s.Item.Name, Empty: s.Item.Name == ""}
			if sv.Empty {
				warnings = append(warnings, fmt.Sprintf("%s: empty gem socket", slotLabel(slotKey)))
			}
			item.Sockets = append(item.Sockets, sv)
		}

		// Set bonuses
		if len(itemData.Set.Effects) > 0 || item.SetName != "" {
			setV := &ItemSetView{Name: item.SetName}
			for _, eff := range itemData.Set.Effects {
				if eff.DisplayString != "" {
					setV.Effects = append(setV.Effects, ItemSetEffectView{
						Display:  eff.DisplayString,
						Required: eff.RequiredCount,
						Active:   eff.IsActive,
					})
				}
			}
			item.ItemSet = setV
		}

		items[slotKey] = item
	}

	return items, warnings
}

func fetchEquipment(client *http.Client, c CharacterStats, rules gearRules) EQChar {
	out := EQChar{Name: c.Name, Realm: c.Realm, Level: c.Level, Ilvl: c.ItemLevel, Thumb: c.ThumbnailURL,
		Items: map[string]EquippedItem{}}

	var eq blizzardEquippedItemsPayload
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

	items, warnings := parseEquippedItem(&eq, rules, needEnchant, func(id int64) string {
		return itemIcon(client, id)
	})
	out.Items = items
	out.Warnings = warnings

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
	chars := eligibleCharacters(10)

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
		out = append(out, SlotView{SlotType: t, Label: lbl, Item: it, Has: ok})
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

/* Rich Equipment Tooltip */
.slot { position: relative; cursor: default; }
.slot[tabindex="0"]:focus { outline: 1px solid #d050a0; }
.item-tooltip {
  display: none;
  position: absolute;
  top: 100%;
  left: 0;
  margin-top: 6px;
  z-index: 100;
  width: 280px;
  max-width: calc(100vw - 24px);
  max-height: calc(100vh - 32px);
  overflow-y: auto;
  box-sizing: border-box;
  background: #0b0d12;
  border: 1px solid #4a4030;
  border-radius: 6px;
  padding: 10px 12px;
  box-shadow: 0 8px 24px rgba(0,0,0,.85), 0 0 0 1px rgba(255,255,255,.05);
  pointer-events: auto;
  text-align: left;
  line-height: 1.35;
  font-size: .84em;
  color: #fff;
}
.right-col .item-tooltip {
  left: auto;
  right: 0;
}
.bottom-slots .item-tooltip {
  top: auto;
  bottom: 100%;
  margin-top: 0;
  margin-bottom: 6px;
}
.slot:hover .item-tooltip,
.slot:focus .item-tooltip,
.slot:focus-within .item-tooltip,
.slot.tooltip-open .item-tooltip {
  display: block;
}
.tt-title { font-size: 1.05em; font-weight: 600; margin-bottom: 4px; word-break: break-word; }
.tt-row { display: flex; justify-content: space-between; gap: 8px; font-size: .92em; }
.tt-muted { color: #ffd100; }
.tt-sub { color: #fff; }
.tt-binding { color: #fff; font-size: .92em; margin-top: 2px; }
.tt-unique { color: #fff; font-size: .92em; }
.tt-armor { color: #fff; font-size: .95em; margin-top: 3px; }
.tt-weapon-dmg { color: #fff; font-size: .95em; }
.tt-weapon-dps { color: #fff; font-size: .92em; }
.tt-stats { margin-top: 4px; }
.tt-stat { color: #fff; font-size: .92em; }
.tt-enchant { color: #1eff00; font-size: .92em; margin-top: 4px; }
.tt-sockets { margin-top: 4px; }
.tt-socket-item { font-size: .92em; display: flex; align-items: center; gap: 4px; margin-top: 1px; }
.tt-socket-item.empty { color: #9d9d9d; }
.tt-socket-item:not(.empty) { color: #80d0ff; }
.tt-set { margin-top: 6px; padding-top: 4px; border-top: 1px solid #2a2418; }
.tt-set-title { color: #ffd100; font-size: .95em; font-weight: 600; margin-bottom: 2px; }
.tt-set-effect { font-size: .88em; margin-top: 2px; }
.tt-set-effect.active { color: #1eff00; }
.tt-set-effect.inactive { color: #7f7f7f; }
.tt-spells { margin-top: 4px; }
.tt-spell { color: #1eff00; font-size: .9em; margin-top: 2px; }
.tt-reqs { margin-top: 5px; color: #ff6060; font-size: .9em; }
@media (max-width: 600px) {
  .right-col .item-tooltip { left: 0; right: auto; }
}
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
        <div class="col left-col">{{range $.Left}}{{template "slot" .}}{{end}}</div>
        <div class="col right-col">{{range $.Right}}{{template "slot" .}}{{end}}</div>
        <div class="bottom bottom-slots">{{range $.Bottom}}{{template "slot" .}}{{end}}</div>
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
<script>
(function() {
  function closeAllTooltips() {
    document.querySelectorAll('.slot.tooltip-open').forEach(function(s) {
      s.classList.remove('tooltip-open');
    });
  }

  function adjustPosition(slot) {
    var tt = slot.querySelector('.item-tooltip');
    if (!tt) return;
    // Reset inline styling first to measure natural dimensions
    tt.style.left = '';
    tt.style.right = '';
    tt.style.top = '';
    tt.style.bottom = '';
    tt.style.transform = '';
    tt.style.maxHeight = '';

    var slotRect = slot.getBoundingClientRect();
    var ttRect = tt.getBoundingClientRect();
    var vpW = window.innerWidth || document.documentElement.clientWidth;
    var vpH = window.innerHeight || document.documentElement.clientHeight;
    var pad = 8;

    // Horizontal positioning check
    // Default left is 0 (relative to slot) or right 0 for right-col
    var isRightCol = !!slot.closest('.right-col');
    if (isRightCol) {
      // Check if overflowing left
      if (slotRect.right - ttRect.width < pad) {
        tt.style.left = (-slotRect.left + pad) + 'px';
        tt.style.right = 'auto';
      }
    } else {
      // Check if overflowing right
      if (slotRect.left + ttRect.width > vpW - pad) {
        var shift = (slotRect.left + ttRect.width) - (vpW - pad);
        tt.style.left = Math.max(-slotRect.left + pad, -shift) + 'px';
      }
    }

    // Vertical positioning check
    var spaceBelow = vpH - slotRect.bottom - pad;
    var spaceAbove = slotRect.top - pad;
    var isBottomSlot = !!slot.closest('.bottom-slots');

    if (isBottomSlot) {
      if (spaceAbove < ttRect.height && spaceBelow > spaceAbove) {
        tt.style.top = '100%';
        tt.style.bottom = 'auto';
        tt.style.marginTop = '6px';
        tt.style.marginBottom = '0';
        tt.style.maxHeight = Math.max(120, spaceBelow - 6) + 'px';
      } else {
        tt.style.maxHeight = Math.max(120, spaceAbove - 6) + 'px';
      }
    } else {
      if (spaceBelow < ttRect.height && spaceAbove > spaceBelow) {
        tt.style.top = 'auto';
        tt.style.bottom = '100%';
        tt.style.marginTop = '0';
        tt.style.marginBottom = '6px';
        tt.style.maxHeight = Math.max(120, spaceAbove - 6) + 'px';
      } else {
        tt.style.maxHeight = Math.max(120, spaceBelow - 6) + 'px';
      }
    }
  }

  function updateActivePositions() {
    var activeSlots = document.querySelectorAll('.slot.tooltip-open, .slot:hover, .slot:focus-within');
    activeSlots.forEach(function(s) {
      if (s.querySelector('.item-tooltip')) {
        adjustPosition(s);
      }
    });
  }

  document.querySelectorAll('.slot[tabindex="0"]').forEach(function(slot) {
    slot.addEventListener('mouseenter', function() { adjustPosition(slot); });
    slot.addEventListener('focus', function() { adjustPosition(slot); });
  });

  document.addEventListener('click', function(e) {
    var clickedSlot = e.target.closest('.slot[tabindex="0"]');
    var insideTooltip = e.target.closest('.item-tooltip');
    if (insideTooltip) return;

    document.querySelectorAll('.slot.tooltip-open').forEach(function(s) {
      if (s !== clickedSlot) s.classList.remove('tooltip-open');
    });

    if (clickedSlot) {
      var wasOpen = clickedSlot.classList.contains('tooltip-open');
      if (!wasOpen) {
        clickedSlot.classList.add('tooltip-open');
        adjustPosition(clickedSlot);
      } else {
        clickedSlot.classList.remove('tooltip-open');
      }
    }
  });

  document.addEventListener('keydown', function(e) {
    if (e.key === 'Escape') {
      closeAllTooltips();
      if (document.activeElement && document.activeElement.classList.contains('slot')) {
        document.activeElement.blur();
      }
      return;
    }
    var slot = e.target.closest('.slot[tabindex="0"]');
    if (!slot) return;
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      var wasOpen = slot.classList.contains('tooltip-open');
      closeAllTooltips();
      if (!wasOpen) {
        slot.classList.add('tooltip-open');
        adjustPosition(slot);
      }
    }
  });

  window.addEventListener('resize', updateActivePositions, { passive: true });
  window.addEventListener('scroll', updateActivePositions, { passive: true });
})();
</script>
</body></html>
{{define "slot"}}
  <div class="slot {{if not .Has}}empty{{end}}" {{if .Has}}tabindex="0" aria-describedby="tooltip-{{lower .SlotType}}" aria-label="{{.Item.Name}}"{{end}}>
    {{if .Has}}
      <div class="icon q-{{.Item.Quality}}" {{if .Item.Icon}}style="background-image:url('{{.Item.Icon}}')"{{end}}><span class="ilvl">{{.Item.Ilvl}}</span></div>
      <div class="item-info">
        <div class="item-name t-{{.Item.Quality}}">{{.Item.Name}}</div>
        {{if .Item.Enchant}}<div class="enchant">✦ {{.Item.Enchant}}</div>{{end}}
        {{if .Item.MissingEnchant}}<div class="missing">` + template.HTMLEscapeString(T(lang, "equip.missing_enchant")) + `</div>{{end}}
        {{if .Item.Sockets}}<div class="gems">{{range .Item.Sockets}}{{if .Empty}}<span class="empty">` + template.HTMLEscapeString(T(lang, "equip.gem_empty")) + ` </span>{{else}}◆ {{.Gem}} {{end}}{{end}}</div>{{end}}
      </div>
      <div class="item-tooltip" id="tooltip-{{lower .SlotType}}" role="tooltip">
        <div class="tt-title t-{{.Item.Quality}}">{{.Item.Name}}</div>
        {{if .Item.Ilvl}}<div class="tt-row tt-muted"><span>` + fmt.Sprintf(T(lang, "equip.tt_item_level"), "{{.Item.Ilvl}}") + `</span></div>{{end}}
        {{if or .Item.Binding .Item.InventoryType .Item.ItemSubclass}}
          <div class="tt-row tt-sub">
            <span>{{if .Item.InventoryType}}{{.Item.InventoryType}}{{else}}{{.Label}}{{end}}</span>
            {{if .Item.ItemSubclass}}<span>{{.Item.ItemSubclass}}</span>{{end}}
          </div>
        {{end}}
        {{if .Item.Binding}}<div class="tt-binding">{{.Item.Binding}}</div>{{end}}
        {{if .Item.UniqueEquipped}}<div class="tt-unique">{{.Item.UniqueEquipped}}</div>{{end}}
        {{if .Item.WeaponDamage}}
          <div class="tt-row tt-weapon-dmg">
            <span>{{.Item.WeaponDamage}}</span>
            {{if .Item.WeaponSpeed}}<span>{{.Item.WeaponSpeed}}</span>{{end}}
          </div>
        {{end}}
        {{if .Item.WeaponDPS}}<div class="tt-weapon-dps">{{.Item.WeaponDPS}}</div>{{end}}
        {{if .Item.ArmorDisplay}}<div class="tt-armor">{{.Item.ArmorDisplay}}</div>{{end}}
        {{if .Item.Stats}}
          <div class="tt-stats">
            {{range .Item.Stats}}<div class="tt-stat">{{.}}</div>{{end}}
          </div>
        {{end}}
        {{if .Item.Enchant}}<div class="tt-enchant">✦ {{.Item.Enchant}}</div>{{end}}
        {{if .Item.Sockets}}
          <div class="tt-sockets">
            {{range .Item.Sockets}}
              <div class="tt-socket-item {{if .Empty}}empty{{end}}">
                {{if .Empty}}◇ ` + template.HTMLEscapeString(T(lang, "equip.tt_empty_socket")) + `{{else}}◆ {{.Gem}}{{end}}
              </div>
            {{end}}
          </div>
        {{end}}
        {{if .Item.ItemSet}}
          <div class="tt-set">
            <div class="tt-set-title">{{.Item.ItemSet.Name}}</div>
            {{range .Item.ItemSet.Effects}}
              <div class="tt-set-effect {{if .Active}}active{{else}}inactive{{end}}">{{.Display}}</div>
            {{end}}
          </div>
        {{end}}
        {{if .Item.Spells}}
          <div class="tt-spells">
            {{range .Item.Spells}}<div class="tt-spell">{{.}}</div>{{end}}
          </div>
        {{end}}
        {{if .Item.Requirements}}
          <div class="tt-reqs">
            {{range .Item.Requirements}}<div>{{.}}</div>{{end}}
          </div>
        {{end}}
      </div>
    {{else}}
      <div class="icon"></div><div class="item-info slot-label">` + fmt.Sprintf(T(lang, "equip.slot_empty"), "{{.Label}}") + `</div>
    {{end}}
  </div>
{{end}}`
}

var equipmentTemplate = BuildEquipmentTemplate(LangEn, "/equipment")
