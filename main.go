package main

import (
	_ "embed"
	"encoding/json"
	"html/template"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"sync"
	"time"
)

//go:embed index.html
var indexHTML string

//go:embed reasons.json
var reasonsJSON []byte

//go:embed favicon.svg
var faviconSVG []byte

var (
	reasons   []string
	indexTmpl = template.Must(template.New("index").Parse(indexHTML))
)

type response struct {
	Reason string `json:"reason"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	count   int
	resetAt time.Time
}

func newRateLimiter() *rateLimiter {
	rl := &rateLimiter{buckets: make(map[string]*bucket)}
	go rl.cleanup()
	return rl
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.buckets[ip]
	if !ok || now.After(b.resetAt) {
		rl.buckets[ip] = &bucket{count: 1, resetAt: now.Add(time.Minute)}
		return true
	}
	if b.count >= 120 {
		return false
	}
	b.count++
	return true
}

func (rl *rateLimiter) cleanup() {
	for range time.Tick(5 * time.Minute) {
		rl.mu.Lock()
		now := time.Now()
		for ip, b := range rl.buckets {
			if now.After(b.resetAt) {
				delete(rl.buckets, ip)
			}
		}
		rl.mu.Unlock()
	}
}

func clientIP(r *http.Request) string {
	if ip := r.Header.Get("Cf-Connecting-Ip"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return ip
	}
	return r.RemoteAddr
}

func randomReason() string {
	return reasons[rand.IntN(len(reasons))]
}

func main() {
	if err := json.Unmarshal(reasonsJSON, &reasons); err != nil {
		log.Fatalf("failed to parse reasons.json: %v", err)
	}

	rl := newRateLimiter()
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /favicon.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(faviconSVG)
	})

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := indexTmpl.Execute(w, struct{ Reason string }{randomReason()}); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("GET /no", func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		if !rl.allow(ip) {
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(errorResponse{Error: "Too many requests, please try again later. (120 reqs/min/IP)"})
			return
		}

		json.NewEncoder(w).Encode(response{Reason: randomReason()})
	})

	mux.HandleFunc("OPTIONS /no", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.WriteHeader(http.StatusNoContent)
	})

	log.Printf("No-as-a-Service is running on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
