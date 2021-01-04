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
	key := fmt.Sprintf("%d", time.Now().UnixNano())
	pwd := fmt.Sprintf("%d", time.Now().UnixNano())
	data := []string{key, pwd}
	if err := ioutil.WriteFile("/etc/lotus/auth.dat", []byte(fmt.Sprintf("%s:%s", data[0], data[1])), 0600); err != nil {
		t.Fatal(err)
	}

	if IsHlmAuth(data) {
		t.Fatal("expect false, but it's true")
	}

	if err := LoadHlmAuth(); err != nil {
		t.Fatal(err)
	}
	if !IsHlmAuth(GetHlmAuth(key)) {
		t.Fatal("expect true, but it's false")
	}
}
