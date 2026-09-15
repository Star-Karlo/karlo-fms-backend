// Command migrate-schema applies pending db/migrations/*.sql, tracked in
// schema_migrations — the same contract as scripts/ci-migrate.sh, from
// INSIDE the VPC.
//
// WHY THIS EXISTS. ci-migrate.sh runs psql from the GitHub runner, which only
// works because the Singapore RDS can be opened to the runner's IP for the
// duration. The Jakarta RDS sits in the platform's private subnets behind a
// security group that admits the tasks SG and nothing else — no public
// address, nothing to open. So the migration has to run where the database
// is reachable: as a one-off Fargate task in the same VPC, from the same
// image the api ships in, with the migrations embedded so the task needs no
// checkout.
//
// The semantics are ci-migrate.sh's, deliberately: one transaction per file,
// then a separate INSERT recording the version. A failure between the two is
// safe to retry because every file is idempotent (IF NOT EXISTS), which is
// also why the two are not one transaction — a version row inside the same
// transaction as a DDL that partially applied would be rolled back with it
// and the file would run again anyway.
//
// Connects as whatever DATABASE_URL says. Migrations do CREATE TABLE / GRANT /
// RLS, so that must be the owner role (fms_admin), not the app's fms_app.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

func main() {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		log.Fatal("DATABASE_URL is not set")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations
		(version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		log.Fatalf("schema_migrations: %v", err)
	}

	// Where the Dockerfile puts db/migrations; the repo path when run locally.
	dir := os.Getenv("MIGRATIONS_DIR")
	if dir == "" {
		dir = "db/migrations"
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Fatalf("read %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // 0001_…, 0002_… — lexical order is version order

	applied := 0
	for _, name := range names {
		var exists bool
		if err := conn.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, name).Scan(&exists); err != nil {
			log.Fatalf("%s: check: %v", name, err)
		}
		if exists {
			continue
		}
		sql, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf(">> applying %s\n", name)

		// The file as ONE transaction, like psql -1. Exec on a multi-statement
		// string uses the simple protocol, which is what these files need —
		// DO blocks, CREATE POLICY, COMMENT ON all run as sent.
		tx, err := conn.Begin(ctx)
		if err != nil {
			log.Fatalf("%s: begin: %v", name, err)
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			_ = tx.Rollback(ctx)
			log.Fatalf("%s: %v", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			log.Fatalf("%s: commit: %v", name, err)
		}
		if _, err := conn.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
			log.Fatalf("%s: record: %v", name, err)
		}
		applied++
	}
	fmt.Printf(">> %d new migration(s) applied; schema up to date\n", applied)
}
