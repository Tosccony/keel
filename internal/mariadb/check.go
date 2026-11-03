package mariadb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// TableProblem is a table that failed CHECK TABLE.
type TableProblem struct {
	Table   string // database.table
	Message string
}

// CheckTables runs CHECK TABLE on every table except MariaDB's built-in
// views (information_schema, performance_schema, sys). It includes the
// mysql database, whose Aria tables are the ones that break in XAMPP.
func (s *Server) CheckTables(ctx context.Context) (checked int, problems []TableProblem, err error) {
	db, err := sql.Open("mysql", s.dsn())
	if err != nil {
		return 0, nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	rows, err := db.QueryContext(ctx, `
		SELECT table_schema, table_name FROM information_schema.tables
		WHERE table_type = 'BASE TABLE'
		  AND table_schema NOT IN ('information_schema', 'performance_schema', 'sys')
		ORDER BY table_schema, table_name`)
	if err != nil {
		return 0, nil, fmt.Errorf("mariadb: list tables: %w", err)
	}
	var tables [][2]string
	for rows.Next() {
		var schema, name string
		if err := rows.Scan(&schema, &name); err != nil {
			rows.Close()
			return 0, nil, err
		}
		tables = append(tables, [2]string{schema, name})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}

	for _, t := range tables {
		msg, err := checkTable(ctx, db, t[0], t[1])
		if err != nil {
			msg = err.Error()
		}
		checked++
		if msg != "" {
			problems = append(problems, TableProblem{Table: t[0] + "." + t[1], Message: msg})
		}
	}
	return checked, problems, nil
}

// checkRow is one result row of CHECK TABLE.
type checkRow struct {
	Type string // status, error, warning, info or note
	Text string
}

func checkTable(ctx context.Context, db *sql.DB, schema, name string) (string, error) {
	rows, err := db.QueryContext(ctx, "CHECK TABLE "+quoteIdent(schema)+"."+quoteIdent(name))
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var results []checkRow
	for rows.Next() {
		var table, op, typ, text sql.NullString
		if err := rows.Scan(&table, &op, &typ, &text); err != nil {
			return "", err
		}
		results = append(results, checkRow{Type: typ.String, Text: text.String})
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return classifyCheck(results), nil
}

// classifyCheck turns CHECK TABLE's result rows into a problem description,
// or "" if the table is fine. Errors, warnings and any status other than OK
// count as problems; info and notes (such as "doesn't support check") don't.
func classifyCheck(rows []checkRow) string {
	if len(rows) == 0 {
		return "CHECK TABLE gave no result"
	}
	var msgs []string
	for _, r := range rows {
		switch strings.ToLower(r.Type) {
		case "error", "warning":
			msgs = append(msgs, r.Text)
		case "status":
			if !strings.EqualFold(r.Text, "OK") && !strings.EqualFold(r.Text, "Table is already up to date") {
				msgs = append(msgs, r.Text)
			}
		}
	}
	return strings.Join(msgs, "; ")
}

// quoteIdent quotes a database or table name for SQL.
func quoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
