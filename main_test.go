package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestServer(t *testing.T) *httptest.Server {
	return newTestServerAtBase(t, "")
}

func newTestServerAtBase(t *testing.T, base string) *httptest.Server {
	t.Helper()

	store, err := InitStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	var handler http.Handler = newHandler(store)
	if base != "" {
		handler = withBasePath(base, handler)
	}

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func mustCreate(t *testing.T, srv *httptest.Server, texture, smell, comments string) Record {
	t.Helper()

	body, err := json.Marshal(map[string]string{
		"texture":  texture,
		"smell":    smell,
		"comments": comments,
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	resp, err := http.Post(srv.URL+"/api/records", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/records: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", resp.StatusCode)
	}

	var rec Record
	if err := json.NewDecoder(resp.Body).Decode(&rec); err != nil {
		t.Fatalf("decode created record: %v", err)
	}
	return rec
}

func TestCreateThenList(t *testing.T) {
	srv := newTestServer(t)
	mustCreate(t, srv, "smooth", "", "")

	resp, err := http.Get(srv.URL + "/api/records")
	if err != nil {
		t.Fatalf("GET /api/records: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "smooth") {
		t.Errorf("list body = %q, want it to contain the created texture", body)
	}
}

func TestCreateStoresSmellAndComments(t *testing.T) {
	srv := newTestServer(t)
	rec := mustCreate(t, srv, "soft", "strong", "after coffee")

	if rec.Smell != "strong" {
		t.Errorf("created smell = %q, want strong", rec.Smell)
	}
	if rec.Comments != "after coffee" {
		t.Errorf("created comments = %q, want after coffee", rec.Comments)
	}

	resp, err := http.Get(srv.URL + "/api/records")
	if err != nil {
		t.Fatalf("GET /api/records: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "strong") {
		t.Errorf("list body = %q, want it to contain smell", body)
	}
	if !strings.Contains(string(body), "after coffee") {
		t.Errorf("list body = %q, want it to contain comments", body)
	}
}

func TestDeleteRecord(t *testing.T) {
	srv := newTestServer(t)
	rec := mustCreate(t, srv, "rough", "", "")

	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/records/%d", srv.URL, rec.ID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE record: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/records/%d", srv.URL, rec.ID), nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE missing record: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("second delete status = %d, want 404", resp.StatusCode)
	}
}

func TestInitStoreMigratesLegacySchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE records (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		datetime TEXT NOT NULL,
		texture TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	db.Close()

	store, err := InitStore(path)
	if err != nil {
		t.Fatalf("init store: %v", err)
	}
	defer store.Close()

	rec, err := store.Create("soft", "strong", "note")
	if err != nil {
		t.Fatalf("create record: %v", err)
	}
	if rec.Smell != "strong" || rec.Comments != "note" {
		t.Errorf("created record = %+v, want smell strong and comments note", rec)
	}
}

type listResponse struct {
	Records []Record `json:"records"`
	Total   int      `json:"total"`
	Limit   int      `json:"limit"`
	Offset  int      `json:"offset"`
}

func getList(t *testing.T, srv *httptest.Server, query string) listResponse {
	t.Helper()

	resp, err := http.Get(srv.URL + "/api/records" + query)
	if err != nil {
		t.Fatalf("GET /api/records%s: %v", query, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200", resp.StatusCode)
	}

	var out listResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return out
}

func TestListPaging(t *testing.T) {
	srv := newTestServer(t)
	for i := 0; i < 3; i++ {
		mustCreate(t, srv, fmt.Sprintf("texture-%d", i), "Strong", "")
	}

	first := getList(t, srv, "?limit=2&offset=0")
	if len(first.Records) != 2 {
		t.Errorf("first page records = %d, want 2", len(first.Records))
	}
	if first.Total != 3 {
		t.Errorf("total = %d, want 3", first.Total)
	}

	second := getList(t, srv, "?limit=2&offset=2")
	if len(second.Records) != 1 {
		t.Errorf("second page records = %d, want 1", len(second.Records))
	}
}

type statsResponse struct {
	Texture       map[string]int            `json:"texture"`
	Smell         map[string]int            `json:"smell"`
	TextureMonths map[string]map[string]int `json:"textureMonths"`
	SmellMonths   map[string]map[string]int `json:"smellMonths"`
}

func TestStats(t *testing.T) {
	srv := newTestServer(t)
	mustCreate(t, srv, "Soft", "Strong", "")
	mustCreate(t, srv, "Soft", "Normal", "")
	mustCreate(t, srv, "Hard", "Strong", "")

	resp, err := http.Get(srv.URL + "/api/stats")
	if err != nil {
		t.Fatalf("GET /api/stats: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stats status = %d, want 200", resp.StatusCode)
	}

	var stats statsResponse
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}

	if stats.Texture["Soft"] != 2 || stats.Texture["Hard"] != 1 {
		t.Errorf("texture stats = %v, want Soft:2 Hard:1", stats.Texture)
	}
	if stats.Smell["Strong"] != 2 || stats.Smell["Normal"] != 1 {
		t.Errorf("smell stats = %v, want Strong:2 Normal:1", stats.Smell)
	}

	sum := 0
	for _, counts := range stats.TextureMonths {
		for _, count := range counts {
			sum += count
		}
	}
	if sum != 3 {
		t.Errorf("texture months total = %d, want 3", sum)
	}
}

func TestStatsMonths(t *testing.T) {
	srv := newTestServer(t)
	rec := mustCreate(t, srv, "Soft", "Strong", "")
	mustCreate(t, srv, "Soft", "Normal", "")
	mustCreate(t, srv, "Hard", "Strong", "")

	resp, err := http.Get(srv.URL + "/api/stats?year=" + rec.Datetime.Format("2006"))
	if err != nil {
		t.Fatalf("GET /api/stats: %v", err)
	}
	defer resp.Body.Close()

	var stats statsResponse
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}

	month := rec.Datetime.Format("2006-01")
	if stats.TextureMonths[month]["Soft"] != 2 {
		t.Errorf("Soft in %s = %d, want 2", month, stats.TextureMonths[month]["Soft"])
	}
	if stats.TextureMonths[month]["Hard"] != 1 {
		t.Errorf("Hard in %s = %d, want 1", month, stats.TextureMonths[month]["Hard"])
	}
	if stats.SmellMonths[month]["Strong"] != 2 {
		t.Errorf("Strong smell in %s = %d, want 2", month, stats.SmellMonths[month]["Strong"])
	}
	if stats.SmellMonths[month]["Normal"] != 1 {
		t.Errorf("Normal smell in %s = %d, want 1", month, stats.SmellMonths[month]["Normal"])
	}
}

func TestDaysSinceLast(t *testing.T) {
	store, err := InitStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init store: %v", err)
	}
	defer store.Close()

	stats, err := store.Stats("")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.DaysSinceLast != nil {
		t.Errorf("daysSinceLast with no records = %d, want nil", *stats.DaysSinceLast)
	}

	old := time.Now().Add(-96 * time.Hour).Format(time.RFC3339)
	if _, err := store.db.Exec(
		`INSERT INTO records (datetime, texture, smell, comments) VALUES (?, 'Soft', 'Strong', '')`, old,
	); err != nil {
		t.Fatalf("insert old record: %v", err)
	}

	stats, err = store.Stats("")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.DaysSinceLast == nil {
		t.Fatal("daysSinceLast = nil, want 4")
	}
	if *stats.DaysSinceLast != 4 {
		t.Errorf("daysSinceLast = %d, want 4", *stats.DaysSinceLast)
	}
}

func TestServesUnderBasePath(t *testing.T) {
	srv := newTestServerAtBase(t, "/poop")

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	resp, err := client.Get(srv.URL + "/poop")
	if err != nil {
		t.Fatalf("GET /poop: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMovedPermanently {
		t.Errorf("GET /poop status = %d, want 301", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/poop/" {
		t.Errorf("GET /poop Location = %q, want /poop/", loc)
	}

	resp, err = http.Get(srv.URL + "/poop/")
	if err != nil {
		t.Fatalf("GET /poop/: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /poop/ status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), `id="records"`) {
		t.Errorf("prefixed index did not look like the app shell")
	}

	resp, err = http.Get(srv.URL + "/poop/api/records")
	if err != nil {
		t.Fatalf("GET /poop/api/records: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /poop/api/records status = %d, want 200", resp.StatusCode)
	}
}

func TestServesIndex(t *testing.T) {
	srv := newTestServer(t)

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `id="records"`) {
		t.Errorf("index body did not look like the app shell")
	}
}
