package directory

import (
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func ProvideHandler(db *sql.DB, cfg *config.Config) (*Handler, error) {
	if cfg == nil {
		svc, err := NewService(nil, &config.DirectoryConfig{})
		if err != nil {
			return nil, err
		}
		return NewHandler(svc), nil
	}
	repo := NewRepository(db)
	svc, err := NewService(repo, &cfg.Directory)
	if err != nil {
		return nil, err
	}
	return NewHandler(svc), nil
}
