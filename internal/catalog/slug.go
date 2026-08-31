package catalog

import (
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// ESPELHO EM SQL — db/migrations
//
// Este arquivo é a fonte da verdade do algoritmo de slug, mas ele NÃO é o único
// lugar onde o algoritmo existe: há uma cópia em SQL, em db/migrations, usada
// pelo seed para gravar a coluna de slug direto no banco. Os dois lados precisam
// andar juntos, no mesmo PR. Se divergirem, o slug que o banco grava deixa de
// bater com o que a API calcula e todo link publicado do título quebra.
//
// O contrato compartilhado pelos dois lados são estes quatro exemplos:
//
//	"Homem-Aranha: Um Novo Dia" -> homem-aranha-um-novo-dia
//	"Ficção Científica"         -> ficcao-cientifica
//	"Coração & Alma"            -> coracao-alma
//	"  Lei & Ordem  "           -> lei-ordem
//
// Mexeu em regra aqui? Rode os testes de slug_test.go, ajuste o SQL e confira
// que os quatro exemplos continuam valendo dos dois lados.
// ---------------------------------------------------------------------------

// slugSemTitulo é o slug usado quando o título não sobra nenhum caractere
// aproveitável (regra 5). Ex.: título vazio ou só de pontuação.
const slugSemTitulo = "sem-titulo"

// Slugify converte um título em slug de URL, aplicando as regras nesta ordem:
//
//	(1) minúsculas;
//	(2) acentos viram a letra base (ver semAcento);
//	(3) tudo que não for [a-z0-9] vira "-";
//	(4) hifens repetidos viram um só, e hífen no início e no fim é removido;
//	(5) se sobrar string vazia, o resultado é "sem-titulo".
//
// As regras 3 e 4 saem de graça de uma passada só: um caractere inválido apenas
// marca um hífen como pendente, e o hífen só é escrito quando vem um caractere
// válido depois dele. Assim uma sequência de inválidos vira um hífen só, hífen
// no fim nunca chega a ser escrito e hífen no início é barrado por len(out) > 0.
func Slugify(s string) string {
	out := make([]rune, 0, len(s))
	hifenPendente := false

	// Percorre por rune, não por byte: "ç" ocupa dois bytes em UTF-8 e iterar
	// por byte partiria o caractere no meio, corrompendo a saída.
	for _, r := range strings.ToLower(s) {
		c := semAcento(r)
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			if hifenPendente && len(out) > 0 {
				out = append(out, '-')
			}
			hifenPendente = false
			out = append(out, c)
			continue
		}
		hifenPendente = true
	}

	if len(out) == 0 {
		return slugSemTitulo
	}
	return string(out)
}

// semAcento devolve a letra base de uma rune acentuada, ou a própria rune quando
// ela não está no mapa. É o equivalente Go do translate() do espelho em SQL, com
// exatamente este mapa:
//
//	de:   áàâãäéèêëíìîïóòôõöúùûüçñ
//	para: aaaaaeeeeiiiiooooouuuucn
//
// Só as minúsculas aparecem aqui de propósito: a regra 1 (minúsculas) roda antes
// da regra 2, então "CORAÇÃO" já chegou como "coração". Vale para os dois lados —
// no SQL o translate() também vem depois do lower(). Qualquer outra rune fora do
// mapa passa direto e cai na regra 3, virando "-".
func semAcento(r rune) rune {
	switch r {
	case 'á', 'à', 'â', 'ã', 'ä':
		return 'a'
	case 'é', 'è', 'ê', 'ë':
		return 'e'
	case 'í', 'ì', 'î', 'ï':
		return 'i'
	case 'ó', 'ò', 'ô', 'õ', 'ö':
		return 'o'
	case 'ú', 'ù', 'û', 'ü':
		return 'u'
	case 'ç':
		return 'c'
	case 'ñ':
		return 'n'
	}
	return r
}

// SlugCandidates devolve os candidatos a slug de um título, do mais curto ao mais
// específico. Esta função só monta a fila; QUEM ESCOLHE é quem enxerga o lote
// inteiro. O contrato de desempate, idêntico ao do espelho em db/migrations, é:
// quando uma base repete, NENHUM dos títulos em conflito fica com a base pura —
// todos descem juntos para o próximo candidato. Deixar o "primeiro" ficar com a
// base pura reintroduziria a dependência de ordem de inserção que a regra existe
// para evitar, e faria o primeiro re-seed reescrever o slug que o backfill gravou.
// Do lado Go isso vive em cmd/seed (candidatosDoLote); no SQL, nos níveis
// repete_base/repete_ano/repete_tmdb. Escolher título a título quebra o espelho.
//
// A fila, do mais curto ao mais específico:
//
//	base                 ex.: homem-aranha
//	base-<ano>           ex.: homem-aranha-2021
//	base-<ano>-<tmdbID>  ex.: homem-aranha-2021-634649
//
// Com ano ausente (0 ou negativo) o candidato do meio é pulado E o último perde a
// parte do ano — sobram base e base-<tmdbID>. Ano 0 não é um ano: costurar um
// "-0-" no meio do slug publicaria na URL um dado que o título não tem. Esta é
// também a forma que o espelho em db/migrations gera (o ramo ELSE do CASE que
// monta cand_tmdb), e é por isso que as duas pontas precisam concordar aqui: se
// um lado escrever base-0-<tmdbID> e o outro base-<tmdbID>, o slug que o backfill
// grava deixa de bater com o que o seed calcula em todo título sem ano.
//
// POR QUE ano e tmdb_id, e não um contador (base-2, base-3): sufixo por ordem de
// inserção muda a cada re-seed. O título que entrou primeiro ontem pode entrar
// depois hoje, e aí o link de ontem passa a apontar para outro título. Ano e
// tmdb_id são propriedades estáveis do próprio título, então o slug sai igual
// toda vez que o seed roda, independente da ordem.
//
// POR QUE a fila termina na prática: o último candidato termina no tmdb_id, que
// separa títulos diferentes em todo dado vindo da TMDB. Não é garantia absoluta —
// a unicidade da tabela é (tipo, tmdb_id), então um filme e uma série que dividem
// o mesmo tmdb_id chegam ao fim da fila com o mesmo candidato. O espelho em SQL
// cobre esse caso com um nível extra baseado no id da linha, para a migration não
// abortar; do lado Go quem chama (cmd/seed) prefere devolver erro a gravar um slug
// que não é o do título. Se "nenhum slug livre" aparecer no seed, é este o motivo.
func SlugCandidates(titulo string, ano, tmdbID int) []string {
	base := Slugify(titulo)

	candidatos := make([]string, 0, 3)
	candidatos = append(candidatos, base)
	if ano > 0 {
		candidatos = append(candidatos,
			base+"-"+strconv.Itoa(ano),
			base+"-"+strconv.Itoa(ano)+"-"+strconv.Itoa(tmdbID),
		)
	} else {
		candidatos = append(candidatos, base+"-"+strconv.Itoa(tmdbID))
	}
	return candidatos
}
