// Package migrate applies a module's SQL files over pgx. The upstream images do not
// contain their schema (it is mounted into initdb.d), so tsi applies it itself, which
// also works for a Postgres the user already runs.
package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/tsi-coop/tsi-cli/catalog"
	"github.com/tsi-coop/tsi-cli/tsierr"
)

const ledger = "_tsi_cli_migrations"

func connect(ctx context.Context, dsn string) (*pgx.Conn, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	// Migration files hold many statements and dollar-quoted bodies, so use the simple protocol.
	cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	return pgx.ConnectConfig(ctx, cfg)
}

// IsAuthFailure reports whether PostgreSQL rejected the credentials (SQLSTATE 28P01).
func IsAuthFailure(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && (pe.Code == "28P01" || pe.Code == "28000")
}

// WaitForServer retries until a connection to dsn succeeds or ctx ends.
func WaitForServer(ctx context.Context, dsn string) error {
	var last error
	for {
		c, err := connect(ctx, dsn)
		if err == nil {
			c.Close(ctx)
			return nil
		}
		last = err
		if IsAuthFailure(err) {
			return err // retrying cannot help
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("postgres did not become reachable: %w", last)
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
func quoteLit(s string) string   { return `'` + strings.ReplaceAll(s, `'`, `''`) + `'` }

// EnsureDatabase creates the module's role and database if they do not exist. adminDSN
// must be allowed to create roles and databases.
func EnsureDatabase(ctx context.Context, adminDSN, user, password, db string) error {
	c, err := connect(ctx, adminDSN)
	if err != nil {
		return fmt.Errorf("connect as admin: %w", err)
	}
	defer c.Close(ctx)
	var n int
	if err := c.QueryRow(ctx, "SELECT count(*) FROM pg_roles WHERE rolname = "+quoteLit(user)).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		if _, err := c.Exec(ctx, "CREATE ROLE "+quoteIdent(user)+" LOGIN PASSWORD "+quoteLit(password)); err != nil {
			return err
		}
	} else if _, err := c.Exec(ctx, "ALTER ROLE "+quoteIdent(user)+" PASSWORD "+quoteLit(password)); err != nil {
		return err
	}
	if err := c.QueryRow(ctx, "SELECT count(*) FROM pg_database WHERE datname = "+quoteLit(db)).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		if _, err := c.Exec(ctx, "CREATE DATABASE "+quoteIdent(db)+" OWNER "+quoteIdent(user)); err != nil {
			return err
		}
	}
	return nil
}

// Result reports what Apply did.
type Result struct {
	Applied []string
	Skipped []string
}

// Apply runs the module's migration steps that are not yet recorded in the database.
// env resolves from_env references of set_database_setting actions. Those actions run over
// adminDSN, because Postgres only lets a superuser set a database-level custom setting.
func Apply(ctx context.Context, src Source, m *catalog.Module, moduleDSN, adminDSN, dbName string, env func(string) string) (*Result, error) {
	c, err := connect(ctx, moduleDSN)
	if err != nil {
		return nil, tsierr.New(tsierr.CodeMigration, m.Name, "migrate", "cannot connect to the module database", "check the database settings in tsi.yaml", err)
	}
	defer func() { c.Close(ctx) }()
	if _, err := c.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+ledger+" (id text PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
		return nil, tsierr.New(tsierr.CodeMigration, m.Name, "migrate", "cannot create the migration ledger", "", err)
	}
	res := &Result{}
	for _, st := range m.Migrations.Steps {
		id := st.ID()
		var sql, sum string
		switch {
		case st.File != "":
			raw, err := src.Get(ctx, m, st)
			if err != nil {
				return nil, err
			}
			sql = string(raw)
			h := sha256.Sum256(raw)
			sum = hex.EncodeToString(h[:])
		default:
			val := env(st.FromEnv)
			if val == "" {
				return nil, tsierr.New(tsierr.CodeMigration, m.Name, id, st.FromEnv+" is empty",
					"refusing to configure the database with an empty key", nil)
			}
			sql = "ALTER DATABASE " + quoteIdent(dbName) + " SET " + st.Setting + " = " + quoteLit(val)
			h := sha256.Sum256([]byte(st.Setting + "=" + val))
			sum = hex.EncodeToString(h[:])
		}
		var prev string
		err := c.QueryRow(ctx, "SELECT checksum FROM "+ledger+" WHERE id = "+quoteLit(id)).Scan(&prev)
		if err == nil {
			if prev != sum {
				return nil, tsierr.New(tsierr.CodeMigration, m.Name, id, "already applied with different content",
					"the schema or key changed since it was applied; start over with `tsi compose down --volumes`", nil)
			}
			res.Skipped = append(res.Skipped, id)
			continue
		} else if err != pgx.ErrNoRows {
			return nil, err
		}
		if st.Action == "set_database_setting" {
			if err := execAdmin(ctx, adminDSN, sql); err != nil {
				return nil, tsierr.New(tsierr.CodeMigration, m.Name, id, "cannot set the database setting",
					"the admin connection must be a superuser", err)
			}
			// ALTER DATABASE ... SET only affects new sessions; reconnect so later files see it.
			c.Close(ctx)
			if c, err = connect(ctx, moduleDSN); err != nil {
				return nil, err
			}
		} else if _, err := c.Exec(ctx, sql); err != nil {
			return nil, tsierr.New(tsierr.CodeMigration, m.Name, id, "migration failed", "", err)
		}
		if _, err := c.Exec(ctx, "INSERT INTO "+ledger+"(id, checksum) VALUES ("+quoteLit(id)+","+quoteLit(sum)+")"); err != nil {
			return nil, err
		}
		res.Applied = append(res.Applied, id)
	}
	return res, nil
}

func execAdmin(ctx context.Context, dsn, sql string) error {
	c, err := connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer c.Close(ctx)
	_, err = c.Exec(ctx, sql)
	return err
}
