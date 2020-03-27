package main

import (
	"io/ioutil"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
)

const worker_id_file = "worker.id"

var (
	workerid   = ""
	workeridMu = sync.Mutex{}
)

func GetWorkerID(repo string) string {
	workeridMu.Lock()
	defer workeridMu.Unlock()

	if len(workerid) > 0 {
		return workerid
	}
	file := filepath.Join(repo, worker_id_file)
	// checkfile
	id, err := ioutil.ReadFile(file)
	if err == nil {
		workerid = strings.TrimSpace(string(id))
		return workerid
	}
	workerid = uuid.New().String()
	if err := ioutil.WriteFile(file, []byte(workerid), 0666); err != nil {
		// depend on this
		panic(err)
	}

	return workerid
}
