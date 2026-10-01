package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type runRequest struct {
	Input json.RawMessage `json:"input"`
}
type runResponse struct {
	Status string `json:"status"`
	RequestID string `json:"request_id"`
	DS json.RawMessage `json:"ds"`
	ML json.RawMessage `json:"ml"`
}

func main() {
	dsURL := strings.TrimRight(env("DS_URL", "http://ds:8001"), "/")
	mlURL := strings.TrimRight(env("ML_URL", "http://ml:8002"), "/")
	client := &http.Client{Timeout: 10 * time.Second}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := check(ctx, client, dsURL+"/healthz"); err != nil {
			writeError(w, http.StatusServiceUnavailable, "ds_unavailable", "DS service is not ready.")
			return
		}
		if err := check(ctx, client, mlURL+"/healthz"); err != nil {
			writeError(w, http.StatusServiceUnavailable, "ml_unavailable", "ML service is not ready.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status":"ok","backend":"ok","ds":"ok","ml":"ok"})
	})
	mux.HandleFunc("POST /api/run", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var input runRequest
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil || len(input.Input) == 0 || !json.Valid(input.Input) {
			writeError(w, http.StatusBadRequest, "invalid_request", "Send JSON shaped as {\"input\": {...}}.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		dsResult, err := postJSON(ctx, client, dsURL+"/analyze", map[string]json.RawMessage{"input":input.Input})
		if err != nil {
			writeError(w, http.StatusBadGateway, "ds_request_failed", "DS service failed or returned invalid JSON.")
			return
		}
		mlPayload := struct {
			Input json.RawMessage `json:"input"`
			DS json.RawMessage `json:"ds_result"`
		}{Input:input.Input, DS:dsResult}
		mlResult, err := postJSON(ctx, client, mlURL+"/predict", mlPayload)
		if err != nil {
			writeError(w, http.StatusBadGateway, "ml_request_failed", "ML service failed or returned invalid JSON.")
			return
		}
		writeJSON(w, http.StatusOK, runResponse{Status:"ok",RequestID:newRequestID(),DS:dsResult,ML:mlResult})
	})

	server := &http.Server{Addr:":8080",Handler:mux,ReadHeaderTimeout:5*time.Second,ReadTimeout:10*time.Second,WriteTimeout:25*time.Second,IdleTimeout:60*time.Second}
	log.Printf("backend listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}

func check(ctx context.Context, client *http.Client, endpoint string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil { return err }
	res, err := client.Do(req)
	if err != nil { return err }
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 { return io.ErrUnexpectedEOF }
	return nil
}
func postJSON(ctx context.Context, client *http.Client, endpoint string, payload any) (json.RawMessage, error) {
	body, err := json.Marshal(payload)
	if err != nil { return nil, err }
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil { return nil, err }
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil { return nil, err }
	defer res.Body.Close()
	response, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil { return nil, err }
	if res.StatusCode < 200 || res.StatusCode >= 300 || !json.Valid(response) { return nil, io.ErrUnexpectedEOF }
	return json.RawMessage(response), nil
}
func newRequestID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil { return time.Now().UTC().Format("20060102T150405.000000000") }
	return hex.EncodeToString(b[:])
}
func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" { return value }
	return fallback
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"status":"error","error":map[string]string{"code":code,"message":message}})
}
