package main

import (
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"

	assets "flip7"
	"flip7/internal/api"
	"flip7/internal/hub"
)

func main() {
	defaultAddr := ":8080"
	if port := os.Getenv("PORT"); port != "" {
		defaultAddr = ":" + port
	}
	addr := flag.String("addr", defaultAddr, "HTTP listen address")
	flag.Parse()

	// Strip the "web" prefix so paths resolve as /index.html, not /web/index.html.
	webFS, err := fs.Sub(assets.WebFS, "web")
	if err != nil {
		log.Fatal(err)
	}

	h := hub.New()
	router := api.NewRouter(h, webFS)

	log.Printf("Flip 7 listening on %s", *addr)

	// Self-ping every 10 minutes so Render.com's free tier does not spin the
	// container down (it kills services after 15 min of no inbound HTTP traffic,
	// even when WebSocket connections are active).
	go func() {
		url := "http://localhost" + *addr + "/healthz"
		client := &http.Client{Timeout: 5 * time.Second}
		for range time.Tick(5 * time.Minute) {
			resp, err := client.Get(url)
			if err != nil {
				log.Printf("self-ping failed: %v", err)
			} else {
				resp.Body.Close()
				log.Printf("self-ping ok")
			}
		}
	}()

	if err := http.ListenAndServe(*addr, router); err != nil {
		log.Fatal(err)
	}
}
