// Package gemini é o backend de embeddings da busca semântica: chama a API REST
// do Gemini (gemini-embedding-001) diretamente. Também expõe um rerank
// LLM-as-reranker opcional via generateContent. Como a Anthropic não oferece
// embeddings, o Gemini é o backend primário — o mesmo usado no projeto irmão
// demo_gdgjp.
package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
)

// ErrNotConfigured sinaliza ausência de API key; a busca cai para o modo sem vetor.
var ErrNotConfigured = errors.New("gemini: sem API key configurada")

// Dim é a dimensionalidade de saída que pedimos (truncada por MRL, normalizada por nós).
const Dim = 768

// model é o id do modelo de embedding. gemini-embedding-001 suporta truncação
// Matryoshka via outputDimensionality mas NÃO re-normaliza abaixo de 3072 dims,
// então normalizamos (L2) o vetor retornado.
const model = "gemini-embedding-001"

// endpoint é o template da URL REST embedContent (API key como query param).
const endpoint = "https://generativelanguage.googleapis.com/v1beta/models/%s:embedContent?key=%s"

// Client chama o endpoint de embeddings do Gemini.
type Client struct {
	APIKey string
	HTTP   *http.Client
}

// New retorna um client. Com key vazia, Embed retorna ErrNotConfigured e o
// chamador segue sem a perna vetorial.
func New(apiKey string) *Client {
	return &Client{
		APIKey: apiKey,
		HTTP:   &http.Client{Timeout: 8 * time.Second},
	}
}

// Enabled indica se o caminho Gemini pode ser usado.
func (c *Client) Enabled() bool { return c != nil && c.APIKey != "" }

// embedRequest é o corpo JSON de models/*:embedContent.
type embedRequest struct {
	Model                string  `json:"model"`
	Content              content `json:"content"`
	TaskType             string  `json:"taskType"`
	OutputDimensionality int     `json:"outputDimensionality"`
}

type content struct {
	Parts []part `json:"parts"`
}

type part struct {
	Text string `json:"text"`
}

type embedResponse struct {
	Embedding struct {
		Values []float32 `json:"values"`
	} `json:"embedding"`
}

// Embed retorna um embedding de consulta 768-dim, normalizado (L2). Usa o task
// type RETRIEVAL_QUERY (o catálogo foi embutido como RETRIEVAL_DOCUMENT), de modo
// que os espaços de query e documento se alinham para a busca por cosseno.
func (c *Client) Embed(ctx context.Context, query string) ([]float32, error) {
	if !c.Enabled() {
		return nil, ErrNotConfigured
	}

	body, err := json.Marshal(embedRequest{
		Model:                "models/" + model,
		Content:              content{Parts: []part{{Text: query}}},
		TaskType:             "RETRIEVAL_QUERY",
		OutputDimensionality: Dim,
	})
	if err != nil {
		return nil, fmt.Errorf("gemini.Embed marshal: %w", err)
	}

	url := fmt.Sprintf(endpoint, model, c.APIKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gemini.Embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini.Embed do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("gemini.Embed status %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}

	var out embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("gemini.Embed decode: %w", err)
	}
	if len(out.Embedding.Values) == 0 {
		return nil, errors.New("gemini.Embed: embedding vazio na resposta")
	}

	return l2Normalize(out.Embedding.Values), nil
}

// l2Normalize escala um vetor para norma unitária. Retorna a entrada inalterada
// quando a norma é zero (degenerado, mas evita NaN poluindo a busca por cosseno).
func l2Normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	norm := math.Sqrt(sum)
	if norm == 0 {
		return v
	}
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(float64(x) / norm)
	}
	return out
}
