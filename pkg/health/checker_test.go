package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nebari-dev/jhub-app-proxy/pkg/logger"
)

func TestChecker_CheckOnce_Success(t *testing.T) {
	// Create test server that returns 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := DefaultCheckConfig(server.URL)
	log := logger.New(logger.DefaultConfig())
	checker := NewChecker(cfg, log)

	ctx := context.Background()
	err := checker.CheckOnce(ctx)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestChecker_CheckOnce_Failure(t *testing.T) {
	// Create test server that returns 500
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := DefaultCheckConfig(server.URL)
	log := logger.New(logger.DefaultConfig())
	checker := NewChecker(cfg, log)

	ctx := context.Background()
	err := checker.CheckOnce(ctx)
	if err == nil {
		t.Error("expected error for 500 status, got nil")
	}
}

func TestChecker_WaitUntilReady_Success(t *testing.T) {
	attempts := 0
	// Create test server that succeeds after 2 attempts
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts >= 2 {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()

	cfg := CheckConfig{
		URL:              server.URL,
		Timeout:          5 * time.Second,
		Interval:         100 * time.Millisecond,
		InitialDelay:     0,
		SuccessThreshold: 1,
		HTTPTimeout:      1 * time.Second,
	}

	log := logger.New(logger.DefaultConfig())
	checker := NewChecker(cfg, log)

	ctx := context.Background()
	err := checker.WaitUntilReady(ctx)
	if err != nil {
		t.Errorf("expected process to become ready, got error: %v", err)
	}

	if attempts < 2 {
		t.Errorf("expected at least 2 attempts, got %d", attempts)
	}
}

func TestChecker_WaitUntilReady_Timeout(t *testing.T) {
	// Create test server that never succeeds
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := CheckConfig{
		URL:              server.URL,
		Timeout:          500 * time.Millisecond,
		Interval:         100 * time.Millisecond,
		InitialDelay:     0,
		SuccessThreshold: 1,
		HTTPTimeout:      100 * time.Millisecond,
	}

	log := logger.New(logger.DefaultConfig())
	checker := NewChecker(cfg, log)

	ctx := context.Background()
	err := checker.WaitUntilReady(ctx)
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
}

func TestChecker_WaitUntilReady_SuccessThreshold(t *testing.T) {
	attempts := 0
	// Create test server that alternates between success and failure
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		// Fail every other request until attempt 5
		if attempts < 5 && attempts%2 == 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	cfg := CheckConfig{
		URL:              server.URL,
		Timeout:          5 * time.Second,
		Interval:         100 * time.Millisecond,
		InitialDelay:     0,
		SuccessThreshold: 2, // Require 2 consecutive successes
		HTTPTimeout:      1 * time.Second,
	}

	log := logger.New(logger.DefaultConfig())
	checker := NewChecker(cfg, log)

	ctx := context.Background()
	err := checker.WaitUntilReady(ctx)
	if err != nil {
		t.Errorf("expected process to become ready, got error: %v", err)
	}
}

func TestDefaultCheckConfig(t *testing.T) {
	url := "http://localhost:8080/health"
	cfg := DefaultCheckConfig(url)

	if cfg.URL != url {
		t.Errorf("expected URL %s, got %s", url, cfg.URL)
	}
	if cfg.Timeout == 0 {
		t.Error("expected non-zero timeout")
	}
	if cfg.Interval == 0 {
		t.Error("expected non-zero interval")
	}
}
