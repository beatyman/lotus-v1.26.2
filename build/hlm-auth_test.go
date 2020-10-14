package build

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"
	"time"
)

func TestHlmAuth(t *testing.T) {
	if err := os.MkdirAll("/etc/lotus", 0755); err != nil {
		t.Fatal(err)
	}
	key := []byte(fmt.Sprintf("%d", time.Now().UnixNano()))
	if err := ioutil.WriteFile("/etc/lotus/auth.dat", key, 0600); err != nil {
		t.Fatal(err)
	}

	if IsHlmAuth(key) {
		t.Fatal("expect false, but it's true")
	}
	if !IsHlmAuth(key) {
		t.Fatal("expect true, but it's false")
	}

	if !IsHlmAuth(GetHlmAuth()) {
		t.Fatal("expect true, but it's false")
	}
}
