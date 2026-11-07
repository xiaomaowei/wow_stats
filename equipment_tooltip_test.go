package main

import (
	"bytes"
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestParseEquippedItem_FullPayload(t *testing.T) {
	rawJSON := `{
		"equipped_items": [
			{
				"item": { "id": 19019 },
				"slot": { "type": "MAIN_HAND", "name": "Main Hand" },
				"quality": { "type": "LEGENDARY", "name": "Legendary" },
				"name": "Thunderfury, Blessed Blade of the Windseeker",
				"level": { "value": 80, "display_string": "Item Level 80" },
				"inventory_type": { "type": "ONE_HAND", "name": "One-Hand" },
				"item_subclass": { "name": "Sword" },
				"binding": { "type": "ON_ACQUIRE", "name": "Binds when picked up" },
				"unique_equipped": "Unique-Equipped",
				"armor": {
					"display": { "display_string": "15 Armor" }
				},
				"weapon": {
					"damage": { "min_value": 44, "max_value": 115, "display_string": "44 - 115 Damage" },
					"attack_speed": { "value": 1900, "display_string": "Speed 1.90" },
					"dps": { "value": 41.84, "display_string": "(41.8 damage per second)" }
				},
				"stats": [
					{
						"type": { "type": "AGILITY", "name": "Agility" },
						"value": 5,
						"display": { "display_string": "+5 Agility" }
					},
					{
						"type": { "type": "STAMINA", "name": "Stamina" },
						"value": 8,
						"display": { "display_string": "+8 Stamina" }
					}
				],
				"requirements": {
					"level": { "value": 60, "display_string": "Requires Level 60" },
					"playable_classes": { "display_string": "Classes: Warrior, Paladin, Rogue, Hunter, Death Knight" }
				},
				"spells": [
					{
						"description": "Chance on hit: Blasts your target with a cyclone, delivering 300 Nature damage."
					}
				],
				"enchantments": [
					{ "display_string": "Crusader" }
				],
				"sockets": [
					{
						"item": { "name": "Bold Crimson Spinel" },
						"socket_type": { "name": "Red Socket" },
						"display_string": "Red Socket"
					}
				],
				"set": {
					"display_string": "Windseeker Regalia",
					"item_set": { "name": "Windseeker Regalia" },
					"effects": [
						{
							"display_string": "(2) Set: Increases attack power by 50.",
							"required_count": 2,
							"is_active": true
						}
					]
				}
			}
		]
	}`

	var payload blizzardEquippedItemsPayload
	if err := json.Unmarshal([]byte(rawJSON), &payload); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	rules := gearRules{MinLevel: 60, EnchantSlots: []string{"MAIN_HAND"}}
	needEnchant := map[string]bool{"MAIN_HAND": true}
	iconMap := map[int64]string{19019: "https://example.com/icon.jpg"}

	items, warnings := parseEquippedItem(&payload, rules, needEnchant, func(id int64) string {
		return iconMap[id]
	})

	if len(warnings) != 0 {
		t.Errorf("expected 0 warnings, got %v", warnings)
	}

	it, ok := items["MAIN_HAND"]
	if !ok {
		t.Fatalf("expected MAIN_HAND item to be present")
	}

	if it.ID != 19019 {
		t.Errorf("expected ID 19019, got %d", it.ID)
	}
	if it.Name != "Thunderfury, Blessed Blade of the Windseeker" {
		t.Errorf("unexpected Name: %s", it.Name)
	}
	if it.Quality != "legendary" {
		t.Errorf("expected quality 'legendary', got '%s'", it.Quality)
	}
	if it.Ilvl != 80 {
		t.Errorf("expected Ilvl 80, got %d", it.Ilvl)
	}
	if it.SlotName != "Main Hand" {
		t.Errorf("expected SlotName 'Main Hand', got '%s'", it.SlotName)
	}
	if it.InventoryType != "One-Hand" {
		t.Errorf("expected InventoryType 'One-Hand', got '%s'", it.InventoryType)
	}
	if it.ItemSubclass != "Sword" {
		t.Errorf("expected ItemSubclass 'Sword', got '%s'", it.ItemSubclass)
	}
	if it.Binding != "Binds when picked up" {
		t.Errorf("expected Binding 'Binds when picked up', got '%s'", it.Binding)
	}
	if it.UniqueEquipped != "Unique-Equipped" {
		t.Errorf("expected UniqueEquipped 'Unique-Equipped', got '%s'", it.UniqueEquipped)
	}
	if it.ArmorDisplay != "15 Armor" {
		t.Errorf("expected ArmorDisplay '15 Armor', got '%s'", it.ArmorDisplay)
	}
	if it.WeaponDamage != "44 - 115 Damage" {
		t.Errorf("expected WeaponDamage '44 - 115 Damage', got '%s'", it.WeaponDamage)
	}
	if it.WeaponSpeed != "Speed 1.90" {
		t.Errorf("expected WeaponSpeed 'Speed 1.90', got '%s'", it.WeaponSpeed)
	}
	if it.WeaponDPS != "(41.8 damage per second)" {
		t.Errorf("expected WeaponDPS '(41.8 damage per second)', got '%s'", it.WeaponDPS)
	}
	if len(it.Stats) != 2 || it.Stats[0] != "+5 Agility" || it.Stats[1] != "+8 Stamina" {
		t.Errorf("unexpected Stats: %v", it.Stats)
	}
	if len(it.Requirements) != 2 || it.Requirements[0] != "Requires Level 60" {
		t.Errorf("unexpected Requirements: %v", it.Requirements)
	}
	if len(it.Spells) != 1 || !strings.Contains(it.Spells[0], "Chance on hit") {
		t.Errorf("unexpected Spells: %v", it.Spells)
	}
	if it.Enchant != "Crusader" {
		t.Errorf("expected Enchant 'Crusader', got '%s'", it.Enchant)
	}
	if it.MissingEnchant {
		t.Errorf("expected MissingEnchant to be false")
	}
	if len(it.Sockets) != 1 || it.Sockets[0].Gem != "Bold Crimson Spinel" || it.Sockets[0].Empty {
		t.Errorf("unexpected Sockets: %v", it.Sockets)
	}
	if it.ItemSet == nil || it.ItemSet.Name != "Windseeker Regalia" || len(it.ItemSet.Effects) != 1 {
		t.Errorf("unexpected ItemSet: %+v", it.ItemSet)
	} else if !it.ItemSet.Effects[0].Active || it.ItemSet.Effects[0].Required != 2 {
		t.Errorf("unexpected ItemSet Effect: %+v", it.ItemSet.Effects[0])
	}
}

func TestParseEquippedItem_IncompleteFields(t *testing.T) {
	// Item with minimal fields (no weapon, no armor, no stats, no enchants, empty socket, missing enchant warning)
	rawJSON := `{
		"equipped_items": [
			{
				"item": { "id": 12345 },
				"slot": { "type": "HEAD" },
				"quality": { "type": "COMMON" },
				"name": "Cloth Cap",
				"level": { "value": 10 },
				"sockets": [
					{
						"item": { "name": "" }
					}
				]
			}
		]
	}`

	var payload blizzardEquippedItemsPayload
	if err := json.Unmarshal([]byte(rawJSON), &payload); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	rules := gearRules{MinLevel: 80, EnchantSlots: []string{"HEAD"}}
	needEnchant := map[string]bool{"HEAD": true}

	items, warnings := parseEquippedItem(&payload, rules, needEnchant, nil)

	it, ok := items["HEAD"]
	if !ok {
		t.Fatalf("expected HEAD item")
	}

	// Absent values should NOT be invented
	if it.ArmorDisplay != "" {
		t.Errorf("expected empty ArmorDisplay, got '%s'", it.ArmorDisplay)
	}
	if it.WeaponDamage != "" || it.WeaponSpeed != "" || it.WeaponDPS != "" {
		t.Errorf("expected empty weapon stats, got dmg='%s' spd='%s' dps='%s'", it.WeaponDamage, it.WeaponSpeed, it.WeaponDPS)
	}
	if len(it.Stats) != 0 {
		t.Errorf("expected 0 stats, got %v", it.Stats)
	}
	if len(it.Requirements) != 0 {
		t.Errorf("expected 0 requirements, got %v", it.Requirements)
	}
	if len(it.Spells) != 0 {
		t.Errorf("expected 0 spells, got %v", it.Spells)
	}
	if it.Binding != "" {
		t.Errorf("expected empty Binding, got '%s'", it.Binding)
	}
	if it.UniqueEquipped != "" {
		t.Errorf("expected empty UniqueEquipped, got '%s'", it.UniqueEquipped)
	}
	if it.ItemSet != nil {
		t.Errorf("expected nil ItemSet, got %+v", it.ItemSet)
	}
	if it.Enchant != "" {
		t.Errorf("expected empty Enchant, got '%s'", it.Enchant)
	}
	if !it.MissingEnchant {
		t.Errorf("expected MissingEnchant to be true")
	}
	if len(it.Sockets) != 1 || !it.Sockets[0].Empty {
		t.Errorf("expected 1 empty socket, got %v", it.Sockets)
	}

	// Warnings should contain missing enchant and empty socket
	if len(warnings) != 2 {
		t.Errorf("expected 2 warnings (missing enchant + empty socket), got %v", warnings)
	}
}

func TestEquipmentTemplate_EscapesXSS(t *testing.T) {
	// Construct malicious item input
	maliciousItem := EquippedItem{
		ID:             99999,
		Name:           "<script>alert('xss-name')</script>",
		Ilvl:           700,
		Quality:        "epic",
		SlotName:       "<img src=x onerror=alert('xss-slot')>",
		InventoryType:  "<b>One-Hand</b>",
		ItemSubclass:   "<i>Sword</i>",
		Binding:        "<script>alert('xss-binding')</script>",
		UniqueEquipped: "<script>alert('xss-unique')</script>",
		ArmorDisplay:   "<script>alert('xss-armor')</script>",
		WeaponDamage:   "<script>alert('xss-dmg')</script>",
		WeaponSpeed:    "<script>alert('xss-speed')</script>",
		WeaponDPS:      "<script>alert('xss-dps')</script>",
		Stats:          []string{"<script>alert('xss-stat')</script>"},
		Spells:         []string{"<script>alert('xss-spell')</script>"},
		Requirements:   []string{"<script>alert('xss-req')</script>"},
		Enchant:        "<script>alert('xss-enchant')</script>",
		Sockets: []SocketView{
			{Gem: "<script>alert('xss-gem')</script>", Empty: false},
		},
		ItemSet: &ItemSetView{
			Name: "<script>alert('xss-setname')</script>",
			Effects: []ItemSetEffectView{
				{Display: "<script>alert('xss-seteffect')</script>", Required: 2, Active: true},
			},
		},
	}

	page := EQPageData{
		Loading: false,
		Chars: []EQChar{
			{
				Name:  "TestHero",
				Realm: "TestRealm",
				Level: 80,
				Ilvl:  700,
				Items: map[string]EquippedItem{
					"HEAD": maliciousItem,
				},
			},
		},
	}
	page.Selected = &page.Chars[0]
	page.Left = []SlotView{
		{Label: "Head", Item: maliciousItem, Has: true},
	}

	// Parse and execute template
	tmpl, err := template.New("equipment").Funcs(template.FuncMap{
		"key":        func(c EQChar) string { return c.Name + "-" + c.Realm },
		"lower":      strings.ToLower,
		"pct":        func(v float64) string { return "0.0" },
		"bar":        func(v float64) int { return 0 },
		"t":          func(k string) string { return T(LangEn, k) },
		"tFmt":       func(k string, args ...interface{}) string { return TFmt(LangEn, k, args...) },
		"statName":   func(name string) string { return StatName(LangEn, name) },
		"lang":       func() string { return LangEn },
		"formatWarn": func(w string) string { return w },
	}).Parse(BuildEquipmentTemplate(LangEn, "/equipment"))
	if err != nil {
		t.Fatalf("Template parsing failed: %v", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, page); err != nil {
		t.Fatalf("Template execution failed: %v", err)
	}

	html := buf.String()

	// Verify no unescaped script tag from any payload field
	if strings.Contains(html, "<script>alert(") {
		t.Fatalf("CRITICAL SECURITY FLAW: Unescaped <script> tag detected in HTML output:\n%s", html)
	}
	if strings.Contains(html, "<img src=x onerror=") {
		t.Fatalf("CRITICAL SECURITY FLAW: Unescaped <img> tag detected in HTML output")
	}

	// Verify escaped versions exist
	if !strings.Contains(html, "&lt;script&gt;alert(&#39;xss-name&#39;)&lt;/script&gt;") {
		t.Errorf("Expected HTML-escaped xss-name, got output containing: %s", html)
	}
	if !strings.Contains(html, "&lt;script&gt;alert(&#39;xss-enchant&#39;)&lt;/script&gt;") {
		t.Errorf("Expected HTML-escaped xss-enchant")
	}
	if !strings.Contains(html, "&lt;script&gt;alert(&#39;xss-armor&#39;)&lt;/script&gt;") {
		t.Errorf("Expected HTML-escaped xss-armor")
	}
	if !strings.Contains(html, "&lt;script&gt;alert(&#39;xss-stat&#39;)&lt;/script&gt;") {
		t.Errorf("Expected HTML-escaped xss-stat")
	}
	if !strings.Contains(html, "&lt;script&gt;alert(&#39;xss-setname&#39;)&lt;/script&gt;") {
		t.Errorf("Expected HTML-escaped xss-setname")
	}
}

func TestEquipmentTemplate_Localization(t *testing.T) {
	sampleItem := EquippedItem{
		ID:            20001,
		Name:          "Heroic Helm",
		Ilvl:          630,
		Quality:       "epic",
		InventoryType: "Head",
		ArmorDisplay:  "250 Armor",
		Sockets: []SocketView{
			{Empty: true},
		},
	}

	for _, lang := range []string{LangEn, LangZhTW} {
		t.Run("Lang_"+lang, func(t *testing.T) {
			page := EQPageData{
				Loading: false,
				Chars: []EQChar{
					{
						Name:  "Hero",
						Realm: "Realm",
						Level: 80,
						Ilvl:  630,
						Items: map[string]EquippedItem{"HEAD": sampleItem},
					},
				},
			}
			page.Selected = &page.Chars[0]
			page.Left = []SlotView{
				{SlotType: "HEAD", Label: SlotLabel(lang, "HEAD"), Item: sampleItem, Has: true},
			}

			tmpl, err := template.New("equipment").Funcs(template.FuncMap{
				"key":        func(c EQChar) string { return c.Name + "-" + c.Realm },
				"lower":      strings.ToLower,
				"pct":        func(v float64) string { return "0.0" },
				"bar":        func(v float64) int { return 0 },
				"t":          func(k string) string { return T(lang, k) },
				"tFmt":       func(k string, args ...interface{}) string { return TFmt(lang, k, args...) },
				"statName":   func(name string) string { return StatName(lang, name) },
				"lang":       func() string { return lang },
				"formatWarn": func(w string) string { return w },
			}).Parse(BuildEquipmentTemplate(lang, "/equipment"))
			if err != nil {
				t.Fatalf("Template parse failed for %s: %v", lang, err)
			}

			var buf bytes.Buffer
			if err := tmpl.Execute(&buf, page); err != nil {
				t.Fatalf("Template execute failed for %s: %v", lang, err)
			}

			res := buf.String()
			if lang == LangZhTW {
				if !strings.Contains(res, "物品等級 630") {
					t.Errorf("expected zh-TW '物品等級 630' in output")
				}
				if !strings.Contains(res, "空插槽") {
					t.Errorf("expected zh-TW '空插槽' in output")
				}
			} else {
				if !strings.Contains(res, "Item Level 630") {
					t.Errorf("expected English 'Item Level 630' in output")
				}
				if !strings.Contains(res, "Empty Socket") {
					t.Errorf("expected English 'Empty Socket' in output")
				}
			}

			// Accessible keyboard and ARIA markup checks
			if !strings.Contains(res, `tabindex="0" aria-describedby="tooltip-head" aria-label="Heroic Helm"`) {
				t.Errorf("expected accessible keyboard attributes on slot with aria-describedby")
			}
			if !strings.Contains(res, `class="item-tooltip" id="tooltip-head" role="tooltip"`) {
				t.Errorf("expected tooltip id and role attributes without permanent aria-hidden")
			}
			if strings.Contains(res, `aria-hidden="true"`) {
				t.Errorf("expected no permanent aria-hidden=true on tooltip")
			}
			if strings.Contains(res, `role="button"`) || strings.Contains(res, `aria-haspopup=`) {
				t.Errorf("expected no misleading button/haspopup semantics")
			}
		})
	}
}

func TestEquipmentHandler_WorksOnForever(t *testing.T) {
	origConfig := config
	origOAuth2Config := oauth2Config
	origUserToken := userToken
	defer func() {
		config = origConfig
		oauth2Config = origOAuth2Config
		tokenMutex.Lock()
		userToken = origUserToken
		tokenMutex.Unlock()
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

	tokenMutex.Lock()
	userToken = &oauth2.Token{AccessToken: "mock-forever-token"}
	tokenMutex.Unlock()

	eqState.Lock()
	eqState.data = []EQChar{
		{
			Name:  "Arthas Menethil",
			Realm: "Normal",
			Level: 25,
			Ilvl:  40,
			Items: map[string]EquippedItem{
				"HEAD": {
					ID:           12345,
					Name:         "Forever Iron Helm",
					Ilvl:         40,
					Quality:      "rare",
					ArmorDisplay: "80 點護甲",
					Binding:      "拾取後綁定",
				},
			},
		},
	}
	eqState.fetched = time.Now()
	eqState.loading = false
	eqState.Unlock()

	sess := globalSessions.Create()
	sess.mu.Lock()
	sess.Authenticated = true
	sess.Token = &oauth2.Token{AccessToken: "mock-forever-token", Expiry: time.Now().Add(time.Hour)}
	sess.Summary = AccountSummary{
		Authenticated: true,
		Characters: []CharacterStats{
			{Name: "Arthas Menethil", Realm: "Normal", Level: 25},
		},
	}
	sess.mu.Unlock()
	sess.Equipment.Lock()
	sess.Equipment.Data = eqState.data
	sess.Equipment.Fetched = time.Now()
	sess.Equipment.Loading = false
	sess.Equipment.Unlock()
	defer globalSessions.Delete(sess.ID)

	req := httptest.NewRequest("GET", "/equipment?c=arthas-menethil-normal&lang=zh-TW", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sess.ID})
	rr := httptest.NewRecorder()

	handleEquipment(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", rr.Code)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "Forever Iron Helm") {
		t.Errorf("expected equipment page to render item name for Forever character")
	}
	if !strings.Contains(body, "80 點護甲") {
		t.Errorf("expected armor display in tooltip")
	}
	if !strings.Contains(body, "拾取後綁定") {
		t.Errorf("expected binding in tooltip")
	}
	if !strings.Contains(body, "item-tooltip") {
		t.Errorf("expected item-tooltip container in rendered page")
	}
}
