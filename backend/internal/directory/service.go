package directory

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
)

const (
	maxResponseBytes      = 4 * 1024 * 1024
	maxAuthorizationBytes = 1024
	queryTimeout          = 2 * time.Second
	totalTimeout          = 3 * time.Second
	concurrencyLimit      = 2
	refillPerMinute       = 10
	rateBucketCapacity    = 2
)

type Service struct {
	repo              rowLister
	enabled           bool
	currentTokenHash  [32]byte
	previousTokenHash [32]byte
	hasPreviousToken  bool
	rate              *tokenBucket
	semaphore         chan struct{}
	rotation          time.Duration
	startedAt         time.Time
	now               func() time.Time
}

type rowLister interface {
	List(context.Context) ([]Row, error)
}

type response struct {
	SchemaVersion int           `json:"schema_version"`
	GeneratedAt   string        `json:"generated_at"`
	Accounts      []accountItem `json:"accounts"`
}

type accountItem struct {
	ID       int64   `json:"id"`
	Name     string  `json:"name"`
	Platform string  `json:"platform"`
	Type     string  `json:"type"`
	URL      *string `json:"url"`
	Status   string  `json:"status"`
}

func NewService(repo rowLister, cfg *config.DirectoryConfig) (*Service, error) {
	if cfg == nil {
		cfg = &config.DirectoryConfig{}
	}
	if cfg.Enabled && repo == nil {
		return nil, errors.New("directory repository is required when enabled")
	}
	svc := &Service{
		repo:      repo,
		enabled:   cfg.Enabled,
		rate:      newTokenBucket(rateBucketCapacity, refillPerMinute),
		semaphore: make(chan struct{}, concurrencyLimit),
		startedAt: time.Now(),
		now:       time.Now,
	}
	if !cfg.Enabled {
		return svc, nil
	}
	current, err := decodeConfiguredTokenHash(cfg.CurrentToken)
	if err != nil {
		return nil, fmt.Errorf("directory.current_token: %w", err)
	}
	svc.currentTokenHash = current
	svc.rotation = time.Duration(cfg.RotationWindowSeconds) * time.Second
	if strings.TrimSpace(cfg.PreviousToken) != "" {
		if cfg.RotationWindowSeconds <= 0 {
			return nil, errors.New("directory.rotation_window_seconds must be positive when previous_token is configured")
		}
		previous, err := decodeConfiguredTokenHash(cfg.PreviousToken)
		if err != nil {
			return nil, fmt.Errorf("directory.previous_token: %w", err)
		}
		svc.previousTokenHash = previous
		svc.hasPreviousToken = true
	}
	return svc, nil
}

func (s *Service) Serve(c *gin.Context) {
	if c == nil {
		return
	}
	c.Header("Cache-Control", "no-store")
	if c.Request.Method != http.MethodGet {
		writeError(c, http.StatusMethodNotAllowed, "directory_method_not_allowed", "method not allowed")
		return
	}
	if !s.enabled {
		writeError(c, http.StatusNotFound, "directory_disabled", "Directory is disabled")
		return
	}
	rawAuth := c.GetHeader("Authorization")
	if len(rawAuth) > maxAuthorizationBytes {
		writeError(c, http.StatusUnauthorized, "directory_unauthorized", "Authorization header is required")
		return
	}
	token, ok := extractBearerToken(rawAuth)
	if !ok {
		writeError(c, http.StatusUnauthorized, "directory_unauthorized", "Authorization header is required")
		return
	}
	if !s.validateToken(token) {
		writeError(c, http.StatusUnauthorized, "directory_unauthorized", "Authorization header is required")
		return
	}

	admissionStart := s.now()
	totalDeadline := admissionStart.Add(totalTimeout)
	if !s.rate.Allow(admissionStart) {
		writeError(c, http.StatusTooManyRequests, "directory_rate_limited", "Directory rate limit exceeded")
		return
	}
	if !s.tryAcquire() {
		writeError(c, http.StatusTooManyRequests, "directory_busy", "Directory is busy")
		return
	}
	defer s.release()

	queryStart := s.now()
	queryDeadline := queryStart.Add(queryTimeout)
	if totalDeadline.Before(queryDeadline) {
		queryDeadline = totalDeadline
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), queryDeadline)
	defer cancel()

	rows, err := s.repo.List(ctx)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			if s.now().After(totalDeadline) {
				writeError(c, http.StatusServiceUnavailable, "directory_timeout", "Directory timed out")
			} else {
				writeError(c, http.StatusServiceUnavailable, "directory_query_timeout", "Directory query timed out")
			}
			return
		}
		if errors.Is(err, ErrAccountLimitExceeded) {
			writeError(c, http.StatusRequestEntityTooLarge, "directory_account_limit_exceeded", "Directory account limit exceeded")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "directory_unavailable", "Directory is unavailable")
		return
	}
	if s.now().After(totalDeadline) {
		writeError(c, http.StatusServiceUnavailable, "directory_timeout", "Directory timed out")
		return
	}

	result, err := buildResponse(rows)
	if err != nil {
		switch {
		case errors.Is(err, ErrAccountLimitExceeded):
			writeError(c, http.StatusRequestEntityTooLarge, "directory_account_limit_exceeded", "Directory account limit exceeded")
		case errors.Is(err, ErrSnapshotInvalid):
			writeError(c, http.StatusInternalServerError, "directory_snapshot_invalid", "Directory snapshot invalid")
		default:
			writeError(c, http.StatusInternalServerError, "directory_snapshot_invalid", "Directory snapshot invalid")
		}
		return
	}
	if s.now().After(totalDeadline) {
		writeError(c, http.StatusServiceUnavailable, "directory_timeout", "Directory timed out")
		return
	}

	buf, err := encodeDirectoryResponse(result)
	if err != nil {
		if errors.Is(err, errDirectoryResponseTooLarge) {
			writeError(c, http.StatusRequestEntityTooLarge, "directory_response_limit_exceeded", "Directory response too large")
			return
		}
		writeError(c, http.StatusInternalServerError, "directory_snapshot_invalid", "Directory snapshot invalid")
		return
	}
	if s.now().After(totalDeadline) {
		writeError(c, http.StatusServiceUnavailable, "directory_timeout", "Directory timed out")
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", buf)
}

func buildResponse(rows []Row) (response, error) {
	if len(rows) == 0 {
		return response{}, ErrSnapshotInvalid
	}
	result := response{
		SchemaVersion: 1,
		GeneratedAt:   rows[0].GeneratedAt.UTC().Format(time.RFC3339),
	}
	if rows[0].GeneratedAt.IsZero() {
		return response{}, ErrSnapshotInvalid
	}
	for i, row := range rows {
		if row.ID == nil {
			if len(rows) == 1 && i == 0 {
				result.Accounts = []accountItem{}
				return result, nil
			}
			return response{}, ErrSnapshotInvalid
		}
		if *row.ID <= 0 {
			return response{}, ErrSnapshotInvalid
		}
		if i > 0 && *rows[i-1].ID >= *row.ID {
			return response{}, ErrSnapshotInvalid
		}
		if row.Name == nil || *row.Name == "" || row.Platform == nil || *row.Platform == "" || row.Type == nil || *row.Type == "" || row.Status == nil || *row.Status == "" {
			return response{}, ErrSnapshotInvalid
		}
		switch *row.Type {
		case "apikey", "upstream":
		default:
			return response{}, ErrSnapshotInvalid
		}
		item := accountItem{
			ID:       *row.ID,
			Name:     *row.Name,
			Platform: *row.Platform,
			Type:     *row.Type,
			URL:      sanitizeOriginPtr(row.URLSource),
			Status:   *row.Status,
		}
		result.Accounts = append(result.Accounts, item)
	}
	return result, nil
}

func sanitizeOriginPtr(raw *string) *string {
	if raw == nil {
		return nil
	}
	if value := sanitizeOrigin(*raw); value != "" {
		return &value
	}
	return nil
}

func sanitizeOrigin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return ""
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		return ""
	}
	port := strings.TrimSpace(parsed.Port())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		return strings.ToLower(parsed.Scheme) + "://" + host + ":" + port
	}
	return strings.ToLower(parsed.Scheme) + "://" + host
}

func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{
		"code":    code,
		"message": message,
	})
}

var errDirectoryResponseTooLarge = errors.New("directory response too large")

func encodeDirectoryResponse(v response) ([]byte, error) {
	buf := make([]byte, 0, 1024)
	appendString := func(s string) error {
		if len(buf)+len(s) > maxResponseBytes {
			return errDirectoryResponseTooLarge
		}
		buf = append(buf, s...)
		return nil
	}
	appendQuoted := func(s string) error {
		raw, err := json.Marshal(s)
		if err != nil {
			return err
		}
		return appendString(string(raw))
	}

	if err := appendString(`{"schema_version":`); err != nil {
		return nil, err
	}
	if err := appendString(strconv.Itoa(v.SchemaVersion)); err != nil {
		return nil, err
	}
	if err := appendString(`,"generated_at":`); err != nil {
		return nil, err
	}
	if err := appendQuoted(v.GeneratedAt); err != nil {
		return nil, err
	}
	if err := appendString(`,"accounts":[`); err != nil {
		return nil, err
	}
	for i, account := range v.Accounts {
		if i > 0 {
			if err := appendString(","); err != nil {
				return nil, err
			}
		}
		if err := appendString(`{"id":`); err != nil {
			return nil, err
		}
		if err := appendString(strconv.FormatInt(account.ID, 10)); err != nil {
			return nil, err
		}
		if err := appendString(`,"name":`); err != nil {
			return nil, err
		}
		if err := appendQuoted(account.Name); err != nil {
			return nil, err
		}
		if err := appendString(`,"platform":`); err != nil {
			return nil, err
		}
		if err := appendQuoted(account.Platform); err != nil {
			return nil, err
		}
		if err := appendString(`,"type":`); err != nil {
			return nil, err
		}
		if err := appendQuoted(account.Type); err != nil {
			return nil, err
		}
		if err := appendString(`,"status":`); err != nil {
			return nil, err
		}
		if err := appendQuoted(account.Status); err != nil {
			return nil, err
		}
		if err := appendString(`,"url":`); err != nil {
			return nil, err
		}
		if account.URL == nil {
			if err := appendString("null"); err != nil {
				return nil, err
			}
		} else if err := appendQuoted(*account.URL); err != nil {
			return nil, err
		}
		if err := appendString("}"); err != nil {
			return nil, err
		}
	}
	if err := appendString(`]}`); err != nil {
		return nil, err
	}
	return buf, nil
}

func extractBearerToken(header string) (string, bool) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", false
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	token := strings.TrimSpace(parts[1])
	return token, token != ""
}

func decodeConfiguredTokenHash(raw string) ([32]byte, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return [32]byte{}, err
	}
	if len(decoded) < 32 {
		return [32]byte{}, fmt.Errorf("decoded token must be at least 32 bytes")
	}
	return sha256.Sum256(decoded), nil
}

func (s *Service) validateToken(raw string) bool {
	decoded, err := decodeConfiguredTokenHash(raw)
	if err != nil {
		return false
	}
	if subtle.ConstantTimeCompare(decoded[:], s.currentTokenHash[:]) == 1 {
		return true
	}
	if !s.hasPreviousToken {
		return false
	}
	if s.rotation <= 0 {
		return false
	}
	if s.now().Sub(s.startedAt) > s.rotation {
		return false
	}
	return subtle.ConstantTimeCompare(decoded[:], s.previousTokenHash[:]) == 1
}

func (s *Service) tryAcquire() bool {
	select {
	case s.semaphore <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Service) release() {
	select {
	case <-s.semaphore:
	default:
	}
}

type tokenBucket struct {
	mu       sync.Mutex
	capacity float64
	tokens   float64
	rate     float64
	last     time.Time
}

func newTokenBucket(capacity, refillPerMinute int) *tokenBucket {
	return &tokenBucket{
		capacity: float64(capacity),
		tokens:   float64(capacity),
		rate:     float64(refillPerMinute) / 60.0,
	}
}

func (b *tokenBucket) Allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.last.IsZero() {
		b.last = now
	}
	if now.Before(b.last) {
		b.last = now
	}
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * b.rate
		if b.tokens > b.capacity {
			b.tokens = b.capacity
		}
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
