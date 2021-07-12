// need install nfs-server
package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gwaylib/errors"
)

func ExportToNFS(repo string) error {
	repoAbs, err := filepath.Abs(repo)
	if err != nil {
		return errors.As(err, repo)
	}
	exportsData, err := ioutil.ReadFile("/etc/exports")
	if err != nil {
		return errors.As(err)
	}
	r := csv.NewReader(bytes.NewReader(exportsData))
	r.Comment = '#'

	oriRecords, err := r.ReadAll()
	if err != nil {
		return errors.As(err, repo)
	}
	exist := ""
	for _, line := range oriRecords {
		if len(line) == 0 {
			continue
		}
		exports := strings.Split(strings.TrimSpace(line[0]), " ")
		if len(exports) == 0 {
			continue
		}
		exportPath, err := filepath.Abs(exports[0])
		if err != nil {
			return errors.As(err, repo)
		}
		if exportPath != repoAbs {
			continue
		}
		exist = exportPath
	}
	if len(exist) == 0 {
		export := fmt.Sprintf("%s *(ro,sync,insecure,no_root_squash)", repoAbs)
		exportsData = append(exportsData, []byte(export)...)
		exportsData = append(exportsData, []byte("\n")...)
		if err := ioutil.WriteFile("/etc/exports", exportsData, 0600); err != nil {
			return errors.As(err)
		}
		return exec.Command("systemctl", "reload", "nfs-server").Run()
	}
	return nil
}
