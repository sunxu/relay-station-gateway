package directory

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type fakeRepo struct {
	rows     []Row
	err      error
	deadline chan time.Time
	calls    int
}

func (r *fakeRepo) List(ctx context.Context) ([]Row, error) {
	r.calls++
	if r.deadline != nil {
		if dl, ok := ctx.Deadline(); ok {
			r.deadline <- dl
		}
	}
	return r.rows, r.err
}

func TestServiceServeSuccess(t *testing.T) {
	svc := newTestService(t, &fakeRepo{rows: []Row{{
		GeneratedAt: time.Date(2026, 9, 3, 2, 1, 2, 0, time.UTC),
		ID:          ptr[int64](11),
		Name:        ptr("alpha"),
		Platform:    ptr("openai-future"),
		Type:        ptr("apikey"),
		URLSource:   ptr("HTTPS://User:Secret@Example.COM:8080/path?q=1#frag"),
		Status:      ptr("active-future"),
	}}})

	w := performRequest(t, svc, validAuthHeader())
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.NotContains(t, w.Body.String(), "Secret")
	require.NotContains(t, w.Body.String(), "/path")

	var got response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, 1, got.SchemaVersion)
	require.Equal(t, "2026-09-03T02:01:02Z", got.GeneratedAt)
	require.Len(t, got.Accounts, 1)
	require.Equal(t, int64(11), got.Accounts[0].ID)
	require.Equal(t, "alpha", got.Accounts[0].Name)
	require.Equal(t, "openai-future", got.Accounts[0].Platform)
	require.Equal(t, "apikey", got.Accounts[0].Type)
	require.Equal(t, "https://example.com:8080", deref(got.Accounts[0].URL))
	require.Equal(t, "active-future", got.Accounts[0].Status)
}

func TestServiceServeRejectsInvalidTypeInvariant(t *testing.T) {
	svc := newTestService(t, &fakeRepo{rows: []Row{{
		GeneratedAt: time.Now().UTC(),
		ID:          ptr[int64](1),
		Name:        ptr("n"),
		Platform:    ptr("openai"),
		Type:        ptr("future-type"),
		Status:      ptr("active"),
	}}})

	w := performRequest(t, svc, validAuthHeader())
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.JSONEq(t, `{"code":"directory_snapshot_invalid","message":"Directory snapshot invalid"}`, w.Body.String())
}

func TestServiceServeRejectsUnsupportedMethods(t *testing.T) {
	repo := &fakeRepo{rows: []Row{{
		GeneratedAt: time.Now().UTC(),
		ID:          ptr[int64](1),
		Name:        ptr("n"),
		Platform:    ptr("openai"),
		Type:        ptr("apikey"),
		Status:      ptr("active"),
	}}}
	svc := newTestService(t, repo)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
		w := performRequestWithMethod(t, svc, method, validAuthHeader())
		require.Equal(t, http.StatusMethodNotAllowed, w.Code)
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		require.JSONEq(t, `{"code":"directory_method_not_allowed","message":"method not allowed"}`, w.Body.String())
	}
	require.Zero(t, repo.calls)
}

func TestServiceServeRejectsMalformedToken(t *testing.T) {
	svc := newTestService(t, &fakeRepo{})
	w := performRequest(t, svc, "Bearer invalid!token")
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.JSONEq(t, `{"code":"directory_unauthorized","message":"Authorization header is required"}`, w.Body.String())
}

func TestAuthorizationHeaderLengthLimit(t *testing.T) {
	repo := &fakeRepo{}
	svc := newTestService(t, repo)
	w := performRequest(t, svc, "Bearer "+strings.Repeat("A", maxAuthorizationBytes))
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.JSONEq(t, `{"code":"directory_unauthorized","message":"Authorization header is required"}`, w.Body.String())
	require.Zero(t, repo.calls)
}

func TestAuthorizationHeaderWhitespaceLengthLimit(t *testing.T) {
	repo := &fakeRepo{}
	svc := newTestService(t, repo)
	w := performRequest(t, svc, strings.Repeat(" ", maxAuthorizationBytes+1))
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.JSONEq(t, `{"code":"directory_unauthorized","message":"Authorization header is required"}`, w.Body.String())
	require.Zero(t, repo.calls)
}

func TestConfiguredTokenBounds(t *testing.T) {
	_, err := NewService(&fakeRepo{}, &config.DirectoryConfig{
		Enabled:      true,
		CurrentToken: base64.RawURLEncoding.EncodeToString(bytes32(1)[:31]),
	})
	require.Error(t, err)

	svc, err := NewService(&fakeRepo{}, &config.DirectoryConfig{
		Enabled:      true,
		CurrentToken: base64.RawURLEncoding.EncodeToString(bytes32(1)),
	})
	require.NoError(t, err)
	require.True(t, svc.validateToken(base64.RawURLEncoding.EncodeToString(bytes32(1))))

	_, err = NewService(&fakeRepo{}, &config.DirectoryConfig{
		Enabled:      true,
		CurrentToken: base64.URLEncoding.EncodeToString(bytes32(1)),
	})
	require.Error(t, err)

	_, err = NewService(&fakeRepo{}, &config.DirectoryConfig{
		Enabled:      true,
		CurrentToken: "not-base64",
	})
	require.Error(t, err)
}

func TestServiceTokenRotationWindow(t *testing.T) {
	current := bytes32(1)
	previous := bytes32(2)
	svc, err := NewService(&fakeRepo{}, &config.DirectoryConfig{
		Enabled:               true,
		CurrentToken:          base64.RawURLEncoding.EncodeToString(current),
		PreviousToken:         base64.RawURLEncoding.EncodeToString(previous),
		RotationWindowSeconds: 3,
	})
	require.NoError(t, err)
	t0 := time.Now()
	svc.startedAt = t0
	svc.now = func() time.Time { return t0 }

	require.True(t, svc.validateToken(base64.RawURLEncoding.EncodeToString(current)))
	require.True(t, svc.validateToken(base64.RawURLEncoding.EncodeToString(previous)))

	svc.now = func() time.Time { return t0.Add(4 * time.Second) }
	require.False(t, svc.validateToken(base64.RawURLEncoding.EncodeToString(previous)))
}

func TestServiceServeRateLimits(t *testing.T) {
	svc := newTestService(t, &fakeRepo{rows: []Row{{
		GeneratedAt: time.Now().UTC(),
		ID:          ptr[int64](1),
		Name:        ptr("n"),
		Platform:    ptr("openai"),
		Type:        ptr("apikey"),
		Status:      ptr("active"),
	}}})

	require.Equal(t, http.StatusOK, performRequest(t, svc, validAuthHeader()).Code)
	require.Equal(t, http.StatusOK, performRequest(t, svc, validAuthHeader()).Code)
	w := performRequest(t, svc, validAuthHeader())
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.Contains(t, w.Body.String(), "directory_rate_limited")
}

func TestTokenBucketRefill(t *testing.T) {
	bucket := newTokenBucket(rateBucketCapacity, refillPerMinute)
	t0 := time.Unix(0, 0)

	require.True(t, bucket.Allow(t0))
	require.True(t, bucket.Allow(t0))
	require.False(t, bucket.Allow(t0))
	require.True(t, bucket.Allow(t0.Add(6*time.Second)))
	require.False(t, bucket.Allow(t0.Add(6*time.Second)))
	require.True(t, bucket.Allow(t0.Add(12*time.Second)))
	require.False(t, bucket.Allow(t0.Add(12*time.Second)))
	require.True(t, bucket.Allow(t0.Add(24*time.Second)))
	require.True(t, bucket.Allow(t0.Add(24*time.Second)))
	require.False(t, bucket.Allow(t0.Add(24*time.Second)))
}

func TestServiceServeRejectsBusyRequests(t *testing.T) {
	svc := newTestService(t, &fakeRepo{rows: []Row{{
		GeneratedAt: time.Now().UTC(),
		ID:          ptr[int64](1),
		Name:        ptr("n"),
		Platform:    ptr("openai"),
		Type:        ptr("apikey"),
		Status:      ptr("active"),
	}}})
	svc.semaphore <- struct{}{}
	svc.semaphore <- struct{}{}

	w := performRequest(t, svc, validAuthHeader())
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.Contains(t, w.Body.String(), "directory_busy")
}

func TestServiceServeMapsQueryDeadline(t *testing.T) {
	deadlineCh := make(chan time.Time, 1)
	svc := newTestService(t, &fakeRepo{err: context.DeadlineExceeded, deadline: deadlineCh})
	t0 := time.Now()
	calls := 0
	svc.now = func() time.Time {
		calls++
		if calls == 1 {
			return t0
		}
		return t0.Add(500 * time.Millisecond)
	}

	w := performRequest(t, svc, validAuthHeader())
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Contains(t, w.Body.String(), "directory_query_timeout")
	select {
	case dl := <-deadlineCh:
		require.WithinDuration(t, t0.Add(2500*time.Millisecond), dl, time.Millisecond)
	default:
		t.Fatal("expected query deadline to be set")
	}
}

func TestServiceServeTimesOutAfterQuery(t *testing.T) {
	svc := newTestService(t, &fakeRepo{rows: []Row{{
		GeneratedAt: time.Now().UTC(),
		ID:          ptr[int64](1),
		Name:        ptr("n"),
		Platform:    ptr("openai"),
		Type:        ptr("apikey"),
		Status:      ptr("active"),
	}}})
	t0 := time.Now()
	calls := 0
	svc.now = func() time.Time {
		calls++
		if calls >= 2 {
			return t0.Add(4 * time.Second)
		}
		return t0
	}

	w := performRequest(t, svc, validAuthHeader())
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Contains(t, w.Body.String(), "directory_timeout")
}

func TestServiceServeTimesOutAfterEncodeBeforeWrite(t *testing.T) {
	svc := newTestService(t, &fakeRepo{rows: []Row{{
		GeneratedAt: time.Now().UTC(),
		ID:          ptr[int64](1),
		Name:        ptr("n"),
		Platform:    ptr("openai"),
		Type:        ptr("apikey"),
		Status:      ptr("active"),
	}}})
	t0 := time.Now()
	calls := 0
	svc.now = func() time.Time {
		calls++
		switch calls {
		case 1, 2, 3, 4:
			return t0
		default:
			return t0.Add(4 * time.Second)
		}
	}

	w := performRequest(t, svc, validAuthHeader())
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Contains(t, w.Body.String(), "directory_timeout")
	require.NotContains(t, w.Body.String(), "\"schema_version\":1")
}

func TestServiceServeEmptyDirectoryContract(t *testing.T) {
	svc := newTestService(t, &fakeRepo{rows: []Row{{
		GeneratedAt: time.Date(2026, 9, 3, 2, 1, 2, 0, time.UTC),
		ID:          nil,
	}}})

	w := performRequest(t, svc, validAuthHeader())
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var got response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, 1, got.SchemaVersion)
	require.Equal(t, "2026-09-03T02:01:02Z", got.GeneratedAt)
	require.Empty(t, got.Accounts)
}

func TestServiceServeRejectsEmptySnapshot(t *testing.T) {
	svc := newTestService(t, &fakeRepo{rows: []Row{}})

	w := performRequest(t, svc, validAuthHeader())
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.JSONEq(t, `{"code":"directory_snapshot_invalid","message":"Directory snapshot invalid"}`, w.Body.String())
}

func TestServiceServeRejectsNonIncreasingIDs(t *testing.T) {
	svc := newTestService(t, &fakeRepo{rows: []Row{
		{GeneratedAt: time.Now().UTC(), ID: ptr[int64](2), Name: ptr("a"), Platform: ptr("openai"), Type: ptr("apikey"), Status: ptr("active")},
		{GeneratedAt: time.Now().UTC(), ID: ptr[int64](1), Name: ptr("b"), Platform: ptr("openai"), Type: ptr("apikey"), Status: ptr("active")},
	}})

	w := performRequest(t, svc, validAuthHeader())
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.JSONEq(t, `{"code":"directory_snapshot_invalid","message":"Directory snapshot invalid"}`, w.Body.String())
}

func TestServiceServeRejectsOversizedResponse(t *testing.T) {
	svc := newTestService(t, &fakeRepo{rows: []Row{{
		GeneratedAt: time.Now().UTC(),
		ID:          ptr[int64](1),
		Name:        ptr(strings.Repeat("a", maxResponseBytes)),
		Platform:    ptr("openai"),
		Type:        ptr("apikey"),
		Status:      ptr("active"),
	}}})

	w := performRequest(t, svc, validAuthHeader())
	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	require.JSONEq(t, `{"code":"directory_response_limit_exceeded","message":"Directory response too large"}`, w.Body.String())
}

func TestServiceServeDisabled(t *testing.T) {
	svc, err := NewService(nil, &config.DirectoryConfig{Enabled: false})
	require.NoError(t, err)

	w := performRequest(t, svc, validAuthHeader())
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

func TestDirectoryConfigSerializationOmitsTokens(t *testing.T) {
	cfg := config.DirectoryConfig{
		Enabled:               true,
		CurrentToken:          "current",
		PreviousToken:         "previous",
		RotationWindowSeconds: 60,
	}

	jsonBytes, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NotContains(t, string(jsonBytes), "current")
	require.NotContains(t, string(jsonBytes), "previous")

	yamlBytes, err := yaml.Marshal(cfg)
	require.NoError(t, err)
	require.NotContains(t, string(yamlBytes), "current")
	require.NotContains(t, string(yamlBytes), "previous")
}

func TestEncodeDirectoryResponseBoundary(t *testing.T) {
	base := response{
		SchemaVersion: 1,
		GeneratedAt:   "2026-09-03T02:01:02Z",
		Accounts: []accountItem{{
			ID:       1,
			Name:     "",
			Platform: "openai",
			Type:     "apikey",
			Status:   "active",
		}},
	}

	fitNameLen := func(n int) bool {
		fit := base
		fit.Accounts[0].Name = strings.Repeat("a", n)
		_, err := encodeDirectoryResponse(fit)
		return err == nil
	}

	low, high := 0, maxResponseBytes
	for low < high {
		mid := (low + high + 1) / 2
		if fitNameLen(mid) {
			low = mid
			continue
		}
		high = mid - 1
	}

	fit := base
	fit.Accounts[0].Name = strings.Repeat("a", low)
	fitBytes, err := encodeDirectoryResponse(fit)
	require.NoError(t, err)
	require.LessOrEqual(t, len(fitBytes), maxResponseBytes)

	fit.Accounts[0].Name = strings.Repeat("a", low+1)
	_, err = encodeDirectoryResponse(fit)
	require.ErrorIs(t, err, errDirectoryResponseTooLarge)
}

func newTestService(t *testing.T, repo rowLister) *Service {
	t.Helper()
	svc, err := NewService(repo, &config.DirectoryConfig{
		Enabled:      true,
		CurrentToken: base64.RawURLEncoding.EncodeToString(bytes32(1)),
	})
	require.NoError(t, err)
	return svc
}

func performRequest(t *testing.T, svc *Service, authHeader string) *httptest.ResponseRecorder {
	return performRequestWithMethod(t, svc, http.MethodGet, authHeader)
}

func performRequestWithMethod(t *testing.T, svc *Service, method, authHeader string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Any("/internal/v1/api-account-directory", NewHandler(svc).Get)
	req := httptest.NewRequest(method, "/internal/v1/api-account-directory", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func validAuthHeader() string {
	return "Bearer " + base64.RawURLEncoding.EncodeToString(bytes32(1))
}

func bytes32(seed byte) []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = seed
	}
	return out
}

func ptr[T any](v T) *T {
	return &v
}
