package mariadb

import (
	"strings"
	"testing"
)

func TestClassifyCheck(t *testing.T) {
	tests := []struct {
		name string
		rows []checkRow
		want string // "" means the table is fine
	}{
		{"ok", []checkRow{{"status", "OK"}}, ""},
		{"ok, lowercase", []checkRow{{"status", "ok"}}, ""},
		{"already up to date", []checkRow{{"status", "Table is already up to date"}}, ""},
		{"engine can't check", []checkRow{{"note", "The storage engine for the table doesn't support check"}}, ""},
		{"info then ok", []checkRow{{"info", "Found 3 rows"}, {"status", "OK"}}, ""},
		{"error", []checkRow{{"error", "Table 'shop.orders' is marked as crashed"}, {"status", "Operation failed"}}, "marked as crashed; Operation failed"},
		{"warning", []checkRow{{"warning", "1 client is using or hasn't closed the table properly"}, {"status", "OK"}}, "hasn't closed the table properly"},
		{"corrupt status", []checkRow{{"status", "Corrupt"}}, "Corrupt"},
		{"no rows at all", nil, "gave no result"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyCheck(tt.rows)
			if (tt.want == "") != (got == "") || !strings.Contains(got, tt.want) {
				t.Errorf("classifyCheck = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestQuoteIdent(t *testing.T) {
	for in, want := range map[string]string{
		"orders":   "`orders`",
		"my table": "`my table`",
		"we`ird":   "`we``ird`",
		"café ✓":   "`café ✓`",
		"``":       "``````",
	} {
		if got := quoteIdent(in); got != want {
			t.Errorf("quoteIdent(%q) = %s, want %s", in, got, want)
		}
	}
}
