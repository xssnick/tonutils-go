package adnl

import (
	"bytes"
	"sync"
)

// packetHeaderPad is the zero prefix an outbound packet reserves for its
// routing header, which can only be filled in after the body is encrypted:
// 64 bytes on a channel (channel id + checksum), 96 on a root packet (peer id
// + ephemeral public key + checksum).
var packetHeaderPad [96]byte

// A packet is assembled in a pooled staging buffer and copied out exactly
// sized. The copy is what makes the pool safe: the datagram we return is
// handed to the net manager, which may keep it queued past our return (see
// SyncConn.WriteTo), so the staging array can never be handed over -- only
// reused. In exchange every packet used to burn three allocations of a fixed
// 1648 bytes (the 64-byte seed slice, the bytes.Buffer, and the MaxMTU grow)
// no matter how small the packet actually was.
var packetBuildPool = sync.Pool{
	New: func() any { return bytes.NewBuffer(make([]byte, 0, MaxMTU)) },
}

// Packets above the MTU are rejected by send and rebuilt smaller, so a staging
// buffer that outgrew this is a one-off; drop it instead of pinning it.
const maxPooledPacketBufCap = 2 * MaxMTU

func getPacketBuild(headerSize int) *bytes.Buffer {
	buf := packetBuildPool.Get().(*bytes.Buffer)
	buf.Reset()
	buf.Write(packetHeaderPad[:headerSize])
	return buf
}

func putPacketBuild(buf *bytes.Buffer) {
	if buf.Cap() > maxPooledPacketBufCap {
		return
	}
	packetBuildPool.Put(buf)
}

// messageBuildPool stages the TL body of an outbound message. tl.Serialize
// without a buffer starts from a fresh DefaultSerializeBufferSize slice on
// every call, and that body is only ever read once, to be copied into the
// packet being built.
var messageBuildPool = sync.Pool{
	New: func() any { return bytes.NewBuffer(make([]byte, 0, BasePayloadMTU)) },
}

// packetContentPool keeps the self-referential PacketContent backing storage
// off the per-datagram allocation path. The compiler has to move PacketContent
// to the heap because its optional fields point into the struct itself, while a
// channel packet is parsed and consumed synchronously before process returns.
var packetContentPool = sync.Pool{
	New: func() any { return new(PacketContent) },
}

// A body cannot legitimately exceed HugePacketMaxSz; anything that grew past
// it is not worth keeping around.
const maxPooledMessageBufCap = 2 * HugePacketMaxSz

func getMessageBuild() *bytes.Buffer {
	buf := messageBuildPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

func putMessageBuild(buf *bytes.Buffer) {
	if buf.Cap() > maxPooledMessageBufCap {
		return
	}
	messageBuildPool.Put(buf)
}

func getPacketContent() *PacketContent {
	return packetContentPool.Get().(*PacketContent)
}

func putPacketContent(packet *PacketContent) {
	*packet = PacketContent{}
	packetContentPool.Put(packet)
}
