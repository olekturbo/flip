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
		defer func() {
			if r := recover(); r != nil {
				log.Printf("self-ping goroutine panic: %v", r)
			}
		}()
		url := "http://127.0.0.1" + *addr + "/healthz"
		client := &http.Client{Timeout: 5 * time.Second}
		log.Printf("self-ping goroutine started, url=%s", url)
		time.Sleep(10 * time.Second) // wait for server to be ready
		for {
			resp, err := client.Get(url)
			if err != nil {
				log.Printf("self-ping failed: %v", err)
			} else {
				resp.Body.Close()
				log.Printf("self-ping ok")
			}
			time.Sleep(5 * time.Minute)
		}
	}()

	if err := http.ListenAndServe(*addr, router); err != nil {
		log.Fatal(err)
	}
}
