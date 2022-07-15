package liteclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestGetConfigFromUrl(t *testing.T) {
	type args struct {
		url      string
		response string
	}
	tests := []struct {
		name    string
		args    args
		want    *GlobalConfig
		wantErr bool
	}{
		{
			name: "basic",
			args: args{
				url: "http://test.url",
				response: `{
					"liteservers": [{
						"ip": 1,
						"port": 2,
						"id": {
							"@type": "testtype",
							"key": "testkey"
						}
					}]
				}`,
			},
			want: &GlobalConfig{
				Liteservers: []LiteserverConfig{
					{
						IP:   1,
						Port: 2,
						ID: ServerID{
							Type: "testtype",
							Key:  "testkey",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "error json",
			args: args{
				url: "http://test.url",
				response: `{
					"liteservers": [{
						"ip: 1,
						"port": 2,
						"id": {
							"@type": "testtype",
							"key": "testkey"
						}
					}]
				}`,
			},
			want:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := getMockClient(tt.args.response)
			defer server.Close()
			got, err := GetConfigFromUrl(context.Background(), server.URL)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetConfigFromUrl() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetConfigFromUrl() = %v, want %v", got, tt.want)
			}
		})
	}
}

func getMockClient(response string) *httptest.Server {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(response))
	})
	server := httptest.NewServer(handler)
	return server
}
