package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"strconv"

	"github.com/gwaylib/log"
)

func ReportData(httpMethod string, reqData interface{}) ([]byte, error) {
	reqDataBytes, err := json.Marshal(reqData)
	if err != nil {
		log.Println(err)
	}

	log.Println("\nreqData => ", string(reqDataBytes))

	address := "http://10.1.30.14:10100/hlmmonitoring/v1/data/collect"
	req, err := http.NewRequest(httpMethod, address, bytes.NewBuffer(reqDataBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	// req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		log.Error(err)
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		// io.Copy(ioutil.Discard, resp.Body)
		respData, _ := ioutil.ReadAll(resp.Body)
		return nil, errors.New("status code: " + strconv.Itoa(resp.StatusCode) + ", msg: " + resp.Status + ", " + string(respData))
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	log.Println(string(body))

	return body, nil
}
