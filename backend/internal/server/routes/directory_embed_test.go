//go:build embed

package routes

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/directory"
	"github.com/Wei-Shaw/sub2api/internal/web"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type embeddedDirectoryRepo struct{}

type embeddedSettings struct{}

func (embeddedSettings) GetPublicSettingsForInjection(context.Context) (any, error) {
	return map[string]any{}, nil
}

func (embeddedDirectoryRepo) List(context.Context) ([]directory.Row, error) {
	ids := []int64{9007199254740991, 9007199254740992, 9007199254740993, 9223372036854775807}
	rows := make([]directory.Row, 0, len(ids))
	for _, id := range ids {
		id, name, platform, kind, status := id, "embedded", "openai", "apikey", "active"
		rows = append(rows, directory.Row{GeneratedAt: time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC), ID: &id, Name: &name, Platform: &platform, Type: &kind, Status: &status})
	}
	return rows, nil
}

func TestEmbeddedDirectoryRouteMatrixBothFrontendEntrypoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, err := directory.NewService(embeddedDirectoryRepo{}, &config.DirectoryConfig{
		Enabled: true, CurrentToken: base64.RawURLEncoding.EncodeToString(make([]byte, 32)),
	})
	require.NoError(t, err)
	server, err := web.NewFrontendServer(embeddedSettings{})
	require.NoError(t, err)
	middlewares := []struct {
		name    string
		handler gin.HandlerFunc
	}{{"settings", server.Middleware()}, {"legacy", web.ServeEmbeddedFrontend()}}
	paths := []string{"/api/ping", "/v1/ping", "/v1beta/ping", "/backend-api/ping", "/antigravity/ping", "/setup/ping", "/health", "/models", "/responses", "/responses/compact", "/alpha/search", "/images/test", "/videos/test"}
	valid := "Bearer " + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	for _, entry := range middlewares {
		t.Run(entry.name, func(t *testing.T) {
			frontend := entry.handler
			newRouter := func() *gin.Engine {
				r := gin.New()
				r.Use(frontend)
				RegisterDirectoryRoutes(r, directory.NewHandler(svc))
				for _, path := range paths {
					r.GET(path, func(c *gin.Context) { c.String(http.StatusOK, "backend") })
				}
				return r
			}
			do := func(method, path, token string) *httptest.ResponseRecorder {
				r := newRouter()
				req := httptest.NewRequest(method, path, nil)
				if token != "" {
					req.Header.Set("Authorization", token)
				}
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				return w
			}
			ok := do(http.MethodGet, "/internal/v1/api-account-directory", valid)
			require.Equal(t, http.StatusOK, ok.Code)
			require.Contains(t, ok.Header().Get("Content-Type"), "application/json")
			require.Equal(t, "no-store", ok.Header().Get("Cache-Control"))
			var envelope struct {
				SchemaVersion int `json:"schema_version"`
				Accounts      []struct {
					ID json.RawMessage `json:"id"`
				} `json:"accounts"`
			}
			require.NoError(t, json.Unmarshal(ok.Body.Bytes(), &envelope))
			require.Equal(t, 1, envelope.SchemaVersion)
			require.Len(t, envelope.Accounts, 4)
			for i, id := range []string{"9007199254740991", "9007199254740992", "9007199254740993", "9223372036854775807"} {
				require.Equal(t, id, string(envelope.Accounts[i].ID))
			}
			for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
				w := do(method, "/internal/v1/api-account-directory", valid)
				require.Equal(t, http.StatusMethodNotAllowed, w.Code)
				require.JSONEq(t, `{"code":"directory_method_not_allowed","message":"method not allowed"}`, w.Body.String())
				require.Contains(t, w.Header().Get("Content-Type"), "application/json")
				require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			}
			for _, token := range []string{"", "Bearer invalid"} {
				w := do(http.MethodGet, "/internal/v1/api-account-directory", token)
				require.Equal(t, http.StatusUnauthorized, w.Code)
				require.JSONEq(t, `{"code":"directory_unauthorized","message":"Authorization header is required"}`, w.Body.String())
				require.Contains(t, w.Header().Get("Content-Type"), "application/json")
				require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			}
			disabled, err := directory.NewService(nil, &config.DirectoryConfig{Enabled: false})
			require.NoError(t, err)
			disabledRouter := gin.New()
			disabledRouter.Use(frontend)
			RegisterDirectoryRoutes(disabledRouter, directory.NewHandler(disabled))
			disabledReq := httptest.NewRequest(http.MethodGet, "/internal/v1/api-account-directory", nil)
			disabledReq.Header.Set("Authorization", valid)
			disabledResp := httptest.NewRecorder()
			disabledRouter.ServeHTTP(disabledResp, disabledReq)
			require.Equal(t, http.StatusNotFound, disabledResp.Code)
			require.JSONEq(t, `{"code":"directory_disabled","message":"Directory is disabled"}`, disabledResp.Body.String())
			require.Contains(t, disabledResp.Header().Get("Content-Type"), "application/json")
			require.Equal(t, "no-store", disabledResp.Header().Get("Cache-Control"))
			unknown := do(http.MethodGet, "/internal/unknown", valid)
			require.Equal(t, http.StatusNotFound, unknown.Code)
			require.NotContains(t, unknown.Body.String(), "<!doctype html")
			spa := do(http.MethodGet, "/dashboard", "")
			require.Equal(t, http.StatusOK, spa.Code)
			require.Contains(t, spa.Header().Get("Content-Type"), "text/html")
			for _, path := range paths {
				w := do(http.MethodGet, path, "")
				require.Equal(t, http.StatusOK, w.Code)
				require.Equal(t, "backend", w.Body.String())
			}
		})
	}
}
