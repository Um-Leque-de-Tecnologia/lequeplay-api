package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// batchEndpoint é o template da URL REST batchEmbedContents.
const batchEndpoint = "https://generativelanguage.googleapis.com/v1beta/models/%s:batchEmbedContents?key=%s"

// MaxBatch é o maior número de documentos por chamada de batch.
const MaxBatch = 100

type batchRequest struct {
	Requests []embedRequest `json:"requests"`
}

type batchResponse struct {
	Embeddings []struct {
		Values []float32 `json:"values"`
	} `json:"embeddings"`
}

// EmbedDocumentsBatch embute até MaxBatch documentos numa única chamada usando o
// task type RETRIEVAL_DOCUMENT, retornando um vetor 768-dim normalizado (L2) por
// texto de entrada, em ordem. Usado offline pelo pipeline de seed (não pelo
// caminho de query ao vivo, que usa Embed com RETRIEVAL_QUERY). Tem timeout mais
// generoso porque chamadas de batch são mais pesadas.
func (c *Client) EmbedDocumentsBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if !c.Enabled() {
		return nil, ErrNotConfigured
	}
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	reqs := make([]embedRequest, len(texts))
	for i, t := range texts {
		reqs[i] = embedRequest{
			Model:                "models/" + model,
			Content:              content{Parts: []part{{Text: t}}},
			TaskType:             "RETRIEVAL_DOCUMENT",
			OutputDimensionality: Dim,
		}
	}

	body, err := json.Marshal(batchRequest{Requests: reqs})
	if err != nil {
		return nil, fmt.Errorf("gemini.EmbedDocumentsBatch marshal: %w", err)
	}

	url := fmt.Sprintf(batchEndpoint, model, c.APIKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gemini.EmbedDocumentsBatch request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpc := &http.Client{Timeout: 120 * time.Second}
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini.EmbedDocumentsBatch do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("gemini.EmbedDocumentsBatch status %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}

	var out batchResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("gemini.EmbedDocumentsBatch decode: %w", err)
	}
	if len(out.Embeddings) != len(texts) {
		return nil, fmt.Errorf("gemini.EmbedDocumentsBatch: %d embeddings para %d entradas", len(out.Embeddings), len(texts))
	}

	vecs := make([][]float32, len(texts))
	for i, e := range out.Embeddings {
		if len(e.Values) == 0 {
			return nil, fmt.Errorf("gemini.EmbedDocumentsBatch: embedding vazio em %d", i)
		}
		vecs[i] = l2Normalize(e.Values)
	}
	return vecs, nil
}
