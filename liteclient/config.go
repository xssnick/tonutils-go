package liteclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
)

var (
	ErrNoConnections = errors.New("no connections established")
)

func GetConfigFromUrl(ctx context.Context, url string) (*GlobalConfig, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
        req.Header.Set("Accept", "*/*")
	req = req.WithContext(ctx)

	httpClient := http.DefaultClient

	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer res.Body.Close()

	config := &GlobalConfig{}
	if err := json.NewDecoder(res.Body).Decode(config); err != nil {
		return nil, err
	}

	return config, nil
}

func GetConfigFromFile(filepath string) (*GlobalConfig, error) {
	config := &GlobalConfig{}

	configData, err := os.ReadFile(filepath)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(configData, config); err != nil {
		return nil, err
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
