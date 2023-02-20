package storage

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/dht"
	"github.com/xssnick/tonutils-go/adnl/overlay"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"math"
	"reflect"
	"sync"
	"time"
)

type DHT interface {
	StoreAddress(ctx context.Context, addresses address.List, ttl time.Duration, ownerKey ed25519.PrivateKey, copies int) (int, []byte, error)
	FindAddresses(ctx context.Context, key []byte) (*address.List, ed25519.PublicKey, error)
	FindOverlayNodes(ctx context.Context, overlayId []byte, continuation ...*dht.Continuation) (*overlay.NodesList, *dht.Continuation, error)
	Close()
}

type FileInfo struct {
	Size            uint64
	FromPiece       uint32
	ToPiece         uint32
	FromPieceOffset uint32
	ToPieceOffset   uint32
}

type TorrentDownloader interface {
	ListFiles() []string
	GetFileOffsets(name string) *FileInfo
	DownloadPiece(ctx context.Context, pieceIndex uint32) (_ []byte, err error)
	SetDesiredMinNodesNum(num int)
	Close()
}

type Client struct {
	dht DHT
}

func NewClient(dht DHT) *Client {
	return &Client{
		dht: dht,
	}
}

type torrentDownloader struct {
	bagId      []byte
	piecesNum  uint32
	filesIndex map[string]uint32
	dirName    string

	desiredMinPeersNum int
	threadsPerPeer     int

	info        *TorrentInfo
	header      *TorrentHeader
	client      *Client
	knownNodes  map[string]*overlay.Node
	activeNodes map[string]*storageNode

	mx sync.RWMutex

	pieceQueue chan *pieceRequest

	globalCtx      context.Context
	downloadCancel func()
}

type pieceResponse struct {
	index int32
	data  []byte
	err   error
}

type pieceRequest struct {
	index  int32
	ctx    context.Context
	result chan<- pieceResponse
}

type storageNode struct {
	torrent   *torrentDownloader
	sessionId int64
	rawAdnl   overlay.ADNL
	rldp      *overlay.RLDPOverlayWrapper

	globalCtx context.Context
}

// fec_info_none#c82a1964 = FecInfo;
//
// torrent_header#9128aab7
//   files_count:uint32
//   tot_name_size:uint64
//   tot_data_size:uint64
//   fec:FecInfo
//   dir_name_size:uint32
//   dir_name:(dir_name_size * [uint8])
//   name_index:(files_count * [uint64])
//   data_index:(files_count * [uint64])
//   names:(file_names_size * [uint8])
//   data:(tot_data_size * [uint8])
//     = TorrentHeader;
//
// Filename rules:
// 1) Name can't be empty
// 2) Names in a torrent should be unique
// 3) Name can't start or end with '/' or contain two consequitive '/'
// 4) Components of name can't be equal to "." or ".."
// 5) If there's a name aaa/bbb/ccc, no other name can start with aaa/bbb/ccc/

// torrent_info piece_size:uint32 file_size:uint64 root_hash:(## 256) header_size:uint64 header_hash:(## 256)
//              description:Text = TorrentInfo;

type TorrentInfo struct {
	PieceSize  uint32 `tlb:"## 32"`
	FileSize   uint64 `tlb:"## 64"`
	RootHash   []byte `tlb:"bits 256"`
	HeaderSize uint64 `tlb:"## 64"`
	HeaderHash []byte `tlb:"bits 256"`
}

func (c *Client) CreateDownloader(ctx context.Context, bagId []byte, desiredMinPeersNum, threadsPerPeer int) (_ TorrentDownloader, err error) {
	globalCtx, downloadCancel := context.WithCancel(context.Background())
	var dow = &torrentDownloader{
		client:             c,
		bagId:              bagId,
		activeNodes:        map[string]*storageNode{},
		knownNodes:         map[string]*overlay.Node{},
		globalCtx:          globalCtx,
		downloadCancel:     downloadCancel,
		pieceQueue:         make(chan *pieceRequest, 10),
		desiredMinPeersNum: desiredMinPeersNum,
		threadsPerPeer:     threadsPerPeer,
	}
	defer func() {
		if err != nil {
			downloadCancel()
		}
	}()

	// connect to first node
	err = dow.scale(1)
	if err != nil {
		err = fmt.Errorf("failed to find storage nodes for this bag, err: %w", err)
		return nil, err
	}

	if dow.info.PieceSize == 0 || dow.info.HeaderSize == 0 {
		err = fmt.Errorf("incorrect torrent info sizes")
		return nil, err
	}
	if dow.info.HeaderSize > 20*1024*1024 {
		err = fmt.Errorf("too big header > 20 MB, looks dangerous")
		return nil, err
	}
	go dow.nodesController()

	hdrPieces := dow.info.HeaderSize / uint64(dow.info.PieceSize)
	if dow.info.HeaderSize%uint64(dow.info.PieceSize) > 0 {
		// add not full piece
		hdrPieces += 1
	}

	dow.piecesNum = uint32(dow.info.FileSize / uint64(dow.info.PieceSize))
	if dow.piecesNum%dow.info.PieceSize > 0 {
		dow.piecesNum += 1
	}

	data := make([]byte, 0, hdrPieces*uint64(dow.info.PieceSize))
	for i := uint32(0); i < uint32(hdrPieces); i++ {
		piece, pieceErr := dow.DownloadPiece(ctx, i)
		if pieceErr != nil {
			err = fmt.Errorf("failed to get header piece %d, err: %w", i, pieceErr)
			return nil, err
		}
		data = append(data, piece...)
		// TODO: data piece part save
	}

	var header TorrentHeader
	data, err = tl.Parse(&header, data, true)
	if err != nil {
		err = fmt.Errorf("failed to load header from cell, err: %w", err)
		return nil, err
	}
	dow.header = &header

	dow.dirName = string(header.DirName)
	if header.FilesCount > 1_000_000 {
		return nil, fmt.Errorf("bag has > 1_000_000 files, looks dangerous")
	}
	if uint32(len(header.NameIndex)) != header.FilesCount ||
		uint32(len(header.DataIndex)) != header.FilesCount {
		err = fmt.Errorf("corrupted header, lack of files info")
		return nil, err
	}

	dow.filesIndex = map[string]uint32{}
	for i := uint32(0); i < header.FilesCount; i++ {
		if uint64(len(header.Names)) < header.NameIndex[i] {
			err = fmt.Errorf("corrupted header, too short names data")
			return nil, err
		}
		if dow.info.FileSize < header.DataIndex[i]+dow.info.HeaderSize {
			err = fmt.Errorf("corrupted header, data out of range")
			return nil, err
		}

		nameFrom := uint64(0)
		if i > 0 {
			nameFrom = header.NameIndex[i-1]
		}
		name := header.Names[nameFrom:header.NameIndex[i]]
		dow.filesIndex[string(name)] = i
	}

	return dow, nil
}

func (s *storageNode) Close() {
	s.rawAdnl.Close()
}

func (s *storageNode) loop() {
	defer s.Close()

	fails := 0
	for {
		var req *pieceRequest
		select {
		case <-s.globalCtx.Done():
			return
		case req = <-s.torrent.pieceQueue:
		}

		resp := pieceResponse{
			index: req.index,
		}

		var piece Piece
		resp.err = func() error {
			reqCtx, cancel := context.WithTimeout(req.ctx, 7*time.Second)
			err := s.rldp.DoQuery(reqCtx, int64(s.torrent.info.PieceSize)*3, &GetPiece{req.index}, &piece)
			cancel()
			if err != nil {
				return fmt.Errorf("failed to query piece %d. err: %w", req.index, err)
			}

			proof, err := cell.FromBOC(piece.Proof)
			if err != nil {
				return fmt.Errorf("failed to parse BoC of piece %d, err: %w", req.index, err)
			}

			err = cell.CheckProof(proof, s.torrent.info.RootHash)
			if err != nil {
				return fmt.Errorf("proof check of piece %d failed: %w", req.index, err)
			}

			err = s.torrent.checkProofBranch(proof, piece.Data, uint32(req.index))
			if err != nil {
				return fmt.Errorf("proof branch check of piece %d failed: %w", req.index, err)
			}
			return nil
		}()
		if resp.err == nil {
			fails = 0
			resp.data = piece.Data
		} else {
			fails++
		}
		req.result <- resp

		if fails > 3 {
			// something wrong, close connection, we should reconnect after it
			return
		}
	}
}

func (t *torrentDownloader) connectToNode(ctx context.Context, adnlID []byte, node *overlay.Node, onDisconnect func()) (*storageNode, error) {
	addrs, keyN, err := t.client.dht.FindAddresses(ctx, adnlID)
	if err != nil {
		return nil, fmt.Errorf("failed to find node address: %w", err)
	}

	ax, err := adnl.Connect(ctx, addrs.Addresses[0].IP.String()+":"+fmt.Sprint(addrs.Addresses[0].Port), keyN, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connnect to node: %w", err)
	}
	extADNL := overlay.CreateExtendedADNL(ax)
	rl := overlay.CreateExtendedRLDP(rldp.NewClientV2(extADNL)).CreateOverlay(node.Overlay)

	var sessionReady = make(chan int64, 1)
	var setReady sync.Once
	rl.SetOnQuery(func(transferId []byte, query *rldp.Query) error {
		ctx, cancel := context.WithTimeout(t.globalCtx, 500*time.Second)
		defer cancel()

		switch q := query.Data.(type) {
		case Ping:
			err = rl.SendAnswer(ctx, query.MaxAnswerSize, query.ID, transferId, &Pong{})
			if err != nil {
				return err
			}

			setReady.Do(func() {
				var status Ok
				err = rl.DoQuery(ctx, 1<<25, &AddUpdate{
					SessionID: q.SessionID,
					Seqno:     1,
					Update: UpdateInit{
						HavePieces:       nil,
						HavePiecesOffset: 0,
						State: State{
							WillUpload:   false,
							WantDownload: true,
						},
					},
				}, &status)
				if err != nil {
					// we will try again on next ping
					setReady = sync.Once{}
					return
				}

				sessionReady <- q.SessionID
			})
		case AddUpdate:
			// do nothing with this info for now, just ok
			err = rl.SendAnswer(ctx, query.MaxAnswerSize, query.ID, transferId, &Ok{})
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("unexpected rldp query received by storage cliet: %s", reflect.ValueOf(q).String())
		}
		return nil
	})

	var res TorrentInfoContainer
	err = rl.DoQuery(ctx, 1<<25, &GetTorrentInfo{}, &res)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent info: %w", err)
	}

	cl, err := cell.FromBOC(res.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse torrent info boc: %w", err)
	}

	if !bytes.Equal(cl.Hash(), t.bagId) {
		return nil, fmt.Errorf("incorrect torrent info")
	}

	nodeCtx, cancel := context.WithCancel(t.globalCtx)
	stNode := &storageNode{
		rawAdnl:   ax,
		rldp:      rl,
		globalCtx: nodeCtx,
		torrent:   t,
	}
	rl.SetOnDisconnect(func() {
		cancel()
		onDisconnect()
	})

	t.mx.Lock()
	if t.info == nil {
		var info TorrentInfo
		err = tlb.LoadFromCell(&info, cl.BeginParse())
		if err != nil {
			t.mx.Unlock()
			return nil, fmt.Errorf("invalid torrent info cell")
		}
		t.info = &info
	}
	t.mx.Unlock()

	select {
	case id := <-sessionReady:
		stNode.sessionId = id
		return stNode, nil
	case <-ctx.Done():
		// close connection and all related overlays
		ax.Close()
		return nil, ctx.Err()
	}
}

func (t *torrentDownloader) GetFileOffsets(name string) *FileInfo {
	i, ok := t.filesIndex[name]
	if !ok {
		return nil
	}
	info := &FileInfo{}

	var end = t.header.DataIndex[i]
	var start uint64 = 0
	if i > 0 {
		start = t.header.DataIndex[i-1]
	}
	info.FromPiece = uint32((t.info.HeaderSize + start) / uint64(t.info.PieceSize))
	info.ToPiece = uint32((t.info.HeaderSize + end) / uint64(t.info.PieceSize))
	info.FromPieceOffset = uint32((t.info.HeaderSize + start) - uint64(info.FromPiece)*uint64(t.info.PieceSize))
	info.ToPieceOffset = uint32((t.info.HeaderSize + end) - uint64(info.ToPiece)*uint64(t.info.PieceSize))
	info.Size = (uint64(info.ToPiece-info.FromPiece)*uint64(t.info.PieceSize) + uint64(info.ToPieceOffset)) - uint64(info.FromPieceOffset)
	return info
}

// DownloadPiece - downloads piece from one of available nodes.
// Can be used concurrently to download from multiple nodes in the same time
func (t *torrentDownloader) DownloadPiece(ctx context.Context, pieceIndex uint32) (_ []byte, err error) {
	resp := make(chan pieceResponse, 1)
	req := pieceRequest{
		index:  int32(pieceIndex),
		ctx:    ctx,
		result: resp,
	}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case t.pieceQueue <- &req:
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case result := <-resp:
			if result.err != nil {
				continue
				// return nil, fmt.Errorf("failed to query piece %d after retries: %w", pieceIndex, err)
			}
			return result.data, nil
		}
	}
}

func (t *torrentDownloader) checkProofBranch(proof *cell.Cell, data []byte, piece uint32) error {
	if piece >= t.piecesNum {
		return fmt.Errorf("too big piece")
	}

	tree, err := proof.BeginParse().LoadRef()
	if err != nil {
		return err
	}

	// calc tree depth
	depth := int(math.Log2(float64(t.piecesNum)))
	if t.piecesNum > uint32(math.Pow(2, float64(depth))) {
		// add 1 if pieces num is not exact log2
		depth++
	}

	// check bits from left to right and load branches
	for i := depth - 1; i >= 0; i-- {
		isLeft := piece&(1<<i) == 0

		b, err := tree.LoadRef()
		if err != nil {
			return err
		}

		if isLeft {
			tree = b
			continue
		}

		// we need right branch
		tree, err = tree.LoadRef()
		if err != nil {
			return err
		}
	}

	branchHash, err := tree.LoadSlice(256)
	if err != nil {
		return err
	}

	dataHash := sha256.New()
	dataHash.Write(data)
	if !bytes.Equal(branchHash, dataHash.Sum(nil)) {
		return fmt.Errorf("incorrect branch hash")
	}
	return nil
}

// scale - add more nodes to pool, to increase load speed and capacity
func (t *torrentDownloader) scale(num int) error {
	if num == 0 {
		return nil
	}

	var nodesDhtCont *dht.Continuation

	checkedNodes := map[string]bool{}
	attempts := 2
	for {
		for _, node := range t.knownNodes {
			adnlID, err := adnl.ToKeyID(node.ID)
			if err != nil {
				continue
			}
			id := hex.EncodeToString(adnlID)

			t.mx.RLock()
			isActive := t.activeNodes[id] != nil
			t.mx.RUnlock()
			// will not connect to already active node
			if isActive {
				continue
			}

			// will not try again to connect in this scale iteration
			if checkedNodes[id] {
				continue
			}
			checkedNodes[id] = true

			ctx, cancel := context.WithTimeout(t.globalCtx, 5*time.Second)
			stNode, err := t.connectToNode(ctx, adnlID, node, func() {
				t.mx.Lock()
				delete(t.activeNodes, id)
				t.mx.Unlock()
			})
			if err != nil {
				cancel()
				continue
			}

			var al overlay.NodesList
			err = stNode.rldp.DoQuery(ctx, 1<<25, &overlay.GetRandomPeers{}, &al)
			cancel()
			if err != nil {
				stNode.Close()
				continue
			}

			for _, n := range al.List {
				nodeId, err := adnl.ToKeyID(n.ID)
				if err != nil {
					continue
				}
				// add known nodes in case we will need them in future to scale
				t.knownNodes[hex.EncodeToString(nodeId)] = &n
			}

			for i := 0; i < t.threadsPerPeer; i++ {
				go stNode.loop()
			}

			t.mx.Lock()
			t.activeNodes[id] = stNode
			t.mx.Unlock()

			num--
			if num == 0 {
				return nil
			}
		}

		ctx, cancel := context.WithTimeout(t.globalCtx, 15*time.Second)

		var err error
		var nodes *overlay.NodesList

		nodes, nodesDhtCont, err = t.client.dht.FindOverlayNodes(ctx, t.bagId, nodesDhtCont)
		cancel()
		if err != nil {
			attempts--
			if attempts == 0 {
				return fmt.Errorf("no nodes found")
			}

			select {
			case <-t.globalCtx.Done():
				return t.globalCtx.Err()
			case <-time.After(1 * time.Second):
				continue
			}
		}

		for i := range nodes.List {
			id, err := adnl.ToKeyID(nodes.List[i].ID)
			if err != nil {
				continue
			}
			// add known nodes in case we will need them in future to scale
			t.knownNodes[hex.EncodeToString(id)] = &nodes.List[i]
		}
	}
}

func (t *torrentDownloader) nodesController() {
	for {
		select {
		case <-t.globalCtx.Done():
			return
		case <-time.After(1 * time.Second):
		}

		t.mx.RLock()
		peersNum := len(t.activeNodes)
		t.mx.RUnlock()

		if peersNum < t.desiredMinPeersNum {
			_ = t.scale(t.desiredMinPeersNum - peersNum)
		}
	}
}

func (t *torrentDownloader) SetDesiredMinNodesNum(num int) {
	t.desiredMinPeersNum = num
}

func (t *torrentDownloader) ListFiles() []string {
	files := make([]string, 0, len(t.filesIndex))
	for s := range t.filesIndex {
		files = append(files, s)
	}
	return files
}

func (t *torrentDownloader) Close() {
	t.downloadCancel()
}
