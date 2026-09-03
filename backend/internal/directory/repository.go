package directory

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const projectionSQL = `
WITH snapshot AS (
	SELECT transaction_timestamp() AS generated_at
),
directory AS (
	SELECT
		id,
		name,
		platform,
		type,
		CASE
			WHEN jsonb_typeof(credentials -> 'base_url') = 'string' THEN
				CASE
					WHEN octet_length(credentials ->> 'base_url') <= 4096 THEN credentials ->> 'base_url'
					ELSE NULL
				END
			ELSE NULL
		END AS url_source,
		status
	FROM accounts
	WHERE deleted_at IS NULL
		AND type IN ('apikey', 'upstream')
	ORDER BY id ASC
	LIMIT 10001
)
SELECT snapshot.generated_at, directory.id, directory.name, directory.platform, directory.type, directory.url_source, directory.status
FROM snapshot
LEFT JOIN directory ON TRUE
ORDER BY directory.id ASC NULLS LAST`

const maxAccounts = 10000

var (
	ErrSnapshotInvalid      = errors.New("directory snapshot invalid")
	ErrAccountLimitExceeded = errors.New("directory account limit exceeded")
)

type Repository struct {
	db *sql.DB
}

type Row struct {
	GeneratedAt time.Time
	ID          *int64
	Name        *string
	Platform    *string
	Type        *string
	URLSource   *string
	Status      *string
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) List(ctx context.Context) ([]Row, error) {
	if r == nil || r.db == nil {
		return nil, ErrSnapshotInvalid
	}
	rows, err := r.db.QueryContext(ctx, projectionSQL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]Row, 0, 32)
	for rows.Next() {
		var (
			generatedAt sql.NullTime
			id          sql.NullInt64
			name        sql.NullString
			platform    sql.NullString
			accountType sql.NullString
			urlSource   sql.NullString
			status      sql.NullString
		)
		if err := rows.Scan(&generatedAt, &id, &name, &platform, &accountType, &urlSource, &status); err != nil {
			return nil, err
		}
		row := Row{}
		if generatedAt.Valid {
			row.GeneratedAt = generatedAt.Time
		}
		if id.Valid {
			value := id.Int64
			row.ID = &value
		}
		if name.Valid {
			value := name.String
			row.Name = &value
		}
		if platform.Valid {
			value := platform.String
			row.Platform = &value
		}
		if accountType.Valid {
			value := accountType.String
			row.Type = &value
		}
		if urlSource.Valid {
			value := urlSource.String
			row.URLSource = &value
		}
		if status.Valid {
			value := status.String
			row.Status = &value
		}
		out = append(out, row)
		if len(out) > maxAccounts {
			return nil, ErrAccountLimitExceeded
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrSnapshotInvalid
	}
	return out, nil
}
