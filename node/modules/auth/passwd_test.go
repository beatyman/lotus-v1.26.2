package auth

import (
	"fmt"
	"testing"
)

func TestRandPlainText(t *testing.T) {
	fmt.Println(RandPlainText(96))
}
