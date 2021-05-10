package utils

import (
	"crypto/tls"
	"net/http"
)

var HttpsClient = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
