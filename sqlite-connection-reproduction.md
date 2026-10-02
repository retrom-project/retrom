# Released store connection lifecycle reproduction

Run against a checkout of release v0.0.102 using a separate temporary module named `retrom/audit/store-lifecycle`, with a `replace retrom => /path/to/retrom-v0.0.102` directive. No production database is used when no argument is passed. The program creates and removes its own temporary database.

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	dbapi "retrom/internal/database"
	"retrom/internal/store"
	"time"
)

func emit(v any) { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
func snapshot(db *store.DB) map[string]int {
	v := map[string]int{}
	for _, s := range []string{"foreign_keys", "busy_timeout", "synchronous"} {
		var n int
		if e := dbapi.QueryRowContext(context.Background(), db.SQL, "PRAGMA "+s).Scan(&n); e != nil {
			panic(e)
		}
		v[s] = n
	}
	return v
}
func main() {
	if len(os.Args) > 1 {
		db, e := store.Open(context.Background(), os.Args[1], time.Now)
		r := map[string]any{"opened": e == nil, "startupError": fmt.Sprint(e)}
		if db != nil {
			db.Close()
		}
		emit(r)
		return
	}
	dir, e := os.MkdirTemp("", "retrom-store-lifecycle-")
	if e != nil {
		panic(e)
	}
	defer os.RemoveAll(dir)
	name := filepath.Join(dir, "retrom.db")
	db, e := store.Open(context.Background(), name, time.Now)
	if e != nil {
		panic(e)
	}
	r := map[string]any{"before": snapshot(db)}
	for _, sql := range []string{"CREATE TABLE audit_parent(id INTEGER PRIMARY KEY)", "CREATE TABLE audit_child(id INTEGER PRIMARY KEY,parent_id INTEGER REFERENCES audit_parent(id) ON DELETE CASCADE)", "INSERT INTO audit_parent VALUES(1)", "INSERT INTO audit_child VALUES(1,1)"} {
		if _, e = db.SQL.ExecContext(context.Background(), sql); e != nil {
			panic(e)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	var sum int
	r["queryError"] = fmt.Sprint(dbapi.QueryRowContext(ctx, db.SQL, "WITH RECURSIVE c(x) AS (VALUES(0) UNION ALL SELECT x+1 FROM c WHERE x<100000000) SELECT sum(x) FROM c").Scan(&sum))
	cancel()
	time.Sleep(200 * time.Millisecond)
	r["after"] = snapshot(db)
	_, e = db.SQL.ExecContext(context.Background(), "DELETE FROM audit_parent WHERE id=1")
	r["deleteError"] = fmt.Sprint(e)
	var count int
	_ = dbapi.QueryRowContext(context.Background(), db.SQL, "SELECT count(*) FROM audit_child").Scan(&count)
	r["orphanChildren"] = count
	r["integrityError"] = fmt.Sprint(db.IntegrityCheck(context.Background()))
	db.Close()
	next, e := store.Open(context.Background(), name, time.Now)
	r["reopenError"] = fmt.Sprint(e)
	if next != nil {
		next.Close()
	}
	emit(r)
}

```
