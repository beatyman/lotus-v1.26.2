package apistruct

import (
	"bytes"
	"testing"
)

func TestNewAPIAdminToken(t *testing.T) {
	a, err := NewAPIAdminToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewAPIAdminToken()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Compare(a, b) == 0 {
		t.Fatal("need different screct")
	}
}
