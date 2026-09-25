package podcast

import (
	"testing"
	"time"

	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/integrations/tmdb"
)

func TestParseDuration(t *testing.T) {
	cases := map[string]int{
		"":         0,
		"1800":     30, // segundos
		"30:00":    30, // MM:SS
		"45:30":    46, // arredonda
		"1:02:03":  62, // HH:MM:SS
		"00:29:30": 30, // arredonda 29:30 -> 30
		"abc":      0,  // ilegível
		"0":        0,
	}
	for in, want := range cases {
		if got := parseDuration(in); got != want {
			t.Errorf("parseDuration(%q) = %d, quer %d", in, got, want)
		}
	}
}

func TestParsePubDate(t *testing.T) {
	got := parsePubDate("Wed, 05 Feb 2025 08:00:00 -0300")
	if got.IsZero() {
		t.Fatal("parsePubDate retornou zero para data válida")
	}
	if got.Year() != 2025 || got.Month() != time.February || got.Day() != 5 {
		t.Errorf("parsePubDate = %v, quer 2025-02-05", got)
	}
	if !parsePubDate("data inválida").IsZero() {
		t.Error("parsePubDate deveria devolver zero para data inválida")
	}
}

func TestUpdatePeriodToFrequencia(t *testing.T) {
	cases := map[string]string{
		"daily":   "Diário",
		"WEEKLY":  "Semanal",
		"monthly": "Mensal",
		"yearly":  "Anual",
		"":        "",
		"random":  "",
	}
	for in, want := range cases {
		if got := updatePeriodToFrequencia(in); got != want {
			t.Errorf("updatePeriodToFrequencia(%q) = %q, quer %q", in, got, want)
		}
	}
}

func TestInferFrequency(t *testing.T) {
	base := time.Date(2025, 2, 5, 0, 0, 0, 0, time.UTC)

	weekly := make([]tmdb.Episode, 0, 5)
	for i := 0; i < 5; i++ {
		weekly = append(weekly, tmdb.Episode{PublicadoEm: base.AddDate(0, 0, -7*i)})
	}
	if got := inferFrequency(weekly); got != "Semanal" {
		t.Errorf("inferFrequency(semanal) = %q, quer Semanal", got)
	}

	monthly := make([]tmdb.Episode, 0, 4)
	for i := 0; i < 4; i++ {
		monthly = append(monthly, tmdb.Episode{PublicadoEm: base.AddDate(0, 0, -30*i)})
	}
	if got := inferFrequency(monthly); got != "Mensal" {
		t.Errorf("inferFrequency(mensal) = %q, quer Mensal", got)
	}

	if got := inferFrequency(nil); got != "" {
		t.Errorf("inferFrequency(nil) = %q, quer vazio", got)
	}
}

func TestParseFeed(t *testing.T) {
	const xml = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"
     xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"
     xmlns:sy="http://purl.org/rss/1.0/modules/syndication/">
  <channel>
    <title>O Switch</title>
    <description>Um podcast sobre &lt;b&gt;tecnologia&lt;/b&gt;.</description>
    <itunes:author>Fulano de Tal</itunes:author>
    <itunes:image href="https://cdn.exemplo.com/capa.jpg"/>
    <sy:updatePeriod>weekly</sy:updatePeriod>
    <itunes:category text="Technology"/>
    <item>
      <title>Episódio 2</title>
      <pubDate>Wed, 12 Feb 2025 08:00:00 -0300</pubDate>
      <itunes:duration>00:32:00</itunes:duration>
      <itunes:episode>2</itunes:episode>
    </item>
    <item>
      <title>O switch com 40 casos</title>
      <pubDate>Wed, 05 Feb 2025 08:00:00 -0300</pubDate>
      <itunes:duration>1800</itunes:duration>
      <itunes:episode>1</itunes:episode>
    </item>
  </channel>
</rss>`

	got, err := parseFeed([]byte(xml), podcastMeta{ID: 123, Genero: "Fallback"})
	if err != nil {
		t.Fatalf("parseFeed erro: %v", err)
	}
	if got.Titulo != "O Switch" {
		t.Errorf("Titulo = %q", got.Titulo)
	}
	if got.Frequencia != "Semanal" {
		t.Errorf("Frequencia = %q, quer Semanal", got.Frequencia)
	}
	if got.Sinopse != "Um podcast sobre tecnologia." {
		t.Errorf("Sinopse = %q (tags HTML deveriam ser removidas)", got.Sinopse)
	}
	if got.PosterPath != "https://cdn.exemplo.com/capa.jpg" {
		t.Errorf("PosterPath = %q", got.PosterPath)
	}
	if len(got.Generos) != 1 || got.Generos[0] != "Technology" {
		t.Errorf("Generos = %v", got.Generos)
	}
	if len(got.Creditos) != 1 || got.Creditos[0].Papel != "apresentacao" || got.Creditos[0].Nome != "Fulano de Tal" {
		t.Errorf("Creditos = %+v", got.Creditos)
	}
	if len(got.Episodios) != 2 {
		t.Fatalf("len(Episodios) = %d, quer 2", len(got.Episodios))
	}
	// Ordenado por data desc: episódio 2 primeiro.
	if got.Episodios[0].Numero != 2 || got.Episodios[0].DuracaoMin != 32 {
		t.Errorf("Episodios[0] = %+v", got.Episodios[0])
	}
	if got.Episodios[1].Numero != 1 || got.Episodios[1].DuracaoMin != 30 {
		t.Errorf("Episodios[1] = %+v", got.Episodios[1])
	}
	if got.Episodios[1].PublicadoEm.Format("2006-01-02") != "2025-02-05" {
		t.Errorf("Episodios[1].PublicadoEm = %v", got.Episodios[1].PublicadoEm)
	}
}
