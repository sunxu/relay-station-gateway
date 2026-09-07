//go:build unit

package service

import (
	"context"
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

// directoryIsolationRepo holds the two Directory execution slots until the
// request context is cancelled. This is an admission/isolation fixture, not a
// database or throughput benchmark.
type directoryIsolationRepo struct {
	started atomic.Int32
	active  atomic.Int32
	entered chan struct{}
}

func (r *directoryIsolationRepo) List(ctx context.Context) ([]directory.Row, error) {
	r.started.Add(1)
	r.active.Add(1)
	defer r.active.Add(-1)
	select {
	case r.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func runDirectoryIsolationBurst(t *testing.T, fn func(*testing.T)) {
	t.Helper()
	repo := &directoryIsolationRepo{entered: make(chan struct{}, 2)}
	token := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	svc, err := directory.NewService(repo, &config.DirectoryConfig{
		Enabled: true, CurrentToken: token,
	})
	require.NoError(t, err)
	r := gin.New()
	r.Any("/internal/v1/api-account-directory", directory.NewHandler(svc).Get)
	h := httptest.NewServer(r)
	defer h.Close()

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	defer func() { cancel(); wg.Wait() }()
	client := &http.Client{Timeout: 3 * time.Second}
	request := func() int {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, h.URL+"/internal/v1/api-account-directory", nil)
		if reqErr != nil {
			return 0
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, doErr := client.Do(req)
		if doErr != nil {
			return 0
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	statuses := make(chan int, 3)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses <- request()
		}()
	}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for i := 0; i < 2; i++ {
		select {
		case <-repo.entered:
		case <-deadline.C:
			t.Fatal("Directory did not occupy both execution slots")
		}
	}
	require.Equal(t, int32(2), repo.started.Load())
	require.Equal(t, int32(2), repo.active.Load())
	// Send the third request only after both service slots are occupied. It
	// must be rejected immediately by the existing non-blocking admission path.
	wg.Add(1)
	go func() {
		defer wg.Done()
		statuses <- request()
	}()
	select {
	case status := <-statuses:
		require.Equal(t, http.StatusTooManyRequests, status)
	case <-time.After(time.Second):
		t.Fatal("Directory third request did not return bounded admission result")
	}
	t.Run("native", fn)
	require.Equal(t, int32(2), repo.started.Load(), "rejected Directory request must not reach the repository")
	require.Equal(t, int32(2), repo.active.Load(), "Directory slots must remain occupied through native behavior")
	cancel()
	wg.Wait()
	close(statuses)
	for range statuses {
		// The two occupied requests are expected to finish after cancellation.
	}
}

func TestDirectoryBurstPreservesNativeGatewayBehavior(t *testing.T) {
	cases := []struct {
		name string
		fn   func(*testing.T)
	}{
		{"scheduler_priority", TestGatewayService_SelectAccountForModelWithPlatform_PriorityAndLastUsed},
		{"sticky_selection", TestSelectAccountWithLoadAwareness_StickyReadReuse},
		{"retry_429", TestGatewayCompatPoolMode429AllowsSameAccountRetry},
		{"health_breaker_default", TestOpenAIAPIKeyHealthBreakerDefaultDisabled},
		{"health_breaker_persisted", TestOpenAIAPIKeyHealthBreakerTripsPersistedAndRuntimeState},
		{"guardian_affinity", TestOpenAIGatewayService_GuardianParentAffinitySelectsParentAccountAcrossSchedulers},
		{"upstream_drain", TestStreamUpstreamResponse_ClientDisconnectDrainsUsage},
	}
	for _, tc := range cases {
		t.Run(tc.name+"_baseline", tc.fn)
		t.Run(tc.name+"_under_directory_burst", func(t *testing.T) {
			runDirectoryIsolationBurst(t, tc.fn)
		})
	}
}
