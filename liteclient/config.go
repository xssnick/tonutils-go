package liteclient

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

var (
	ErrNoConnections = errors.New("no connections established")
)

type HttpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

func GetConfigFromUrl(url string, httpClient *http.Client, req *http.Request) (GlobalConfig, error) {
	if req == nil {
		var err error
		req, err = http.NewRequest("GET", url, nil)
		if err != nil {
			return GlobalConfig{}, err
		}
	}

	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	res, err := httpClient.Do(req)
	if err != nil {
		return GlobalConfig{}, err
	}

	defer res.Body.Close()

	config := GlobalConfig{}
	if err := json.NewDecoder(res.Body).Decode(&config); err != nil {
		return GlobalConfig{}, err
	}

	return config, nil
}

func intToIP4(ipInt int64) string {
	b0 := strconv.FormatInt((ipInt>>24)&0xff, 10)
	b1 := strconv.FormatInt((ipInt>>16)&0xff, 10)
	b2 := strconv.FormatInt((ipInt>>8)&0xff, 10)
	b3 := strconv.FormatInt((ipInt & 0xff), 10)
	return b0 + "." + b1 + "." + b2 + "." + b3
}
