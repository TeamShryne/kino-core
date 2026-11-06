package pow

import (
	"context"
	"encoding/hex"
	"testing"
)

func TestHashV1CapturedDigest(t *testing.T) {
	got := HashV1([]byte("bcfc97aab4ceae4ae42f_1785644803019_20048"))
	if hex.EncodeToString(got[:]) != "5bedddfb9f9efd5f252d9e7cbe72dbd0af274a4c3b01a0acf181d2f600b135d4" {
		t.Fatalf("unexpected DeepSeekHashV1 digest: %x", got)
	}
}

func TestSolveCapturedChallenge(t *testing.T) {
	got, err := Solve(context.Background(), "bcfc97aab4ceae4ae42f", 1785644803019,
		"5bedddfb9f9efd5f252d9e7cbe72dbd0af274a4c3b01a0acf181d2f600b135d4", 20050)
	if err != nil || got != 20048 {
		t.Fatalf("Solve() = %d, %v; want 20048", got, err)
	}
}
