package config

import "testing"

func TestMySQLPoolDefaults(t *testing.T) {
	t.Setenv("MYSQL_MAX_OPEN_CONNS", "")
	t.Setenv("MYSQL_MAX_IDLE_CONNS", "")
	t.Setenv("MYSQL_CONN_MAX_LIFETIME", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MySQL.MaxOpenConns != 40 || cfg.MySQL.MaxIdleConns != 20 {
		t.Fatalf("unexpected pool defaults: open=%d idle=%d", cfg.MySQL.MaxOpenConns, cfg.MySQL.MaxIdleConns)
	}
	if got := cfg.MySQL.ConnMaxLifetime.String(); got != "30m0s" {
		t.Fatalf("unexpected conn lifetime: %s", got)
	}
}

func TestMySQLPoolOverrides(t *testing.T) {
	t.Setenv("MYSQL_MAX_OPEN_CONNS", "80")
	t.Setenv("MYSQL_MAX_IDLE_CONNS", "40")
	t.Setenv("MYSQL_CONN_MAX_LIFETIME", "10m")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MySQL.MaxOpenConns != 80 || cfg.MySQL.MaxIdleConns != 40 {
		t.Fatalf("unexpected pool overrides: open=%d idle=%d", cfg.MySQL.MaxOpenConns, cfg.MySQL.MaxIdleConns)
	}
	if got := cfg.MySQL.ConnMaxLifetime.String(); got != "10m0s" {
		t.Fatalf("unexpected conn lifetime: %s", got)
	}
}

func TestMySQLIdleCannotExceedOpen(t *testing.T) {
	t.Setenv("MYSQL_MAX_OPEN_CONNS", "20")
	t.Setenv("MYSQL_MAX_IDLE_CONNS", "21")
	if _, err := Load(); err == nil {
		t.Fatal("expected validation error")
	}
}
