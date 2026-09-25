package main

import "testing"

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"O Switch":             "o-switch",
		"Ficção Científica":    "ficcao-cientifica",
		"  Espaços   demais  ": "espacos-demais",
		"Ação & Aventura!":     "acao-aventura",
		"Coração de Leão":      "coracao-de-leao",
		"---já-com-hifens---":  "ja-com-hifens",
		"":                     "",
		"!!!":                  "",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, quer %q", in, got, want)
		}
	}
}

func TestShortHashStable(t *testing.T) {
	a := shortHash("podcast123")
	b := shortHash("podcast123")
	if a != b {
		t.Errorf("shortHash não é estável: %q != %q", a, b)
	}
	if a == shortHash("podcast124") {
		t.Error("shortHash colidiu para entradas distintas")
	}
	if len(a) < 6 {
		t.Errorf("shortHash muito curto: %q", a)
	}
}
