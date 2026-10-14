package boundaries_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate boundary test")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func readFile(t *testing.T, root, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func importsOf(t *testing.T, root, name string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, name), nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	imports := make([]string, 0, len(f.Imports))
	for _, item := range f.Imports {
		value, err := strconv.Unquote(item.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		imports = append(imports, value)
	}
	sort.Strings(imports)
	return imports
}

func TestARCH001GatewayBusinessImportBaseline(t *testing.T) {
	const module = "github.com/example/live-platform/internal/"
	business := map[string]bool{"auth": true, "danmaku": true, "gift": true, "like": true, "room": true, "stats": true, "viewer": true, "wallet": true}
	var got []string
	for _, path := range importsOf(t, repositoryRoot(t), "cmd/api/main.go") {
		if path == "database/sql" {
			t.Fatal("Gateway must not add direct database/sql usage")
		}
		name := strings.TrimPrefix(path, module)
		if business[name] {
			got = append(got, name)
		}
	}
	want := []string{"auth", "danmaku", "gift", "like", "room", "stats", "viewer", "wallet"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Gateway business-import debt changed: got %v want %v; remove entries as P1 progresses, never add one silently", got, want)
	}
}

func TestARCH002ServiceDatabaseConnectionBaseline(t *testing.T) {
	root := repositoryRoot(t)
	expectedOpenCalls := map[string]int{
		"cmd/api/main.go":          1,
		"cmd/commerce/main.go":     1,
		"cmd/identityroom/main.go": 1,
		"cmd/interaction/main.go":  0,
		"cmd/worker/main.go":       1,
	}
	for file, want := range expectedOpenCalls {
		source := readFile(t, root, file)
		got := strings.Count(source, "mysqlstore.Open(")
		if got != want {
			t.Errorf("%s MySQL connection count=%d want baseline %d; remove connections deliberately, never add one silently", file, got, want)
		}
		if got > 0 && !strings.Contains(source, "cfg.MySQL.DSN") {
			t.Errorf("%s opens MySQL outside the reviewed service DSN", file)
		}
	}
}

func TestARCH003And004ServiceImportBoundaries(t *testing.T) {
	root := repositoryRoot(t)
	interaction := strings.Join(importsOf(t, root, "cmd/interaction/main.go"), "\n")
	for _, forbidden := range []string{"database/sql", "/internal/store/mysqlstore", "/internal/gift", "/internal/wallet"} {
		if strings.Contains(interaction, forbidden) {
			t.Errorf("Interaction imports forbidden dependency %q", forbidden)
		}
	}

	commerce := readFile(t, root, "cmd/commerce/main.go")
	if got := strings.Count(commerce, "room.NewRepository(mysql)"); got != 1 {
		t.Fatalf("known Commerce-to-Room repository exception count=%d want 1; P2 must remove it, new exceptions are forbidden", got)
	}
	identity := strings.Join(importsOf(t, root, "cmd/identityroom/main.go"), "\n")
	for _, forbidden := range []string{"/internal/gift", "/internal/wallet", "/internal/danmaku", "/internal/like", "/internal/mq"} {
		if strings.Contains(identity, forbidden) {
			t.Errorf("IdentityRoom imports forbidden dependency %q", forbidden)
		}
	}
}

func TestARCH005SQLTableOwnershipBaseline(t *testing.T) {
	root := repositoryRoot(t)
	allowed := map[string]map[string]bool{
		"auth":       set("users"),
		"room":       set("live_rooms", "live_sessions", "room_bans", "room_mutes", "users"),
		"gift":       set("gifts", "gift_orders", "wallets", "wallet_transactions", "outbox_events"),
		"wallet":     set("wallets", "wallet_transactions"),
		"danmaku":    set("danmaku_records"),
		"eventdedup": set("processed_events"),
		"like":       set("room_like_snapshots"),
		"outbox":     set("outbox_events"),
		"httpapi":    set("danmaku_records", "gift_orders", "users", "gifts"),
	}
	schemaTables := map[string]bool{}
	createPattern := regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-z_][a-z0-9_]*)`)
	_ = filepath.WalkDir(filepath.Join(root, "migrations"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".sql") {
			return err
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, match := range createPattern.FindAllStringSubmatch(string(raw), -1) {
			schemaTables[strings.ToLower(match[1])] = true
		}
		return nil
	})
	tablePattern := regexp.MustCompile(`(?i)\b(?:FROM|JOIN|INTO|UPDATE|DELETE\s+FROM)\s+([a-z_][a-z0-9_.]*)`)
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(filepath.Join(root, "internal"), path)
		owner := strings.Split(filepath.ToSlash(rel), "/")[0]
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, match := range tablePattern.FindAllStringSubmatch(string(raw), -1) {
			table := strings.ToLower(match[1])
			if !schemaTables[table] {
				continue
			}
			if !allowed[owner][table] {
				t.Errorf("%s accesses unlisted table %q; update ownership deliberately", filepath.ToSlash(rel), table)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestARCH006RedisKeyOwnershipBaseline(t *testing.T) {
	root := repositoryRoot(t)
	allowed := map[string][]string{
		"danmaku":    {"live:limit:danmaku:user:"},
		"gift":       {"live:limit:gift:user:"},
		"like":       {"live:room:{"},
		"redisstore": {"live:room:", "live:stats:rooms:"},
	}
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.Contains(path, "/config/") {
			return nil
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		owner := filepath.Base(filepath.Dir(path))
		ast.Inspect(parsed, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, unquoteErr := strconv.Unquote(literal.Value)
			if unquoteErr != nil || !strings.HasPrefix(value, "live:") {
				return true
			}
			permitted := false
			for _, prefix := range allowed[owner] {
				permitted = permitted || strings.HasPrefix(value, prefix)
			}
			if !permitted {
				t.Errorf("%s owns unlisted Redis key %q", filepath.ToSlash(path), value)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestARCH007EventTypeOwnership(t *testing.T) {
	root := repositoryRoot(t)
	owners := map[string]string{"EventTypeDanmakuSent": "internal/danmaku/", "EventTypeGiftSent": "internal/gift/"}
	for symbol, owner := range owners {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			if rel == "internal/mq/message.go" {
				return nil
			}
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if strings.Contains(string(raw), symbol) && !strings.HasPrefix(rel, owner) {
				t.Errorf("%s uses %s outside owner %s", rel, symbol, owner)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestARCH008InternalRoutesAreNotInGatewayOrIngress(t *testing.T) {
	root := repositoryRoot(t)
	server := readFile(t, root, "internal/httpapi/server.go")
	start := strings.Index(server, "func (s *Server) routes()")
	end := strings.Index(server[start:], "func (s *Server) commerceOr")
	if start < 0 || end < 0 {
		t.Fatal("locate Gateway route table")
	}
	section := server[start : start+end]
	if strings.Contains(section, "/internal/v1/") {
		t.Fatal("Gateway route table exposes /internal/v1")
	}
	_ = filepath.WalkDir(filepath.Join(root, "deploy"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(raw), "/internal/v1/") {
			t.Errorf("deployment file %s exposes an internal route", path)
		}
		return nil
	})
}

type routeCatalog struct {
	BasePath   string `json:"base_path"`
	ErrorShape struct {
		Code, Message string
	} `json:"error_shape"`
	Routes []struct {
		Method, Path, Auth, Owner string
		SuccessStatus             []int    `json:"success_status"`
		ErrorStatus               []int    `json:"error_status"`
		ErrorCodes                []string `json:"error_codes"`
	} `json:"routes"`
}

func TestHTTPRouteCatalogMatchesGateway(t *testing.T) {
	root := repositoryRoot(t)
	var catalog routeCatalog
	if err := json.Unmarshal([]byte(readFile(t, root, "doc/contracts/public-api-v1.json")), &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.BasePath != "/api/v1" || catalog.ErrorShape.Code != "string" || catalog.ErrorShape.Message != "string" {
		t.Fatalf("invalid catalog metadata: %+v", catalog)
	}
	want := make([]string, 0, len(catalog.Routes))
	seen := map[string]bool{}
	for _, route := range catalog.Routes {
		key := route.Method + " " + route.Path
		if seen[key] {
			t.Fatalf("duplicate catalog route %s", key)
		}
		seen[key] = true
		if route.Auth != "public" && route.Auth != "required" {
			t.Errorf("%s has invalid auth %q", key, route.Auth)
		}
		if route.Owner == "" || len(route.SuccessStatus) == 0 || len(route.ErrorStatus) == 0 || len(route.ErrorCodes) == 0 {
			t.Errorf("%s is missing owner/status/error metadata", key)
		}
		want = append(want, key)
	}

	server := readFile(t, root, "internal/httpapi/server.go")
	start := strings.Index(server, "func (s *Server) routes()")
	end := strings.Index(server[start:], "func (s *Server) commerceOr")
	section := server[start : start+end]
	pattern := regexp.MustCompile(`s\.mux\.Handle(?:Func)?\("([A-Z]+) (/api/v1/[^\"]+)"`)
	var got []string
	for _, match := range pattern.FindAllStringSubmatch(section, -1) {
		got = append(got, match[1]+" "+match[2])
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("public-api-v1 drift\nregistered:\n%s\n\ncatalog:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func set(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}
