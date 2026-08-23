package catalog

import "testing"

func TestIsUUID(t *testing.T) {
	cases := map[string]bool{
		"123e4567-e89b-12d3-a456-426614174000": true,
		"123E4567-E89B-12D3-A456-426614174000": true,
		"not-a-uuid":                           false,
		"123e4567e89b12d3a456426614174000":     false, // sem hífens
		"123e4567-e89b-12d3-a456-42661417400g": false, // caractere inválido
		"":                                     false,
	}
	for in, want := range cases {
		if got := isUUID(in); got != want {
			t.Errorf("isUUID(%q) = %v, quer %v", in, got, want)
		}
	}
}

func TestPosterURL(t *testing.T) {
	if got := posterURL(""); got != "" {
		t.Errorf("posterURL(\"\") = %q, quer vazio", got)
	}
	const path = "/abc.jpg"
	want := posterBase + path
	if got := posterURL(path); got != want {
		t.Errorf("posterURL(%q) = %q, quer %q", path, got, want)
	}
}

func TestStampRanks(t *testing.T) {
	items := []SearchItem{{Midia: Midia{Titulo: "a"}}, {Midia: Midia{Titulo: "b"}}, {Midia: Midia{Titulo: "c"}}}
	got := stampRanks(items, 2)
	if len(got) != 2 {
		t.Fatalf("len = %d, quer 2", len(got))
	}
	if got[0].Rank != 1 || got[1].Rank != 2 {
		t.Errorf("ranks = %d,%d, quer 1,2", got[0].Rank, got[1].Rank)
	}

	// nil vira slice vazia (não nula) para serializar como [].
	if empty := stampRanks(nil, 10); empty == nil {
		t.Error("stampRanks(nil) retornou nil, quer slice vazia")
	}
}

func TestFilterClause(t *testing.T) {
	next := 1
	var args []any
	clause := filterClause(Filter{Tipo: "filme", Ano: 2020}, &next, &args)
	if clause != " AND tipo = $1 AND ano = $2" {
		t.Errorf("clause = %q", clause)
	}
	if len(args) != 2 || args[0] != "filme" || args[1] != 2020 {
		t.Errorf("args = %v", args)
	}
	if next != 3 {
		t.Errorf("next = %d, quer 3", next)
	}
}
