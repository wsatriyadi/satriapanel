package panel

import "testing"

func TestClassifyMCPSQL(t *testing.T) {
	tests := []struct {
		name  string
		query string
		scope string
		risky bool
	}{
		{"select", "SELECT * FROM users", "read", false},
		{"cte-select", "WITH x AS (SELECT 1) SELECT * FROM x", "read", false},
		{"cte-delete", "WITH x AS (SELECT 1) DELETE FROM users", "data-write", true},
		{"cte-delete-where", "WITH x AS (SELECT 1) DELETE FROM users WHERE id=1", "data-write", false},
		{"cte-update", "WITH x AS (SELECT 1) UPDATE users SET a=1 WHERE id=1", "data-write", false},
		{"explain-delete", "EXPLAIN ANALYZE DELETE FROM users", "data-write", true},
		{"explain-drop", "EXPLAIN ANALYZE DROP TABLE users", "admin", true},
		{"pragma-write", "PRAGMA journal_mode=WAL", "admin", true},
		{"comment-delete", "/* hapus */ DELETE FROM users", "data-write", true},
		{"literal", "SELECT 'DELETE FROM users'", "read", false},
		{"create", "CREATE TABLE t (id int)", "admin", true},
		{"update-where-literal", "UPDATE t SET a='WHERE'", "data-write", true},
		{"update-where", "UPDATE t SET a=1 WHERE id=1", "data-write", false},
		{"select-into", "SELECT * INTO backup FROM users", "admin", true},
		{"show", "SHOW TABLES", "read", false},
		{"insert", "INSERT INTO t(a) VALUES(1)", "data-write", false},
		{"empty", "  ", "admin", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope, risky := classifyMCPSQL(tt.query)
			if scope != tt.scope || risky != tt.risky {
				t.Fatalf("classifyMCPSQL(%q) = (%q,%v), want (%q,%v)", tt.query, scope, risky, tt.scope, tt.risky)
			}
		})
	}
}

func TestMCPScopes(t *testing.T) {
	read := &MCPToken{Scopes: []string{"read"}}
	if !read.hasScope("read") || read.hasScope("deploy") || read.hasScope("admin") {
		t.Fatal("token read tidak boleh memiliki scope lain")
	}
	admin := &MCPToken{Scopes: []string{"admin"}}
	for _, s := range mcpAllScopes {
		if !admin.hasScope(s) {
			t.Fatalf("token admin harus mencakup scope %s", s)
		}
	}
}
