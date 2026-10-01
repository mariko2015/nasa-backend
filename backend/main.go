package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

type runRequest struct {
	Input json.RawMessage `json:"input"`
}

type runResponse struct {
	Status    string          `json:"status"`
	RequestID string          `json:"request_id"`
	DS        json.RawMessage `json:"ds"`
	ML        json.RawMessage `json:"ml"`
}

type config struct {
	dsURL           string
	mlURL           string
	requestTimeout  time.Duration
	shutdownTimeout time.Duration
	logLevel        slog.Level
}

func main() {
	cfg := loadConfig()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.logLevel}))
	client := &http.Client{Timeout: cfg.requestTimeout}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := check(ctx, client, cfg.dsURL+"/healthz"); err != nil {
			writeError(w, http.StatusServiceUnavailable, "ds_unavailable", "DS service is not ready.")
			return
		}
		if err := check(ctx, client, cfg.mlURL+"/healthz"); err != nil {
			writeError(w, http.StatusServiceUnavailable, "ml_unavailable", "ML service is not ready.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "backend": "ok", "ds": "ok", "ml": "ok"})
	})

	mux.HandleFunc("POST /api/run", func(w http.ResponseWriter, r *http.Request) {
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json.")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var input runRequest
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&input); err != nil || len(input.Input) == 0 || !isJSONObject(input.Input) {
			writeError(w, http.StatusBadRequest, "invalid_request", "Send one JSON object shaped as {\"input\": {...}}.")
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid_request", "Request body must contain exactly one JSON object.")
			return
		}

		requestID := requestIDFromContext(r.Context())
		ctx, cancel := context.WithTimeout(r.Context(), cfg.requestTimeout)
		defer cancel()
		dsResult, err := postJSON(ctx, client, cfg.dsURL+"/analyze", map[string]json.RawMessage{"input": input.Input}, requestID)
		if err != nil {
			logger.ErrorContext(ctx, "DS request failed", "request_id", requestID, "error", err)
			writeError(w, http.StatusBadGateway, "ds_request_failed", "DS service failed or returned invalid JSON.")
			return
		}
		mlPayload := struct {
			Input json.RawMessage `json:"input"`
			DS    json.RawMessage `json:"ds_result"`
		}{Input: input.Input, DS: dsResult}
		mlResult, err := postJSON(ctx, client, cfg.mlURL+"/predict", mlPayload, requestID)
		if err != nil {
			logger.ErrorContext(ctx, "ML request failed", "request_id", requestID, "error", err)
			writeError(w, http.StatusBadGateway, "ml_request_failed", "ML service failed or returned invalid JSON.")
			return
		}
		writeJSON(w, http.StatusOK, runResponse{Status: "ok", RequestID: requestID, DS: dsResult, ML: mlResult})
	})

	server := &http.Server{
		Addr: ":8080", Handler: requestLogging(requestIDMiddleware(mux), logger),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: cfg.requestTimeout + 5*time.Second, IdleTimeout: 60 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("backend listening", "address", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP server stopped", "error", err)
			os.Exit(1)
		}
	case <-signalCtx.Done():
		ctx, cancel := context.WithTimeout(context.Background(), cfg.shutdownTimeout)
		defer cancel()
		logger.Info("graceful shutdown started")
		if err := server.Shutdown(ctx); err != nil {
			logger.Error("graceful shutdown timed out", "error", err)
			_ = server.Close()
			os.Exit(1)
		}
		logger.Info("backend stopped")
	}
}

type contextKey string

const requestIDKey contextKey = "request_id"

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requestLogging(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		logger.InfoContext(r.Context(), "HTTP request", "request_id", requestIDFromContext(r.Context()), "method", r.Method, "path", r.URL.Path, "status", recorder.status, "duration_ms", time.Since(started).Milliseconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func requestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return "unknown"
}

func loadConfig() config {
	return config{
		dsURL:           strings.TrimRight(env("DS_URL", "http://ds:8001"), "/"),
		mlURL:           strings.TrimRight(env("ML_URL", "http://ml:8002"), "/"),
		requestTimeout:  durationEnv("REQUEST_TIMEOUT", 20*time.Second),
		shutdownTimeout: durationEnv("SHUTDOWN_TIMEOUT", 10*time.Second),
		logLevel:        parseLogLevel(env("LOG_LEVEL", "INFO")),
	}
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		panic("invalid positive duration in " + key)
	}
	return parsed
}

func parseLogLevel(value string) slog.Level {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func isJSONObject(raw json.RawMessage) bool {
	var value map[string]json.RawMessage
	return json.Unmarshal(raw, &value) == nil && value != nil
}

func check(ctx context.Context, client *http.Client, endpoint string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func postJSON(ctx context.Context, client *http.Client, endpoint string, payload any, requestID string) (json.RawMessage, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", requestID)
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	response, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 || !json.Valid(response) {
		return nil, io.ErrUnexpectedEOF
	}
	return json.RawMessage(response), nil
}

func newRequestID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().UTC().Format("20060102T150405.000000000")
	}
	return hex.EncodeToString(b[:])
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"status": "error", "error": map[string]string{"code": code, "message": message}})
}
