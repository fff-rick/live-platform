package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	dsn := strings.TrimSpace(os.Getenv("MYSQL_DSN"))
	if dsn == "" {
		log.Error("MYSQL_DSN is required")
		os.Exit(1)
	}
	dir := os.Getenv("MIGRATIONS_DIR")
	if dir == "" {
		dir = "/app/migrations"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := run(ctx, dsn, dir, log); err != nil {
		log.Error("migration failed", "error", err)
		os.Exit(1)
	}
	log.Info("migrations complete")
}

func run(ctx context.Context, dsn, dir string, log *slog.Logger) error {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return fmt.Errorf("parse mysql dsn: %w", err)
	}
	cfg.MultiStatements = true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("reserve mysql migration connection: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.PingContext(ctx); err != nil {
		return fmt.Errorf("ping mysql: %w", err)
	}

	// GET_LOCK is scoped to one MySQL session. Keep one *sql.Conn reserved for
	// the entire migration run so acquire, migration statements and release all
	// execute on the same physical connection. MySQL DDL auto-commits, so
	// migrations are forward-only and should remain idempotent where practical.
	var locked int
	if err := conn.QueryRowContext(ctx, `SELECT GET_LOCK('live-platform-schema-migrate', 60)`).Scan(&locked); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	if locked != 1 {
		return fmt.Errorf("migration lock timeout")
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), `SELECT RELEASE_LOCK('live-platform-schema-migrate')`)
	}()

	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version VARCHAR(255) PRIMARY KEY,
		checksum CHAR(64) NOT NULL,
		applied_at DATETIME(3) NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		checksum := hex.EncodeToString(sum[:])
		var existing string
		err = conn.QueryRowContext(ctx, `SELECT checksum FROM schema_migrations WHERE version=?`, name).Scan(&existing)
		switch {
		case err == nil:
			if existing != checksum {
				return fmt.Errorf("migration %s checksum changed after application", name)
			}
			log.Info("migration already applied", "version", name)
			continue
		case err != sql.ErrNoRows:
			return fmt.Errorf("check migration %s: %w", name, err)
		}

		log.Info("apply migration", "version", name)
		if _, err := conn.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO schema_migrations(version, checksum, applied_at) VALUES(?,?,NOW(3))`, name, checksum); err != nil {
			return fmt.Errorf("record migration %s: %w", name, err)
		}
	}
	if err := provisionServiceAccounts(ctx, conn); err != nil {
		return fmt.Errorf("provision service accounts: %w", err)
	}
	log.Info("service database accounts provisioned")
	return nil
}

type serviceAccount struct {
	schema, user, passwordEnv, privileges string
}

func provisionServiceAccounts(ctx context.Context, conn *sql.Conn) error {
	var accounts []serviceAccount
	for _, schema := range []string{"identity", "room", "commerce", "archive", "query"} {
		upper := strings.ToUpper(schema)
		accounts = append(accounts,
			serviceAccount{schema, "live_" + schema + "_app", upper + "_MYSQL_APP_PASSWORD", "SELECT, INSERT, UPDATE, DELETE"},
			serviceAccount{schema, "live_" + schema + "_migrate", upper + "_MYSQL_MIGRATION_PASSWORD", "ALL PRIVILEGES"},
			serviceAccount{schema, "live_" + schema + "_audit", upper + "_MYSQL_AUDIT_PASSWORD", "SELECT"},
		)
	}
	for _, account := range accounts {
		password := strings.TrimSpace(os.Getenv(account.passwordEnv))
		if password == "" {
			return fmt.Errorf("%s is required", account.passwordEnv)
		}
		var quotedPassword string
		if err := conn.QueryRowContext(ctx, `SELECT QUOTE(?)`, password).Scan(&quotedPassword); err != nil {
			return err
		}
		principal := fmt.Sprintf("'%s'@'%%'", account.user)
		if _, err := conn.ExecContext(ctx, "CREATE USER IF NOT EXISTS "+principal+" IDENTIFIED BY "+quotedPassword); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, "ALTER USER "+principal+" IDENTIFIED BY "+quotedPassword); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, "REVOKE ALL PRIVILEGES, GRANT OPTION FROM "+principal); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, fmt.Sprintf("GRANT %s ON `%s`.* TO %s", account.privileges, account.schema, principal)); err != nil {
			return err
		}
	}
	return nil
}
