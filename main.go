package main

import (
	"log"
	"net/http"
)

const sqlitedb = "store.db"

func main() {
	store, err := InitStore(sqlitedb)
	if err != nil {
		log.Fatalf("failed to initialize store: %v", err)
	}
	defer store.Close()

	server := &http.Server{
		Addr:    ":7878",
		Handler: withBasePath("/poop", newHandler(store)),
	}

	log.Println("Starting server on localhost:7878")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed to start: %v", err)
	}
}
