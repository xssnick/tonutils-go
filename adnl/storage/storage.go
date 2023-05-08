package storage

import (
	"encoding/binary"
	"fmt"
	"github.com/xssnick/tonutils-go/tl"
)

func init() {
	tl.Register(TorrentInfoContainer{}, "storage.torrentInfo data:bytes = storage.TorrentInfo")
	tl.Register(GetTorrentInfo{}, "storage.getTorrentInfo = storage.TorrentInfo")
	tl.Register(Piece{}, "storage.piece proof:bytes data:bytes = storage.Piece")
	tl.Register(GetPiece{}, "storage.getPiece piece_id:int = storage.Piece")
	tl.Register(Ping{}, "storage.ping session_id:long = storage.Pong")
	tl.Register(Pong{}, "storage.pong = storage.Pong")
	tl.Register(AddUpdate{}, "storage.addUpdate session_id:long seqno:int update:storage.Update = Ok")
	tl.Register(State{}, "storage.state will_upload:Bool want_download:Bool = storage.State")
	tl.Register(UpdateInit{}, "storage.updateInit have_pieces:bytes have_pieces_offset:int state:storage.State = storage.Update")
	tl.Register(UpdateHavePieces{}, "storage.updateHavePieces piece_id:(vector int) = storage.Update")
	tl.Register(UpdateState{}, "storage.updateState state:storage.State = storage.Update")
	tl.Register(Ok{}, "storage.ok = Ok")

	tl.Register(FECInfoNone{}, "fec_info_none#c82a1964 = FecInfo")
	tl.Register(TorrentHeader{}, "torrent_header#9128aab7 files_count:uint32 "+
		"tot_name_size:uint64 tot_data_size:uint64 fec:FecInfo "+
		"dir_name_size:uint32 dir_name:(dir_name_size * [uint8]) "+
		"name_index:(files_count * [uint64]) data_index:(files_count * [uint64]) "+
		"names:(file_names_size * [uint8]) data:(tot_data_size * [uint8]) "+
		"= TorrentHeader")
}

type AddUpdate struct {
	SessionID int64 `tl:"long"`
	Seqno     int64 `tl:"int"`
	Update    any   `tl:"struct boxed [storage.updateInit,storage.updateHavePieces,storage.updateState]"`
}

type TorrentInfoContainer struct {
	Data []byte `tl:"bytes"`
}

type GetTorrentInfo struct{}

type Piece struct {
	Proof []byte `tl:"bytes"`
	Data  []byte `tl:"bytes"`
}

type GetPiece struct {
	PieceID int32 `tl:"int"`
}

type Ping struct {
	SessionID int64 `tl:"long"`
}

type Pong struct{}

type State struct {
	WillUpload   bool `tl:"bool"`
	WantDownload bool `tl:"bool"`
}

type UpdateInit struct {
	HavePieces       []byte `tl:"bytes"`
	HavePiecesOffset int32  `tl:"int"`
	State            State  `tl:"struct boxed"`
}

type UpdateHavePieces struct {
	PieceIDs []int32 `tl:"vector int"`
}

type UpdateState struct {
	State State `tl:"struct boxed"`
}

type Ok struct{}

type FECInfoNone struct{}

type TorrentHeader struct {
	FilesCount    uint32
	TotalNameSize uint64
	TotalDataSize uint64
	FEC           FECInfoNone
	DirNameSize   uint32
	DirName       []byte
	NameIndex     []uint64
	DataIndex     []uint64
	Names         []byte
	Data          []byte
}

func (t *TorrentHeader) Parse(data []byte) (_ []byte, err error) {
	// Manual parse because of not standard array definition
	if len(data) < 28 {
		return nil, fmt.Errorf("too short sizes data to parse")
	}
	t.FilesCount = binary.LittleEndian.Uint32(data)
	data = data[4:]
	t.TotalNameSize = binary.LittleEndian.Uint64(data)
	data = data[8:]
	t.TotalDataSize = binary.LittleEndian.Uint64(data)
	data = data[8:]
	data, err = tl.Parse(&t.FEC, data, true)
	if err != nil {
		return nil, fmt.Errorf("failed to parse fec: %w", err)
	}
	t.DirNameSize = binary.LittleEndian.Uint32(data)
	data = data[4:]

	if uint64(len(data)) < uint64(t.DirNameSize)+uint64(t.FilesCount*8*2)+t.TotalNameSize+t.TotalDataSize {
		return nil, fmt.Errorf("too short arrays data to parse")
	}

	t.DirName = data[:t.DirNameSize]
	data = data[t.DirNameSize:]

	for i := uint32(0); i < t.FilesCount; i++ {
		t.NameIndex = append(t.NameIndex, binary.LittleEndian.Uint64(data[i*8:]))
		t.DataIndex = append(t.DataIndex, binary.LittleEndian.Uint64(data[t.FilesCount*8+i*8:]))
	}
	data = data[t.FilesCount*8*2:]

	t.Names = data[:t.TotalNameSize]
	data = data[t.TotalNameSize:]
	t.Data = data[:t.TotalDataSize]
	data = data[t.TotalDataSize:]
	return data, nil
}

func (t *TorrentHeader) Serialize() ([]byte, error) {
	data := make([]byte, 20)
	binary.LittleEndian.PutUint32(data[0:], t.FilesCount)
	binary.LittleEndian.PutUint64(data[4:], t.TotalNameSize)
	binary.LittleEndian.PutUint64(data[12:], t.TotalDataSize)

	fecData, err := tl.Serialize(t.FEC, true)
	if err != nil {
		return nil, err
	}
	data = append(data, fecData...)

	if t.DirNameSize != uint32(len(t.DirName)) {
		return nil, fmt.Errorf("incorrect dir name size")
	}

	dataDirNameSz := make([]byte, 4)
	binary.LittleEndian.PutUint32(dataDirNameSz, t.DirNameSize)
	data = append(data, dataDirNameSz...)
	data = append(data, t.DirName...)

	for _, ni := range t.NameIndex {
		iData := make([]byte, 8)
		binary.LittleEndian.PutUint64(iData, ni)
		data = append(data, iData...)
	}

	for _, ni := range t.DataIndex {
		iData := make([]byte, 8)
		binary.LittleEndian.PutUint64(iData, ni)
		data = append(data, iData...)
	}
	data = append(data, t.Names...)
	data = append(data, t.Data...)

	return data, nil
}
