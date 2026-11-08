package postgres

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/example/realestate-collector/internal/model"
	"github.com/example/realestate-collector/internal/normalize"
	"testing"
)

func TestContentHashStable(t *testing.T) {
	l := model.Listing{Source: "x", ExternalID: "1", Title: "A"}
	a := sha256.Sum256([]byte(normalize.Hashable(l)))
	b := sha256.Sum256([]byte(normalize.Hashable(l)))
	if hex.EncodeToString(a[:]) != hex.EncodeToString(b[:]) {
		t.Fatal("unstable")
	}
}
