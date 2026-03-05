package overlay

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"reflect"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/tl"
)

type mockADNL struct {
	queryHandler      func(msg *adnl.MessageQuery) error
	customHandler     func(msg *adnl.MessageCustom) error
	disconnectHandler func(addr string, key ed25519.PublicKey)

	sendCustomCalls []tl.Serializable
	queryCalls      []tl.Serializable

	queryErr       error
	queryResponder func(req tl.Serializable, result tl.Serializable) error
	sendCustomErr  error

	id        []byte
	remote    string
	closerCtx context.Context
}

func newMockADNL() *mockADNL {
	return &mockADNL{closerCtx: context.Background(), id: []byte{1, 2, 3}, remote: "mock"}
}

func (m *mockADNL) SetCustomMessageHandler(handler func(msg *adnl.MessageCustom) error) {
	m.customHandler = handler
}

func (m *mockADNL) SetQueryHandler(handler func(msg *adnl.MessageQuery) error) {
	m.queryHandler = handler
}

func (m *mockADNL) SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey)) {
	m.disconnectHandler = handler
}

func (m *mockADNL) GetDisconnectHandler() func(addr string, key ed25519.PublicKey) {
	return m.disconnectHandler
}

func (m *mockADNL) SendCustomMessage(ctx context.Context, req tl.Serializable) error {
	m.sendCustomCalls = append(m.sendCustomCalls, req)
	return m.sendCustomErr
}

func (m *mockADNL) Query(ctx context.Context, req, result tl.Serializable) error {
	m.queryCalls = append(m.queryCalls, req)
	if m.queryErr != nil {
		return m.queryErr
	}
	if m.queryResponder != nil {
		return m.queryResponder(req, result)
	}
	return nil
}

func (m *mockADNL) Answer(ctx context.Context, queryID []byte, result tl.Serializable) error {
	return nil
}

func (m *mockADNL) GetCloserCtx() context.Context {
	return m.closerCtx
}

func (m *mockADNL) RemoteAddr() string {
	return m.remote
}

func (m *mockADNL) GetID() []byte {
	return m.id
}

func (m *mockADNL) Close() {}

type mockRLDP struct {
	adnl          rldp.ADNL
	onQuery       func(transferId []byte, query *rldp.Query) error
	onDisconnect  func()
	doQueryFn     func(ctx context.Context, maxAnswerSize uint64, query, result tl.Serializable) error
	doQueryAsync  func(ctx context.Context, maxAnswerSize uint64, id []byte, query tl.Serializable, result chan<- rldp.AsyncQueryResult) error
	doQueryCalls  []tl.Serializable
	sendAnswerErr error
}

func newMockRLDP(adnl rldp.ADNL) *mockRLDP {
	return &mockRLDP{adnl: adnl}
}

func (m *mockRLDP) GetADNL() rldp.ADNL {
	return m.adnl
}

func (m *mockRLDP) GetRateInfo() (left int64, total int64) {
	return 0, 0
}

func (m *mockRLDP) Close() {}

func (m *mockRLDP) DoQuery(ctx context.Context, maxAnswerSize uint64, query, result tl.Serializable) error {
	m.doQueryCalls = append(m.doQueryCalls, query)
	if m.doQueryFn != nil {
		return m.doQueryFn(ctx, maxAnswerSize, query, result)
	}
	return nil
}

func (m *mockRLDP) DoQueryAsync(ctx context.Context, maxAnswerSize uint64, id []byte, query tl.Serializable, result chan<- rldp.AsyncQueryResult) error {
	if m.doQueryAsync != nil {
		return m.doQueryAsync(ctx, maxAnswerSize, id, query, result)
	}
	return nil
}

func (m *mockRLDP) SetOnQuery(handler func(transferId []byte, query *rldp.Query) error) {
	m.onQuery = handler
}

func (m *mockRLDP) SetOnDisconnect(handler func()) {
	m.onDisconnect = handler
}

func (m *mockRLDP) SendAnswer(ctx context.Context, maxAnswerSize uint64, timeoutAt uint32, queryId, transferId []byte, answer tl.Serializable) error {
	return m.sendAnswerErr
}

func setSerializableResult(dst tl.Serializable, src tl.Serializable) error {
	dv := reflect.ValueOf(dst)
	if dv.Kind() != reflect.Ptr || dv.IsNil() {
		return fmt.Errorf("destination must be a non nil pointer")
	}
	val := reflect.ValueOf(src)
	if !val.Type().AssignableTo(dv.Elem().Type()) {
		return fmt.Errorf("cannot assign %s to %s", val.Type(), dv.Elem().Type())
	}
	dv.Elem().Set(val)
	return nil
}
