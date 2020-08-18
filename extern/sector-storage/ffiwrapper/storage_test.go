package ffiwrapper

import (
	"testing"

	"github.com/filecoin-project/lotus/extern/sector-storage/database"
)

func TestCheckSealed(t *testing.T) {
	repo := "/data/sdb/lotus-user-1/.lotusstorage"
	database.InitDB(repo)
	if err := CheckSealed(repo); err != nil {
		t.Fatal(err)
	}
}
