package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// rerankModel é um modelo generativo rápido usado como LLM-as-reranker: lê a
// consulta e todos os candidatos juntos (como um cross-encoder) e devolve uma
// ordenação por relevância.
const rerankModel = "gemini-flash-latest"

const generateEndpoint = "https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s"

// maxRerankDocChars limita quanto de cada candidato enviamos, mantendo o prompt
// pequeno e a chamada rápida.
const maxRerankDocChars = 500

type genRequest struct {
	Contents         []genContent        `json:"contents"`
	GenerationConfig genGenerationConfig `json:"generationConfig"`
}

type genContent struct {
	Parts []part `json:"parts"`
}

type genGenerationConfig struct {
	ResponseMimeType string          `json:"responseMimeType"`
	ResponseSchema   map[string]any  `json:"responseSchema"`
	Temperature      float32         `json:"temperature"`
	ThinkingConfig   *thinkingConfig `json:"thinkingConfig,omitempty"`
}

// thinkingConfig limita o raciocínio do modelo. Um orçamento pequeno (não-zero)
// mantém a latência baixa. gemini-flash-latest rejeita budget 0 (400), então
// limitamos em vez de desligar.
type thinkingConfig struct {
	ThinkingBudget int `json:"thinkingBudget"`
}

const thinkBudget = 128

type genResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

// Rerank ordena os documentos candidatos por relevância à consulta usando um
// modelo generativo do Gemini, retornando um score por documento de entrada
// (maior = melhor), alinhado à ordem de entrada. Em qualquer erro o chamador
// deve cair para a ordem pré-rerank (RRF).
func (c *Client) Rerank(ctx context.Context, query string, docs []string) ([]float32, error) {
	if !c.Enabled() {
		return nil, ErrNotConfigured
	}
	if len(docs) == 0 {
		return []float32{}, nil
	}

	var sb strings.Builder
	sb.WriteString("Você é um sistema de ranking de busca. Dada a BUSCA e a lista de CANDIDATOS (títulos: filmes, séries, podcasts), ordene os índices do MAIS relevante ao MENOS relevante para a intenção da busca. Inclua TODOS os índices exatamente uma vez.\n\n")
	fmt.Fprintf(&sb, "BUSCA: %s\n\nCANDIDATOS:\n", query)
	for i, d := range docs {
		if len(d) > maxRerankDocChars {
			d = d[:maxRerankDocChars]
		}
		fmt.Fprintf(&sb, "%d: %s\n", i, d)
	}
	sb.WriteString("\nResponda APENAS com um array JSON de inteiros (os índices) em ordem de relevância decrescente.")

	reqBody := genRequest{
		Contents: []genContent{{Parts: []part{{Text: sb.String()}}}},
		GenerationConfig: genGenerationConfig{
			ResponseMimeType: "application/json",
			ResponseSchema:   map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
			Temperature:      0,
			ThinkingConfig:   &thinkingConfig{ThinkingBudget: thinkBudget},
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("gemini.Rerank marshal: %w", err)
	}

	url := fmt.Sprintf(generateEndpoint, rerankModel, c.APIKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gemini.Rerank request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpc := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini.Rerank do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("gemini.Rerank status %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}

	var gr genResponse
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		return nil, fmt.Errorf("gemini.Rerank decode: %w", err)
	}
	if len(gr.Candidates) == 0 || len(gr.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("gemini.Rerank: resposta vazia")
	}

	var order []int
	if err := json.Unmarshal([]byte(gr.Candidates[0].Content.Parts[0].Text), &order); err != nil {
		return nil, fmt.Errorf("gemini.Rerank parse ordem %q: %w", gr.Candidates[0].Content.Parts[0].Text, err)
	}

	// Converte a ordenação em scores decrescentes alinhados à entrada. Docs não
	// ranqueados ficam com score 0 (sortam por último, preservando a ordem RRF).
	n := len(docs)
	scores := make([]float32, n)
	seen := make([]bool, n)
	pos := 0
	for _, idx := range order {
		if idx < 0 || idx >= n || seen[idx] {
			continue
		}
		seen[idx] = true
		scores[idx] = float32(n - pos)
		pos++
	}
	return scores, nil
}
