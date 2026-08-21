package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pgx defaults this to max(4, NumCPU), which ties pool size to the host's core count rather than to us.
const defaultMaxConns = 20

func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := poolConfig(url)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// poolConfig parses url and sets MaxConns unless url already carries one.
func poolConfig(url string) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	// ParseConfig reports the resulting MaxConns but not whether it came from url,
	// so ask pgconn — it is the parser pgxpool itself uses, so it reads the setting
	// the same way: both URL and keyword/value forms, percent-encoded keys included.
	// A substring search would miss those and match the value of an unrelated setting.
	conn, err := pgconn.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	if _, ok := conn.RuntimeParams["pool_max_conns"]; !ok {
		cfg.MaxConns = defaultMaxConns
	}
	return cfg, nil
}
