package catalog

import "testing"

// Estes testes são o contrato do slug. O mesmo algoritmo existe em SQL, em
// db/migrations — se algum caso aqui mudar, o espelho muda no mesmo PR, senão o
// slug que a migration grava para de bater com o que o seed calcula e o link
// publicado do título deixa de resolver.

// TestSlugifyExemplosDaRegra cobre os quatro exemplos que precisam valer
// idênticos nos dois lados, Go e SQL.
func TestSlugifyExemplosDaRegra(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Homem-Aranha: Um Novo Dia", "homem-aranha-um-novo-dia"},
		{"Ficção Científica", "ficcao-cientifica"},
		{"Coração & Alma", "coracao-alma"},
		{"  Lei & Ordem  ", "lei-ordem"},
	}
	for _, c := range cases {
		if got := Slugify(c.in); got != c.want {
			t.Errorf("Slugify(%q) = %q, quer %q", c.in, got, c.want)
		}
	}
}

func TestSlugifyRegras(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// Regra 5: não sobrou nada aproveitável.
		{"", "sem-titulo"},
		{"   ", "sem-titulo"},
		{"!!!", "sem-titulo"},
		{"— & —", "sem-titulo"},
		{"-------", "sem-titulo"},

		// Título que já é slug passa intacto: Slugify é idempotente, então
		// re-slugificar um slug gravado não muda a URL.
		{"homem-aranha-um-novo-dia", "homem-aranha-um-novo-dia"},
		{"lei-ordem", "lei-ordem"},

		// Regra 1: maiúsculas viram minúsculas.
		{"LEI E ORDEM", "lei-e-ordem"},

		// Regra 1 antes da regra 2: acento em maiúscula também tem de cair.
		{"CORAÇÃO", "coracao"},
		{"CORAÇÃO & ALMA", "coracao-alma"},
		{"Ódio", "odio"},
		{"ÑOÑO", "nono"},

		// Regra 4: pontuação repetida colapsa num hífen só e as pontas somem.
		{"--Ordem--", "ordem"},
		{"Lei   &&&  Ordem", "lei-ordem"},
		{"...Fim.", "fim"},

		// Regra 3: dígitos sobrevivem, o resto vira hífen.
		{"Blade Runner 2049", "blade-runner-2049"},
		{"1917", "1917"},

		// Rune fora do mapa de acentos não tem letra base: cai na regra 3 e vira
		// hífen, igual a qualquer outro caractere inválido.
		{"Amélie 東京", "amelie"},
		{"Café ☕ Bar", "cafe-bar"},
	}
	for _, c := range cases {
		if got := Slugify(c.in); got != c.want {
			t.Errorf("Slugify(%q) = %q, quer %q", c.in, got, c.want)
		}
	}
}

// TestSlugifyMapaDeAcentos trava o mapa da regra 2 inteiro, na ordem exata em que
// o translate() do espelho em SQL o escreve. É o teste que pega uma letra
// esquecida de um lado só.
func TestSlugifyMapaDeAcentos(t *testing.T) {
	const (
		minusculas = "áàâãäéèêëíìîïóòôõöúùûüçñ"
		maiusculas = "ÁÀÂÃÄÉÈÊËÍÌÎÏÓÒÔÕÖÚÙÛÜÇÑ"
		want       = "aaaaaeeeeiiiiooooouuuucn"
	)
	if got := Slugify(minusculas); got != want {
		t.Errorf("Slugify(acentuadas minúsculas) = %q, quer %q", got, want)
	}
	// A regra 1 roda antes da regra 2, então a versão maiúscula tem de dar o
	// mesmo resultado. No SQL isso vale pelo motivo inverso: lá o translate() vem
	// antes do lower() e o mapa cobre as maiúsculas. O resultado é que bate.
	if got := Slugify(maiusculas); got != want {
		t.Errorf("Slugify(acentuadas maiúsculas) = %q, quer %q", got, want)
	}
}

// TestSlugifyPorRuneNaoPorByte guarda a decisão de percorrer por rune: "ç" ocupa
// dois bytes em UTF-8 e iterar por byte partiria o caractere no meio.
func TestSlugifyPorRuneNaoPorByte(t *testing.T) {
	const in = "çãé"
	const want = "cae"
	if len(in) == len([]rune(in)) {
		t.Fatal("premissa do teste falhou: a entrada deveria ser multibyte")
	}
	if got := Slugify(in); got != want {
		t.Errorf("Slugify(%q) = %q, quer %q — iterar por byte corrompe multibyte", in, got, want)
	}
}

func TestSlugCandidates(t *testing.T) {
	cases := []struct {
		nome   string
		titulo string
		ano    int
		tmdbID int
		want   []string
	}{
		{
			nome:   "com ano, os três candidatos",
			titulo: "Homem-Aranha",
			ano:    2021,
			tmdbID: 634649,
			want:   []string{"homem-aranha", "homem-aranha-2021", "homem-aranha-2021-634649"},
		},
		{
			// Sem ano, o candidato do meio é pulado E o último não leva "-0-".
			// Tem de sair igual ao ramo ELSE do CASE que monta cand_tmdb no
			// espelho em db/migrations.
			nome:   "ano 0 pula o do meio e o último não leva o ano",
			titulo: "Homem-Aranha",
			ano:    0,
			tmdbID: 634649,
			want:   []string{"homem-aranha", "homem-aranha-634649"},
		},
		{
			nome:   "ano negativo conta como ausente",
			titulo: "Homem-Aranha",
			ano:    -1,
			tmdbID: 634649,
			want:   []string{"homem-aranha", "homem-aranha-634649"},
		},
		{
			// A base sai do Slugify, então a regra 5 vale aqui também.
			nome:   "título sem nada aproveitável cai em sem-titulo",
			titulo: "!!!",
			ano:    1999,
			tmdbID: 42,
			want:   []string{"sem-titulo", "sem-titulo-1999", "sem-titulo-1999-42"},
		},
		{
			nome:   "acento na base vale para todos os candidatos",
			titulo: "Ficção Científica",
			ano:    2020,
			tmdbID: 7,
			want:   []string{"ficcao-cientifica", "ficcao-cientifica-2020", "ficcao-cientifica-2020-7"},
		},
	}

	for _, c := range cases {
		t.Run(c.nome, func(t *testing.T) {
			got := SlugCandidates(c.titulo, c.ano, c.tmdbID)
			if len(got) != len(c.want) {
				t.Fatalf("SlugCandidates(%q, %d, %d) = %v, quer %v", c.titulo, c.ano, c.tmdbID, got, c.want)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Errorf("candidato[%d] = %q, quer %q", i, got[i], c.want[i])
				}
			}
		})
	}
}

// TestSlugCandidatesOrdem garante o "do mais curto ao mais específico": quem
// grava percorre a fatia na ordem e fica com o primeiro livre, então o candidato
// mais curto tem de vir primeiro e cada um seguinte tem de ser prefixado pelo
// anterior.
func TestSlugCandidatesOrdem(t *testing.T) {
	const titulo = "Coração & Alma"
	got := SlugCandidates(titulo, 2019, 123)
	if len(got) != 3 {
		t.Fatalf("SlugCandidates = %v, quer 3 candidatos", got)
	}
	if got[0] != Slugify(titulo) {
		t.Errorf("primeiro candidato = %q, quer a base %q", got[0], Slugify(titulo))
	}
	for i := 1; i < len(got); i++ {
		if len(got[i]) <= len(got[i-1]) {
			t.Errorf("candidato[%d] = %q não é mais específico que %q", i, got[i], got[i-1])
		}
	}
}
