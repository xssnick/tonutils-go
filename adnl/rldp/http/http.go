package http

import "github.com/xssnick/tonutils-go/tl"

func init() {
	tl.Register(Request{}, "http.request id:int256 method:string url:string http_version:string headers:(vector http.header) = http.Response")
	tl.Register(Response{}, "http.response http_version:string status_code:int reason:string headers:(vector http.header) no_payload:Bool = http.Response")
	tl.Register(GetNextPayloadPart{}, "http.getNextPayloadPart id:int256 seqno:int max_chunk_size:int = http.PayloadPart")
	tl.Register(PayloadPart{}, "http.payloadPart data:bytes trailer:(vector http.header) last:Bool = http.PayloadPart")
	tl.Register(GetCapabilities{}, "http.proxy.getCapabilities capabilities:long = http.proxy.Capabilities")
	tl.Register(Capabilities{}, "http.proxy.capabilities capabilities:long = http.proxy.Capabilities")
}

type Request struct {
	ID      []byte   `tl:"int256"`
	Method  string   `tl:"string"`
	URL     string   `tl:"string"`
	Version string   `tl:"string"`
	Headers []Header `tl:"vector struct"`
}

type GetNextPayloadPart struct {
	ID           []byte `tl:"int256"`
	Seqno        int32  `tl:"int"`
	MaxChunkSize int32  `tl:"int"`
}

type Response struct {
	Version    string   `tl:"string"`
	StatusCode int32    `tl:"int"`
	Reason     string   `tl:"string"`
	Headers    []Header `tl:"vector struct"`
	NoPayload  bool     `tl:"bool"`
}

type PayloadPart struct {
	Data    []byte   `tl:"bytes"`
	Trailer []Header `tl:"vector struct"`
	IsLast  bool     `tl:"bool"`
}

type Header struct {
	Name  string `tl:"string"`
	Value string `tl:"string"`
}

type GetCapabilities struct {
	Capabilities int64 `tl:"long"`
}

type Capabilities struct {
	Value int64 `tl:"long"`
}

const CapabilityRLDP2 int64 = 1
