package main

import (
	"flag"
	"log"
	"net/http"
	"path/filepath"
	"runtime"

	"flip7/internal/api"
	"flip7/internal/hub"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	flag.Parse()

	// Locate the web/ directory relative to this source file so the server
	// works regardless of the working directory when launched.
	_, filename, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(filename), "..", "..")
	webDir := filepath.Join(root, "web")

	h := hub.New()
	router := api.NewRouter(h, webDir)

	log.Printf("Flip 7 server listening on http://localhost%s", *addr)
	if err := http.ListenAndServe(*addr, router); err != nil {
		log.Fatal(err)
	}
}
