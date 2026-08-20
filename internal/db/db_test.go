package db

import "testing"

const testURL = "postgres://postgres:postgres@localhost:5432/card_backend?sslmode=disable"

func TestPoolConfigMaxConns(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want int32
	}{
		{"no setting takes our default", testURL, defaultMaxConns},
		{"url setting wins", testURL + "&pool_max_conns=7", 7},
		// pgx decodes the key before reading it, so this is a real override.
		{"percent-encoded key still wins", testURL + "&%70ool_max_conns=7", 7},
		// The name only appears as another setting's value, so nothing is overridden.
		{"name in a value is not a setting", testURL + "&application_name=pool_max_conns", defaultMaxConns},
		{"keyword/value form", "host=localhost user=postgres dbname=card_backend pool_max_conns=7", 7},
		{"keyword/value default", "host=localhost user=postgres dbname=card_backend", defaultMaxConns},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := poolConfig(tt.url)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.MaxConns != tt.want {
				t.Errorf("MaxConns = %d, want %d", cfg.MaxConns, tt.want)
			}
		})
	}
}
