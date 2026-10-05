package postgres

import "testing"

func TestBindQueryPreservesPostgreSQLLiteralsAndComments(t *testing.T) {
	for _, test := range []struct{ name, query, want string }{
		{"fragments", "SELECT ?::bigint, ?::text", "SELECT $1::bigint, $2::text"},
		{"quoted", `SELECT '?', 'it''s ?', "?", ?`, `SELECT '?', 'it''s ?', "?", $1`},
		{"escaped", `SELECT E'escaped\' ?', ?`, `SELECT E'escaped\' ?', $1`},
		{"dollars", `SELECT $$?$$, $body_1$?$body_1$, ?`, `SELECT $$?$$, $body_1$?$body_1$, $1`},
		{"native", "SELECT $1, $2, '$3'", "SELECT $1, $2, '$3'"},
		{"comments", "SELECT ? /* ? /* ? */ ? */, ? -- ?\n, ?", "SELECT $1 /* ? /* ? */ ? */, $2 -- ?\n, $3"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := bindQuery(test.query); got != test.want {
				t.Fatalf("query=%q want=%q", got, test.want)
			}
		})
	}
}
