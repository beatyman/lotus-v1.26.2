package auth

import (
	"fmt"
	"io/ioutil"
	"testing"
	"time"
)

func TestHlmAuth(t *testing.T) {
	key := fmt.Sprintf("%d", time.Now().UnixNano())
	pwd := fmt.Sprintf("%d", time.Now().UnixNano())
	if err := ioutil.WriteFile("./auth.dat", []byte(fmt.Sprintf("%s:%s", key, pwd)), 0600); err != nil {
		t.Fatal(err)
	}

	if err := loadHlmAuth("./auth.dat"); err != nil {
		t.Fatal(err)
	}

	if !IsHlmAuth(key, []byte(pwd)) {
		t.Fatal("expect true, but it's false")
	}
	if !IsHlmAuth(key, HlmAuthPwd(key)) {
		t.Fatal("expect true, but it's false")
	}
}
