package routes

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/directory"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRegisterDirectoryRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	svc, err := directory.NewService(nil, &config.DirectoryConfig{Enabled: false})
	require.NoError(t, err)
	RegisterDirectoryRoutes(router, directory.NewHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/internal/v1/api-account-directory", nil)
	req.Header.Set("Authorization", "Bearer "+base64.RawURLEncoding.EncodeToString(bytes32(1)))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

func TestRegisterDirectoryRoutesRejectsUnsupportedMethods(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	svc, err := directory.NewService(nil, &config.DirectoryConfig{Enabled: false})
	require.NoError(t, err)
	RegisterDirectoryRoutes(router, directory.NewHandler(svc))

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
		req := httptest.NewRequest(method, "/internal/v1/api-account-directory", nil)
		req.Header.Set("Authorization", "Bearer "+base64.RawURLEncoding.EncodeToString(bytes32(1)))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusMethodNotAllowed, w.Code)
		require.JSONEq(t, `{"code":"directory_method_not_allowed","message":"method not allowed"}`, w.Body.String())
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	}
}

func bytes32(seed byte) []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = seed
	}
	return out
}
