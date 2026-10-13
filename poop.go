package main

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var DisplayLocation = time.FixedZone("UTC+8", 8*60*60)

var ErrRecordNotFound = errors.New("record not found")

type Record struct {
	ID       int64
	Datetime time.Time
	Texture  string
	Smell    string
	Comments string
}

type Store struct {
	db *sql.DB
}

func InitStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}

	store := &Store{db: db}
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return store, nil
}

func (s *Store) initSchema() error {
	query := `
	CREATE TABLE IF NOT EXISTS records (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		datetime TEXT NOT NULL,
		texture TEXT NOT NULL,
		smell TEXT NOT NULL DEFAULT '',
		comments TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS idx_datetime ON records(datetime DESC);
	`
	if _, err := s.db.Exec(query); err != nil {
		return err
	}

	migrations := []struct {
		column string
		ddl    string
	}{
		{"smell", `ALTER TABLE records ADD COLUMN smell TEXT NOT NULL DEFAULT ''`},
		{"comments", `ALTER TABLE records ADD COLUMN comments TEXT NOT NULL DEFAULT ''`},
	}
	for _, m := range migrations {
		exists, err := s.hasColumn("records", m.column)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if _, err := s.db.Exec(m.ddl); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) hasColumn(table, column string) (bool, error) {
	rows, err := s.db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Create(texture, smell, comments string) (*Record, error) {
	now := time.Now().Truncate(time.Second)
	query := `INSERT INTO records (datetime, texture, smell, comments) VALUES (?, ?, ?, ?)`
	result, err := s.db.Exec(query, now.Format(time.RFC3339), texture, smell, comments)
	if err != nil {
		return nil, fmt.Errorf("failed to create record: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get last insert id: %w", err)
	}

	return &Record{
		ID:       id,
		Datetime: now,
		Texture:  texture,
		Smell:    smell,
		Comments: comments,
	}, nil
}

func (s *Store) List(limit, offset int) ([]Record, error) {
	rows, err := s.db.Query(`SELECT id, datetime, texture, smell, comments FROM records ORDER BY datetime DESC, id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list records: %w", err)
	}
	defer rows.Close()

	records := []Record{}
	for rows.Next() {
		var (
			record   Record
			datetime string
		)
		if err := rows.Scan(&record.ID, &datetime, &record.Texture, &record.Smell, &record.Comments); err != nil {
			return nil, fmt.Errorf("failed to scan record: %w", err)
		}
		record.Datetime, err = time.Parse(time.RFC3339, datetime)
		if err != nil {
			return nil, fmt.Errorf("failed to parse datetime: %w", err)
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Store) Count() (int, error) {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM records`).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count records: %w", err)
	}
	return count, nil
}

type Stats struct {
	Texture       map[string]int            `json:"texture"`
	Smell         map[string]int            `json:"smell"`
	TextureMonths map[string]map[string]int `json:"textureMonths"`
	SmellMonths   map[string]map[string]int `json:"smellMonths"`
	DaysSinceLast *int                      `json:"daysSinceLast"`
}

func (s *Store) Stats(year string) (*Stats, error) {
	texture, err := s.countBy(`SELECT texture, COUNT(*) FROM records GROUP BY texture`)
	if err != nil {
		return nil, err
	}

	smell, err := s.countBy(`SELECT smell, COUNT(*) FROM records GROUP BY smell`)
	if err != nil {
		return nil, err
	}

	textureMonths, err := s.monthCategoryCounts("texture", year)
	if err != nil {
		return nil, err
	}
	smellMonths, err := s.monthCategoryCounts("smell", year)
	if err != nil {
		return nil, err
	}

	last, err := s.Last()
	if err != nil {
		return nil, err
	}
	var daysSinceLast *int
	if last != nil {
		days := int(time.Since(*last).Hours() / 24)
		if days < 0 {
			days = 0
		}
		daysSinceLast = &days
	}

	return &Stats{
		Texture:       texture,
		Smell:         smell,
		TextureMonths: textureMonths,
		SmellMonths:   smellMonths,
		DaysSinceLast: daysSinceLast,
	}, nil
}

// Last returns the datetime of the most recent record, or nil if there are none.
func (s *Store) Last() (*time.Time, error) {
	var datetime string
	err := s.db.QueryRow(`SELECT datetime FROM records ORDER BY datetime DESC, id DESC LIMIT 1`).Scan(&datetime)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get last record: %w", err)
	}

	last, err := time.Parse(time.RFC3339, datetime)
	if err != nil {
		return nil, fmt.Errorf("failed to parse datetime: %w", err)
	}
	return &last, nil
}

// monthCategoryCounts groups records by month and the given column for the year.
// The column is always an internal literal, never user input.
func (s *Store) monthCategoryCounts(column, year string) (map[string]map[string]int, error) {
	query := fmt.Sprintf(`SELECT substr(datetime, 1, 7), %s, COUNT(*) FROM records GROUP BY 1, 2`, column)
	var args []any
	if year != "" {
		query = fmt.Sprintf(`SELECT substr(datetime, 1, 7), %s, COUNT(*) FROM records WHERE substr(datetime, 1, 4) = ? GROUP BY 1, 2`, column)
		args = append(args, year)
	}
	return s.countByPair(query, args...)
}

func (s *Store) countBy(query string, args ...any) (map[string]int, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to count records: %w", err)
	}
	defer rows.Close()

	counts := map[string]int{}
	for rows.Next() {
		var (
			key   string
			count int
		)
		if err := rows.Scan(&key, &count); err != nil {
			return nil, fmt.Errorf("failed to scan count: %w", err)
		}
		counts[key] = count
	}
	return counts, rows.Err()
}

func (s *Store) countByPair(query string, args ...any) (map[string]map[string]int, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to count records: %w", err)
	}
	defer rows.Close()

	counts := map[string]map[string]int{}
	for rows.Next() {
		var (
			first  string
			second string
			count  int
		)
		if err := rows.Scan(&first, &second, &count); err != nil {
			return nil, fmt.Errorf("failed to scan count: %w", err)
		}
		if counts[first] == nil {
			counts[first] = map[string]int{}
		}
		counts[first][second] = count
	}
	return counts, rows.Err()
}

func (s *Store) Delete(id int64) error {
	result, err := s.db.Exec(`DELETE FROM records WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to delete record: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check deleted rows: %w", err)
	}
	if affected == 0 {
		return ErrRecordNotFound
	}
	return nil
}
