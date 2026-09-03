package directory

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestRepositoryList(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, 9, 3, 2, 1, 2, 0, time.UTC)
	mock.ExpectQuery(projectionSQL).
		WillReturnRows(sqlmock.NewRows([]string{"generated_at", "id", "name", "platform", "type", "url_source", "status"}).
			AddRow(now, int64(7), "Name", "openai", "apikey", "https://example.com/path?x=1", "active"))

	rows, err := NewRepository(db).List(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].ID)
	require.Equal(t, int64(7), *rows[0].ID)
	require.Equal(t, "Name", deref(rows[0].Name))
	require.Equal(t, "openai", deref(rows[0].Platform))
	require.Equal(t, "apikey", deref(rows[0].Type))
	require.Equal(t, "https://example.com/path?x=1", deref(rows[0].URLSource))
	require.Equal(t, "active", deref(rows[0].Status))
	require.True(t, rows[0].GeneratedAt.Equal(now))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProjectionSQLBoundsCredentialsBaseURL(t *testing.T) {
	require.Contains(t, projectionSQL, "octet_length(credentials ->> 'base_url') <= 4096")
	require.Contains(t, projectionSQL, "credentials ->> 'base_url'")
	require.NotContains(t, projectionSQL, "credentials,")
	require.NotContains(t, projectionSQL, "extra")
	require.NotContains(t, projectionSQL, "SELECT *")
	require.True(t, strings.Contains(projectionSQL, "LIMIT 10001"))
}

func TestProjectionSQLSafetyStatic(t *testing.T) {
	sql := strings.ToLower(projectionSQL)
	for _, bad := range []string{" insert ", " update ", " delete ", " merge ", " create ", " alter ", " drop ", " for update ", "pg_advisory", ";"} {
		require.NotContains(t, sql, bad)
	}
}

func TestRepositoryListOverflow(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	rows := sqlmock.NewRows([]string{"generated_at", "id", "name", "platform", "type", "url_source", "status"})
	now := time.Now().UTC()
	for i := 1; i <= 10001; i++ {
		rows.AddRow(now, int64(i), "n", "openai", "apikey", nil, "active")
	}
	mock.ExpectQuery(projectionSQL).WillReturnRows(rows)

	_, err = NewRepository(db).List(context.Background())
	require.ErrorIs(t, err, ErrAccountLimitExceeded)
	require.NoError(t, mock.ExpectationsWereMet())
}

func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}
