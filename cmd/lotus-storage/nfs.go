// need install nfs-server
package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io/ioutil"
	"os/exec"
	"strings"

	"github.com/gwaylib/errors"
)

func ExportToNFS(repo string) error {
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
		info := strings.TrimSpace(line[0])
		if strings.Index(info, repo+" ") < 0 {
			continue
		}
		exist = info
	}
	export := fmt.Sprintf("%s *(ro,sync,insecure,no_root_squash)", repo)
	if len(exist) == 0 {
		exportsData = append(exportsData, []byte(export)...)
		exportsData = append(exportsData, []byte("\n")...)
		if err := ioutil.WriteFile("/etc/exports", exportsData, 0600); err != nil {
			return errors.As(err)
		}
		return exec.Command("systemctl", "reload", "nfs-server").Run()
	}
	return nil
}
