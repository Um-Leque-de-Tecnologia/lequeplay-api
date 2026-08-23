// Package migrations embute os arquivos SQL de migração para serem aplicados
// pelo goose em tempo de execução (sem depender do filesystem no container).
package migrations

import "embed"

// FS contém todas as migrations SQL embutidas no binário.
//
//go:embed *.sql
var FS embed.FS
