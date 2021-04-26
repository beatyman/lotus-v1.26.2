package client

import (
	"context"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"

	"github.com/filecoin-project/lotus/cmd/lotus-storage/utils"
	"github.com/gwaylib/errors"
)

var (
	ErrAuthFailed = errors.New("auth failed")
)

type AuthClient struct {
	Host   string
	Md5Pwd string
}

func NewAuthClient(host, md5pwd string) *AuthClient {
	// TODO: check the host format
	return &AuthClient{Host: host, Md5Pwd: md5pwd}
}

func (auth *AuthClient) Check(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://"+auth.Host+"/check", nil)
	if err != nil {
		return nil, errors.As(err)
	}
	req.SetBasicAuth("hlm-miner", auth.Md5Pwd)
	resp, err := utils.HttpsClient.Do(req)
	if err != nil {
		return nil, errors.As(err)
	}
	defer resp.Body.Close()
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.As(err)
	}
	if resp.StatusCode != 200 {
		return nil, errors.Parse(string(respBody)).As(resp.StatusCode)
	}
	return respBody, nil
}

func (auth *AuthClient) changeAuth(ctx context.Context, passwd string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", "https://"+auth.Host+"/sys/auth/change", nil)
	if err != nil {
		return nil, errors.As(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("hlm-miner", passwd)

	resp, err := utils.HttpsClient.Do(req)
	if err != nil {
		return nil, errors.As(err)
	}
	defer resp.Body.Close()
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.As(err)
	}
	switch resp.StatusCode {
	case 200:
		return respBody, nil
	case 401:
		// auth failed
		return nil, ErrAuthFailed.As(resp.StatusCode)
	}
	return nil, errors.Parse(string(respBody)).As(resp.StatusCode)
}
func (auth *AuthClient) ChangeAuth(ctx context.Context) ([]byte, error) {
	key, err := auth.changeAuth(ctx, auth.Md5Pwd)
	if err == nil {
		return key, nil
	}
	if !ErrAuthFailed.Equal(err) {
		return nil, errors.As(err)
	}

	// try with default keys
	return auth.changeAuth(ctx, "d41d8cd98f00b204e9800998ecf8427e")
}

func (auth *AuthClient) NewFileToken(ctx context.Context, sid string) ([]byte, error) {
	params := url.Values{}
	params.Add("sid", sid)
	req, err := http.NewRequestWithContext(ctx, "GET", "https://"+auth.Host+"/sys/file/token?"+params.Encode(), nil)
	if err != nil {
		return nil, errors.As(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("hlm-miner", auth.Md5Pwd)

	resp, err := utils.HttpsClient.Do(req)
	if err != nil {
		return nil, errors.As(err)
	}
	defer resp.Body.Close()
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.As(err)
	}
	if resp.StatusCode != 200 {
		return nil, errors.Parse(string(respBody)).As(resp.StatusCode)
	}
	return respBody, nil
}

func (auth *AuthClient) DelayFileToken(ctx context.Context, sid string) ([]byte, error) {
	params := url.Values{}
	params.Add("sid", sid)
	req, err := http.NewRequestWithContext(ctx, "POST", "https://"+auth.Host+"/sys/file/token", strings.NewReader(params.Encode()))
	if err != nil {
		return nil, errors.As(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("hlm-miner", auth.Md5Pwd)

	resp, err := utils.HttpsClient.Do(req)
	if err != nil {
		return nil, errors.As(err)
	}
	defer resp.Body.Close()
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.As(err)
	}
	if resp.StatusCode != 200 {
		return nil, errors.Parse(string(respBody)).As(resp.StatusCode)
	}
	return respBody, nil
}

func (auth *AuthClient) DeleteFileToken(ctx context.Context, sid string) ([]byte, error) {
	params := url.Values{}
	params.Add("sid", sid)
	req, err := http.NewRequestWithContext(ctx, "DELETE", "https://"+auth.Host+"/sys/file/token?"+params.Encode(), nil)
	if err != nil {
		return nil, errors.As(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("hlm-miner", auth.Md5Pwd)

	resp, err := utils.HttpsClient.Do(req)
	if err != nil {
		return nil, errors.As(err)
	}
	defer resp.Body.Close()
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.As(err)
	}
	if resp.StatusCode != 200 {
		return nil, errors.Parse(string(respBody)).As(resp.StatusCode)
	}
	return respBody, nil
}
