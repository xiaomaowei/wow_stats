package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var db *sql.DB

// initDB connects to Postgres and creates the schema if needed.
// Returns false (and logs a warning) if DATABASE_URL is not set — the app
// continues normally without persistence in that case.
func initDB() bool {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Println("DATABASE_URL not set — running without persistence (no history tracking)")
		return false
	}

	// Ensure the target database exists, creating it if necessary.
	if err := ensureDatabase(dsn); err != nil {
		log.Printf("WARNING: could not ensure database exists: %v — running without persistence", err)
		return false
	}

	var err error
	db, err = sql.Open("pgx", dsn)
	if err != nil {
		log.Printf("WARNING: could not open database connection: %v — running without persistence", err)
		db = nil
		return false
	}

	// Test the connection
	if err = db.Ping(); err != nil {
		log.Printf("WARNING: could not reach database: %v — running without persistence", err)
		db = nil
		return false
	}

	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err = migrateSchema(); err != nil {
		log.Printf("WARNING: schema migration failed: %v — running without persistence", err)
		db = nil
		return false
	}

	log.Println("Database connected — history tracking enabled")
	return true
}

// ensureDatabase connects to the Postgres server via the "postgres" maintenance
// database and creates the target database if it does not already exist.
func ensureDatabase(dsn string) error {
	u, err := url.Parse(dsn)
	if err != nil {
		return fmt.Errorf("invalid DATABASE_URL: %w", err)
	}

	// The database we actually want to use
	targetDB := u.Path // e.g. "/wow_stats"
	if len(targetDB) > 1 {
		targetDB = targetDB[1:] // strip leading "/"
	}
	if targetDB == "" {
		return fmt.Errorf("DATABASE_URL has no database name in path")
	}

	// Build a DSN pointing at the "postgres" maintenance database instead
	adminURL := *u
	adminURL.Path = "/postgres"

	adminDB, err := sql.Open("pgx", adminURL.String())
	if err != nil {
		return fmt.Errorf("could not open admin connection: %w", err)
	}
	defer adminDB.Close()

	if err = adminDB.Ping(); err != nil {
		return fmt.Errorf("could not connect to Postgres server at %s: %w", u.Host, err)
	}

	// Check if the target database already exists
	var exists bool
	err = adminDB.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)`, targetDB,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("could not query pg_database: %w", err)
	}

	if !exists {
		log.Printf("Database %q not found — creating it now...", targetDB)
		// CREATE DATABASE cannot run inside a transaction, so use Exec directly.
		// targetDB comes from our own config so quoting is safe here.
		_, err = adminDB.Exec(fmt.Sprintf(`CREATE DATABASE %q`, targetDB))
		if err != nil {
			return fmt.Errorf("could not create database %q: %w", targetDB, err)
		}
		log.Printf("Database %q created successfully", targetDB)
	} else {
		log.Printf("Database %q already exists", targetDB)
	}

	return nil
}

// migrateSchema creates the tables if they don't already exist.
func migrateSchema() error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS character_snapshots (
			id             BIGSERIAL PRIMARY KEY,
			account_id     TEXT,
			recorded_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			character_name TEXT        NOT NULL,
			realm          TEXT        NOT NULL,
			level          INT,
			item_level     INT,
			gold           BIGINT
		);

		CREATE TABLE IF NOT EXISTS account_snapshots (
			id           BIGSERIAL PRIMARY KEY,
			account_id   TEXT,
			recorded_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			total_gold   BIGINT,
			mounts       INT,
			pets         INT,
			char_count   INT
		);

		ALTER TABLE character_snapshots ADD COLUMN IF NOT EXISTS account_id TEXT;
		ALTER TABLE account_snapshots ADD COLUMN IF NOT EXISTS account_id TEXT;

		CREATE INDEX IF NOT EXISTS idx_char_snapshots_account_name_realm
			ON character_snapshots (account_id, character_name, realm, recorded_at DESC);

		CREATE INDEX IF NOT EXISTS idx_account_snapshots_account_recorded
			ON account_snapshots (account_id, recorded_at DESC);

		CREATE INDEX IF NOT EXISTS idx_char_snapshots_name_realm
			ON character_snapshots (character_name, realm, recorded_at DESC);

		CREATE INDEX IF NOT EXISTS idx_account_snapshots_recorded_at
			ON account_snapshots (recorded_at DESC);
	`)
	return err
}

// saveSnapshot writes a timestamped snapshot of current data to Postgres for the specified account.
// Called in a goroutine after each successful data refresh — never blocks the UI.
// Existing unowned history remains intact and untouched; snapshots without an account ID are rejected.
func saveSnapshot(summary AccountSummary, accountIDs ...string) {
	if db == nil {
		return
	}
	var accountID string
	if len(accountIDs) > 0 {
		accountID = strings.TrimSpace(accountIDs[0])
	}
	if accountID == "" {
		return
	}

	tx, err := db.Begin()
	if err != nil {
		log.Printf("DB snapshot: could not begin transaction: %v", err)
		return
	}
	defer tx.Rollback()

	// Account-wide snapshot
	_, err = tx.Exec(`
		INSERT INTO account_snapshots (account_id, total_gold, mounts, pets, char_count)
		VALUES ($1, $2, $3, $4, $5)`,
		accountID,
		summary.TotalGold,
		summary.MountsCollected,
		summary.PetsCollected,
		len(summary.Characters),
	)
	if err != nil {
		log.Printf("DB snapshot: account insert failed: %v", err)
		return
	}

	// Per-character snapshots (skip stub/error characters)
	for _, c := range summary.Characters {
		if c.Error != "" {
			continue
		}
		_, err = tx.Exec(`
			INSERT INTO character_snapshots (account_id, character_name, realm, level, item_level, gold)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			accountID,
			c.Name, c.Realm, c.Level, c.ItemLevel, c.Gold,
		)
		if err != nil {
			log.Printf("DB snapshot: character insert failed for %s-%s: %v", c.Name, c.Realm, err)
			return
		}
	}

	if err = tx.Commit(); err != nil {
		log.Printf("DB snapshot: commit failed: %v", err)
		return
	}

	log.Printf("DB snapshot saved for account %s — %d characters, total gold: %s",
		accountID, len(summary.Characters), formatGold(summary.TotalGold))
}

// ── History query types ──────────────────────────────────────────────────────

type AccountHistory struct {
	Labels    []string // formatted timestamps
	TotalGold []int64
	Mounts    []int
	Pets      []int
}

type CharacterHistory struct {
	Name       string
	Realm      string
	Labels     []string
	ItemLevel  []int
	Level      []int
	Gold       []int64
	LatestIlvl int // used for sorting — not rendered directly
}

// getAccountHistory returns the last 90 account snapshots for the specified account, oldest first.
// If accountID is empty or db is nil, an empty AccountHistory is returned.
// Existing unowned rows (where account_id IS NULL) are never returned.
func getAccountHistory(accountIDs ...string) (AccountHistory, error) {
	var h AccountHistory
	if db == nil {
		return h, nil
	}
	var accountID string
	if len(accountIDs) > 0 {
		accountID = strings.TrimSpace(accountIDs[0])
	}
	if accountID == "" {
		return h, nil
	}

	rows, err := db.Query(`
		SELECT recorded_at, total_gold, mounts, pets
		FROM account_snapshots
		WHERE account_id = $1
		ORDER BY recorded_at DESC
		LIMIT 90
	`, accountID)
	if err != nil {
		return h, err
	}
	defer rows.Close()

	type row struct {
		t    time.Time
		gold int64
		mnt  int
		pets int
	}
	var buf []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.t, &r.gold, &r.mnt, &r.pets); err != nil {
			return h, err
		}
		buf = append(buf, r)
	}

	// Reverse so oldest is first (for chart left-to-right)
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}

	for _, r := range buf {
		h.Labels = append(h.Labels, r.t.Format("02 Jan 15:04"))
		h.TotalGold = append(h.TotalGold, r.gold)
		h.Mounts = append(h.Mounts, r.mnt)
		h.Pets = append(h.Pets, r.pets)
	}
	return h, nil
}

// getCharacterHistories returns item level + gold + level history for every
// character belonging to accountID that has at least 2 snapshots, limited to the last 90 points each.
// Results are sorted by the character's most recent item level, highest first
// (matching the home page ordering).
// Existing unowned rows (where account_id IS NULL) are never returned.
func getCharacterHistories(accountIDs ...string) ([]CharacterHistory, error) {
	if db == nil {
		return nil, nil
	}
	var accountID string
	if len(accountIDs) > 0 {
		accountID = strings.TrimSpace(accountIDs[0])
	}
	if accountID == "" {
		return nil, nil
	}

	// Get distinct characters for this account that have history.
	charRows, err := db.Query(`
		SELECT DISTINCT character_name, realm
		FROM character_snapshots
		WHERE account_id = $1
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer charRows.Close()

	type charKey struct{ name, realm string }
	var chars []charKey
	for charRows.Next() {
		var k charKey
		if err := charRows.Scan(&k.name, &k.realm); err != nil {
			return nil, err
		}
		chars = append(chars, k)
	}
	charRows.Close()

	var histories []CharacterHistory
	for _, c := range chars {
		rows, err := db.Query(`
			SELECT recorded_at, item_level, level, gold
			FROM character_snapshots
			WHERE account_id = $1 AND character_name = $2 AND realm = $3
			ORDER BY recorded_at DESC
			LIMIT 90
		`, accountID, c.name, c.realm)
		if err != nil {
			return nil, err
		}

		type snap struct {
			t    time.Time
			ilvl int
			lvl  int
			gold int64
		}
		var buf []snap
		for rows.Next() {
			var s snap
			if err := rows.Scan(&s.t, &s.ilvl, &s.lvl, &s.gold); err != nil {
				rows.Close()
				return nil, err
			}
			buf = append(buf, s)
		}
		rows.Close()

		if len(buf) < 2 {
			continue // not enough data to show a trend yet
		}

		// Reverse oldest-first for left-to-right chart rendering
		for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
			buf[i], buf[j] = buf[j], buf[i]
		}

		h := CharacterHistory{
			Name:       c.name,
			Realm:      c.realm,
			LatestIlvl: buf[len(buf)-1].ilvl, // most recent snapshot (last after reversal)
		}
		for _, s := range buf {
			h.Labels = append(h.Labels, s.t.Format("02 Jan 15:04"))
			h.ItemLevel = append(h.ItemLevel, s.ilvl)
			h.Level = append(h.Level, s.lvl)
			h.Gold = append(h.Gold, s.gold)
		}
		histories = append(histories, h)
	}

	// Sort by latest item level descending — highest geared characters first,
	// matching the home page character card ordering.
	for i := 1; i < len(histories); i++ {
		for j := i; j > 0 && histories[j].LatestIlvl > histories[j-1].LatestIlvl; j-- {
			histories[j], histories[j-1] = histories[j-1], histories[j]
		}
	}

	return histories, nil
}
