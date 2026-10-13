package main

import (
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"strconv"
)

//go:embed web
var webFS embed.FS

// withBasePath mounts h so it is served under base (e.g. "/poop"). A request
// to the bare base redirects to base+"/" so relative asset and API paths in the
// page resolve under the prefix.
func withBasePath(base string, h http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(base, http.RedirectHandler(base+"/", http.StatusMovedPermanently))
	mux.Handle(base+"/", http.StripPrefix(base, h))
	return mux
}

func newHandler(store *Store) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/records", func(w http.ResponseWriter, r *http.Request) {
		limit := queryInt(r, "limit", defaultPageSize)
		if limit < 1 {
			limit = defaultPageSize
		} else if limit > maxPageSize {
			limit = maxPageSize
		}
		offset := queryInt(r, "offset", 0)
		if offset < 0 {
			offset = 0
		}

		records, err := store.List(limit, offset)
		if err != nil {
			writeError(w, err)
			return
		}
		total, err := store.Count()
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"records": records,
			"total":   total,
			"limit":   limit,
			"offset":  offset,
		})
	})

	mux.HandleFunc("GET /api/stats", func(w http.ResponseWriter, r *http.Request) {
		stats, err := store.Stats(r.URL.Query().Get("year"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, stats)
	})

	mux.HandleFunc("POST /api/records", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Texture  string `json:"texture"`
			Smell    string `json:"smell"`
			Comments string `json:"comments"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}

		record, err := store.Create(body.Texture, body.Smell, body.Comments)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, record)
	})

	mux.HandleFunc("DELETE /api/records/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := store.Delete(id); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	static, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /", http.FileServer(http.FS(static)))

	return mux
}

const (
	defaultPageSize = 20
	maxPageSize     = 100
)

func queryInt(r *http.Request, key string, fallback int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return fallback
	}
	return value
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("failed to encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrRecordNotFound) {
		http.Error(w, "record not found", http.StatusNotFound)
		return
	}
	log.Printf("request failed: %v", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}
