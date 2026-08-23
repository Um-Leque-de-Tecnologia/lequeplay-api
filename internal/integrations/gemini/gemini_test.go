package gemini

import (
	"math"
	"testing"
)

func TestL2Normalize(t *testing.T) {
	v := []float32{3, 4} // norma 5
	got := l2Normalize(v)
	var sum float64
	for _, x := range got {
		sum += float64(x) * float64(x)
	}
	if math.Abs(math.Sqrt(sum)-1) > 1e-6 {
		t.Errorf("norma resultante = %v, quer 1", math.Sqrt(sum))
	}
	if math.Abs(float64(got[0])-0.6) > 1e-6 || math.Abs(float64(got[1])-0.8) > 1e-6 {
		t.Errorf("normalizado = %v, quer [0.6 0.8]", got)
	}
}

func TestL2NormalizeZero(t *testing.T) {
	v := []float32{0, 0}
	got := l2Normalize(v)
	if got[0] != 0 || got[1] != 0 {
		t.Errorf("vetor zero deve permanecer zero, veio %v", got)
	}
}

func TestEnabled(t *testing.T) {
	if New("").Enabled() {
		t.Error("client sem key deve estar desabilitado")
	}
	if !New("abc").Enabled() {
		t.Error("client com key deve estar habilitado")
	}
}
