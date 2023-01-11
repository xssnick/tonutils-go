package rldp

import "github.com/xssnick/tonutils-go/tl"

func init() {
	tl.Register(FECRaptorQ{}, "fec.raptorQ data_size:int symbol_size:int symbols_count:int = fec.Type")
	tl.Register(FECRoundRobin{}, "fec.roundRobin data_size:int symbol_size:int symbols_count:int = fec.Type")
	tl.Register(FECOnline{}, "fec.online data_size:int symbol_size:int symbols_count:int = fec.Type")
}

type FECRaptorQ struct {
	DataSize     int32 `tl:"int"`
	SymbolSize   int32 `tl:"int"`
	SymbolsCount int32 `tl:"int"`
}

type FECRoundRobin struct {
	DataSize     int32 `tl:"int"`
	SymbolSize   int32 `tl:"int"`
	SymbolsCount int32 `tl:"int"`
}

type FECOnline struct {
	DataSize     int32 `tl:"int"`
	SymbolSize   int32 `tl:"int"`
	SymbolsCount int32 `tl:"int"`
}
