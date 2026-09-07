//go:build unit

package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/directory"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type pressureDirectoryRepo struct {
	started chan struct{}
	release chan struct{}
	mu      sync.Mutex
	calls   int
	active  atomic.Int32
}

func (r *pressureDirectoryRepo) List(ctx context.Context) ([]directory.Row, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	r.active.Add(1)
	defer r.active.Add(-1)
	select {
	case r.started <- struct{}{}:
	default:
	}
	select {
	case <-r.release:
		return pressureDirectoryRows(), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r *pressureDirectoryRepo) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func (r *pressureDirectoryRepo) activeCount() int32 { return r.active.Load() }

func pressureDirectoryRows() []directory.Row {
	id := int64(9007199254740993)
	name, platform, kind, status := "pressure", "openai", "apikey", "active"
	return []directory.Row{{
		GeneratedAt: time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC),
		ID:          &id, Name: &name, Platform: &platform, Type: &kind, Status: &status,
	}}
}

func TestDirectoryPressurePreservesNativeAIRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"healthy", "first_429"} {
		t.Run(mode, func(t *testing.T) {
			for round := 0; round < 3; round++ {
				body := []byte(`{"model":"grok","messages":[{"role":"user","content":"hello"}],"stream":false}`)
				_, _, baselineUpstream, baselineRouter, baselineCleanup := newGrokCredentialFailoverHandler(t, "first_429")
				// The existing first_429 fixture gives both accounts valid credentials.
				// Healthy mode removes only the controlled upstream failure, before requests start.
				if mode == "healthy" {
					baselineUpstream.rateLimitIDs = nil
				}
				baselineStart := time.Now()
				baselineResp := httptest.NewRecorder()
				baselineReq := httptest.NewRequest(http.MethodPost, "/openai/v1/chat/completions", bytes.NewReader(body))
				baselineReq.Header.Set("Content-Type", "application/json")
				baselineRouter.ServeHTTP(baselineResp, baselineReq)
				baselineElapsed := time.Since(baselineStart)
				baselineCleanup()
				require.Equal(t, http.StatusOK, baselineResp.Code, baselineResp.Body.String())
				require.Contains(t, baselineResp.Body.String(), "resp_healthy")
				baselineHits := baselineUpstream.accountHits()
				expectedHits := []int64{801}
				if mode == "first_429" {
					expectedHits = []int64{801, 802}
				}
				require.Equal(t, expectedHits, baselineHits)

				_, _, pressureUpstream, pressureRouter, pressureCleanup := newGrokCredentialFailoverHandler(t, "first_429")
				if mode == "healthy" {
					pressureUpstream.rateLimitIDs = nil
				}
				defer pressureCleanup()
				token := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x2a}, 32))
				directoryRepo := &pressureDirectoryRepo{started: make(chan struct{}, 2), release: make(chan struct{})}
				directoryService, err := directory.NewService(directoryRepo, &config.DirectoryConfig{Enabled: true, CurrentToken: token})
				require.NoError(t, err)
				pressureRouter.Any("/internal/v1/api-account-directory", directory.NewHandler(directoryService).Get)

				var directoryWG sync.WaitGroup
				var releaseOnce sync.Once
				releaseDirectory := func() { releaseOnce.Do(func() { close(directoryRepo.release) }) }
				defer func() {
					releaseDirectory()
					directoryWG.Wait()
				}()
				directoryResponses := make(chan *httptest.ResponseRecorder, 4)
				for i := 0; i < 4; i++ {
					directoryWG.Add(1)
					go func() {
						defer directoryWG.Done()
						req := httptest.NewRequest(http.MethodGet, "/internal/v1/api-account-directory", nil)
						req.Header.Set("Authorization", "Bearer "+token)
						resp := httptest.NewRecorder()
						pressureRouter.ServeHTTP(resp, req)
						directoryResponses <- resp
					}()
				}
				for i := 0; i < 2; i++ {
					select {
					case <-directoryRepo.started:
					case <-time.After(time.Second):
						t.Fatal("directory request did not occupy its bounded slot")
					}
				}
				require.Equal(t, int32(2), directoryRepo.activeCount())

				responses := make([]*httptest.ResponseRecorder, 0, 4)
				// Wait for both rejected requests before exercising AI. Keep
				// their responses for the exact final response-count check.
				for i := 0; i < 2; i++ {
					select {
					case resp := <-directoryResponses:
						require.Equal(t, http.StatusTooManyRequests, resp.Code)
						responses = append(responses, resp)
					case <-time.After(time.Second):
						t.Fatal("Directory overflow request did not return while queries were blocked")
					}
				}
				require.Equal(t, int32(2), directoryRepo.activeCount())
				aiStart := time.Now()
				aiResp := httptest.NewRecorder()
				aiReq := httptest.NewRequest(http.MethodPost, "/openai/v1/chat/completions", bytes.NewReader(body))
				aiReq.Header.Set("Content-Type", "application/json")
				pressureRouter.ServeHTTP(aiResp, aiReq)
				loadedElapsed := time.Since(aiStart)
				require.Less(t, loadedElapsed, time.Second, "native AI request must not wait for Directory slots")
				require.Equal(t, http.StatusOK, aiResp.Code, aiResp.Body.String())
				require.Contains(t, aiResp.Body.String(), "resp_healthy")
				require.Equal(t, baselineHits, pressureUpstream.accountHits())
				require.Equal(t, int32(2), directoryRepo.activeCount(), "AI must finish before either Directory query exits")
				require.Empty(t, directoryResponses, "blocked Directory queries must not complete before release")
				t.Logf("mode=%s round=%d baseline_ai=%s loaded_ai=%s", mode, round, baselineElapsed, loadedElapsed)

				releaseDirectory()
				directoryWG.Wait()
				close(directoryResponses)
				for resp := range directoryResponses {
					responses = append(responses, resp)
				}
				require.Len(t, responses, 4)
				successes, rejected := 0, 0
				for _, resp := range responses {
					require.Equal(t, "no-store", resp.Header().Get("Cache-Control"))
					require.Contains(t, resp.Header().Get("Content-Type"), "application/json")
					switch resp.Code {
					case http.StatusOK:
						successes++
						var envelope struct {
							Accounts []map[string]json.RawMessage `json:"accounts"`
						}
						require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &envelope))
						require.Len(t, envelope.Accounts, 1)
						require.Equal(t, `9007199254740993`, string(envelope.Accounts[0]["id"]), "source numeric ID must remain exact JSON integer")
					case http.StatusTooManyRequests:
						rejected++
						var envelope struct {
							Code string `json:"code"`
						}
						require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &envelope))
						require.Contains(t, []string{"directory_busy", "directory_rate_limited"}, envelope.Code)
					default:
						t.Fatalf("unexpected Directory status %d: %s", resp.Code, resp.Body.String())
					}
				}
				require.Equal(t, 2, successes)
				require.Equal(t, 2, rejected)
				require.Equal(t, 2, directoryRepo.callCount(), "admission must cap repository work at two in-flight requests")
			}
		})
	}
}
