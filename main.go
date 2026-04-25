package main

import (
	"encoding/json"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"sync"
	"time"
)

var reasons []string

type response struct {
	Reason string `json:"reason"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// per-IP rate limiter: 120 req/min
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	count    int
	resetAt  time.Time
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

func main() {
	data, err := os.ReadFile("reasons.json")
	if err != nil {
		log.Fatalf("failed to load reasons.json: %v", err)
	}
	if err := json.Unmarshal(data, &reasons); err != nil {
		log.Fatalf("failed to parse reasons.json: %v", err)
	}

	rl := newRateLimiter()
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /no", func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		if !rl.allow(ip) {
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(errorResponse{Error: "Too many requests, please try again later. (120 reqs/min/IP)"})
			return
		}

		json.NewEncoder(w).Encode(response{Reason: reasons[rand.IntN(len(reasons))]})
	})

	mux.HandleFunc("OPTIONS /no", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.WriteHeader(http.StatusNoContent)
	})

	log.Printf("No-as-a-Service is running on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
