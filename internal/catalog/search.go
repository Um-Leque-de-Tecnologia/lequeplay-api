package catalog

import (
	"context"
	"fmt"
	"sort"
)

const (
	// retrieveN é quantos candidatos cada perna retorna antes da fusão.
	retrieveN = 100
	// rerankN é quantos candidatos fundidos passam pelo reranker.
	rerankN = 30
)

// SearchMode é o modo de busca solicitado.
type SearchMode string

const (
	// ModeAuto usa a busca híbrida (léxica + semântica) — o padrão.
	ModeAuto SearchMode = "auto"
	// ModeHybrid é explicitamente híbrido (RRF).
	ModeHybrid SearchMode = "hybrid"
	// ModeVector é puramente semântico (KNN por cosseno).
	ModeVector SearchMode = "vector"
	// ModeFTS é puramente léxico (full-text + trigram).
	ModeFTS SearchMode = "fts"
)

// GeminiEmbedder é o backend remoto de embedding (satisfeito por *gemini.Client).
type GeminiEmbedder interface {
	Enabled() bool
	Embed(ctx context.Context, query string) ([]float32, error)
}

// Reranker reordena candidatos por relevância (satisfeito por *gemini.Client).
type Reranker interface {
	Rerank(ctx context.Context, query string, docs []string) ([]float32, error)
}

// SearchEngine orquestra o embedding da consulta, as três buscas e o rerank.
type SearchEngine struct {
	repo          *Repo
	gemini        GeminiEmbedder
	reranker      Reranker
	rerankEnabled bool
}

// NewSearchEngine constrói o motor de busca. `reranker` pode ser nil.
func NewSearchEngine(repo *Repo, gem GeminiEmbedder, reranker Reranker, rerankEnabled bool) *SearchEngine {
	return &SearchEngine{repo: repo, gemini: gem, reranker: reranker, rerankEnabled: rerankEnabled}
}

// SearchResult é a resposta da busca.
type SearchResult struct {
	Query        string       `json:"query"`
	Modo         SearchMode   `json:"modo"`
	UsouFallback bool         `json:"usouFallback"`
	Itens        []SearchItem `json:"itens"`
}

// Search executa a busca no modo pedido. Em híbrido/auto/vetorial, embute a
// consulta com Gemini; se o Gemini estiver indisponível, cai para a perna léxica
// (FTS) e marca usouFallback.
func (e *SearchEngine) Search(ctx context.Context, query string, mode SearchMode, f Filter) (SearchResult, error) {
	if mode == "" {
		mode = ModeAuto
	}
	res := SearchResult{Query: query, Modo: mode}

	limit := f.Limite

	// Modo puramente léxico: não precisa de embedding.
	if mode == ModeFTS {
		items, err := e.repo.FTS(ctx, query, f, limit)
		if err != nil {
			return res, err
		}
		res.Itens = stampRanks(items, limit)
		return res, nil
	}

	// Demais modos precisam do vetor da consulta.
	qvec, err := e.embedQuery(ctx, query)
	if err != nil {
		// Sem vetor: degrada para FTS de forma honesta.
		items, ferr := e.repo.FTS(ctx, query, f, limit)
		if ferr != nil {
			return res, ferr
		}
		res.UsouFallback = true
		res.Modo = ModeFTS
		res.Itens = stampRanks(items, limit)
		return res, nil
	}

	if mode == ModeVector {
		items, err := e.repo.Vector(ctx, qvec, f, limit)
		if err != nil {
			return res, err
		}
		res.Itens = stampRanks(items, limit)
		return res, nil
	}

	// auto/hybrid: RRF + rerank opcional.
	candidateN := rerankN
	if limit > candidateN {
		candidateN = limit
	}
	items, err := e.repo.HybridRRF(ctx, query, qvec, f, retrieveN, candidateN)
	if err != nil {
		return res, err
	}
	if e.rerankEnabled {
		items = e.rerank(ctx, query, items)
	}
	res.Itens = stampRanks(items, limit)
	return res, nil
}

// embedQuery embute a consulta via Gemini. Retorna erro quando o Gemini não está
// configurado ou falha, para o chamador decidir o fallback.
func (e *SearchEngine) embedQuery(ctx context.Context, query string) ([]float32, error) {
	if e.gemini == nil || !e.gemini.Enabled() {
		return nil, fmt.Errorf("busca: embedding indisponível (gemini desabilitado)")
	}
	v, err := e.gemini.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("busca: embed da consulta: %w", err)
	}
	return v, nil
}

// rerank pontua os candidatos fundidos e os reordena por relevância. Sem reranker
// disponível (ou em qualquer erro), mantém a ordem RRF.
func (e *SearchEngine) rerank(ctx context.Context, query string, items []SearchItem) []SearchItem {
	if e.reranker == nil || len(items) == 0 {
		return items
	}
	texts := make([]string, len(items))
	for i, it := range items {
		texts[i] = rerankText(it)
	}
	scores, err := e.reranker.Rerank(ctx, query, texts)
	if err != nil || len(scores) != len(items) {
		return items
	}
	out := make([]SearchItem, len(items))
	copy(out, items)
	for i := range out {
		out[i].Score = float64(scores[i])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// stampRanks trunca para n itens e carimba um Rank 1-based em cada.
func stampRanks(items []SearchItem, n int) []SearchItem {
	if n > 0 && len(items) > n {
		items = items[:n]
	}
	for i := range items {
		items[i].Rank = i + 1
	}
	if items == nil {
		items = []SearchItem{}
	}
	return items
}
