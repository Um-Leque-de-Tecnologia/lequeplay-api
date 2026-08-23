package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"

	_ "github.com/jackc/pgx/v5/stdlib" // driver database/sql para o goose
	"github.com/pressly/goose/v3"
)

// migrateLockKey é uma chave fixa de advisory lock do Postgres. Pods concorrentes
// (durante um rolling update) serializam a migração: o segundo espera o primeiro
// terminar antes de aplicar qualquer coisa.
const migrateLockKey int64 = 0x6c657170 // "leqp" em hex

// Migrate aplica as migrations embutidas usando um advisory lock para ser seguro
// com múltiplas réplicas. Idempotente: se o schema já está atualizado, não faz nada.
func Migrate(ctx context.Context, dsn string, fsys fs.FS) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("abrir conexão para migração: %w", err)
	}
	defer func() { _ = db.Close() }()

	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("obter conexão para lock: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrateLockKey); err != nil {
		return fmt.Errorf("adquirir advisory lock de migração: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", migrateLockKey)
	}()

	goose.SetBaseFS(fsys)
	defer goose.SetBaseFS(nil)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("definir dialeto goose: %w", err)
	}
	if err := goose.UpContext(ctx, db, "."); err != nil {
		return fmt.Errorf("aplicar migrations: %w", err)
	}
	return nil
}
