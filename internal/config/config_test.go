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

func TestWorkerRoleDefaults(t *testing.T) {
	t.Setenv("WORKER_ROLES", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"stats", "like-snapshot", "outbox", "gift-consumer", "danmaku-consumer"}
	if len(cfg.Worker.Roles) != len(want) {
		t.Fatalf("unexpected worker role count: %v", cfg.Worker.Roles)
	}
	for i := range want {
		if cfg.Worker.Roles[i] != want[i] {
			t.Fatalf("unexpected worker roles: %v", cfg.Worker.Roles)
		}
	}
}

func TestWorkerRoleValidation(t *testing.T) {
	t.Setenv("WORKER_ROLES", "stats,unknown")
	if _, err := Load(); err == nil {
		t.Fatal("expected unsupported worker role error")
	}
}

func TestKafkaSASLRequiresCredentials(t *testing.T) {
	t.Setenv("KAFKA_SASL_MECHANISM", "scram-sha-256")
	t.Setenv("KAFKA_SASL_USERNAME", "")
	t.Setenv("KAFKA_SASL_PASSWORD", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected SASL credential validation error")
	}
}

func TestStatsOnlyWorkerDoesNotRequireKafkaCredentials(t *testing.T) {
	t.Setenv("WORKER_ROLES", "stats")
	t.Setenv("KAFKA_SASL_MECHANISM", "scram-sha-512")
	t.Setenv("KAFKA_SASL_USERNAME", "")
	t.Setenv("KAFKA_SASL_PASSWORD", "")
	if _, err := Load(); err != nil {
		t.Fatalf("stats-only worker should not require Kafka credentials: %v", err)
	}
}
