package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

func BHFetch(remoteHost string, file string, output string, offerConfirmation bool) error {
	fetchUrl, err := url.Parse(fmt.Sprintf("%+v/api/file_opt/fetch", remoteHost))
	if err != nil {
		log.Errorf("[BH] parse url failed url: %v,err: %v", fetchUrl, err)
		return err
	}
	v := url.Values{}
	v.Add("file", file)
	v.Add("offer_confirmation", "true")
	fetchUrl.RawQuery = v.Encode()

	req, err := http.NewRequest(http.MethodGet, fetchUrl.String(), nil)
	if err != nil {
		log.Errorf("[BH] NewRequest failed err: %v", err)
		return err
	}
	req.Close = true
	req.Header = http.Header{}
	cli := http.Client{}
	cli.Timeout = time.Hour * 24

	resp, err := cli.Do(req)
	if err != nil {
		log.Errorf("[BH] request http failed err: %v", err)
		return err
	}
	if resp.StatusCode >= 500 {
		log.Errorf("[BH] %v:%v access:%v, err: %v", resp.Status, resp.StatusCode, fetchUrl, err)
		return err
	}

	if 400 <= resp.StatusCode && resp.StatusCode < 500 {
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Errorf("[BH] %v:%v access:%v, err: %v", resp.Status, resp.StatusCode, fetchUrl, err)
			return err
		}
		log.Errorf("[BH] %v:%v access:%v, body: %v", resp.Status, resp.StatusCode, fetchUrl, string(body))
		return err
	}
	outputPath, err := os.Create(output)
	if err != nil {
		log.Errorf("[BH] %v:%v access:%v, body: %v", resp.Status, resp.StatusCode, fetchUrl,output)
		return err
	}
	defer outputPath.Close()
	if _, err := io.Copy(outputPath, resp.Body); err != nil {
		log.Errorf("[BH] %v:%v access:%v, body: %v", resp.Status, resp.StatusCode, fetchUrl, output)
		return err
	}
	return nil
}

func BHConfirm(remoteHost string, file string, key string) error {
	revertUrl, err := url.Parse(fmt.Sprintf("%+v/api/file_opt/confirm", remoteHost))
	if err != nil {
		log.Errorf("[BH] parse url failed url: %v,err: %v", revertUrl, err)
		return err
	}
	v := url.Values{}
	v.Add("file", file)
	v.Add("key", key)
	revertUrl.RawQuery = v.Encode()

	req, err := http.NewRequest(http.MethodPut, revertUrl.String(), nil)
	if err != nil {
		log.Errorf("[BH] NewRequest failed err: %v", err)
		return err
	}

	req.Close = true
	req.Header = http.Header{}

	cli := http.Client{}
	cli.Timeout = time.Second * 30
	resp, err := cli.Do(req)
	if err != nil {
		log.Errorf("[BH] request http failed err: %v", err)
		return err
	}

	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		log.Errorf("[BH] %v:%v access:%v, err: %v", resp.Status, resp.StatusCode, revertUrl, err)
		return err
	}

	if 400 <= resp.StatusCode && resp.StatusCode < 500 {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Errorf("[BH] %v:%v access:%v, err: %v", resp.Status, resp.StatusCode, revertUrl, err)
			return err
		}
		log.Errorf("[BH] %v:%v access:%v, body: %v", resp.Status, resp.StatusCode, revertUrl, string(body))
		return err
	}
	return nil
}

//{"file_remote":"http://10.76.1.51:1229","file_id":70315}

type BTFileOpt struct {
	FileRemote string `json:"file_remote"`
	FileId     string `json:"file_id"`
}

func ParseFileOpt(remoteFileUrl string) (*BTFileOpt, error) {
	var opt BTFileOpt
	if err := json.Unmarshal([]byte(remoteFileUrl), &opt); err != nil {
		return &BTFileOpt{}, err
	}
	return &opt, nil
}