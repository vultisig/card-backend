package db

import "testing"

const testURL = "postgres://postgres:postgres@localhost:5432/card_backend?sslmode=disable"

func TestPoolConfigMaxConns(t *testing.T) {
	cfg, err := poolConfig(testURL)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxConns != defaultMaxConns {
		t.Errorf("MaxConns = %d, want %d", cfg.MaxConns, defaultMaxConns)
	}

	cfg, err = poolConfig(testURL + "&pool_max_conns=7")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxConns != 7 {
		t.Errorf("MaxConns = %d, want 7 from the URL", cfg.MaxConns)
	}
}
