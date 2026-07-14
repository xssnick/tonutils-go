package dht

import (
	"bytes"
	"sync/atomic"
	"testing"
	"time"
)

func newBucketTestNode(id byte) *dhtNode {
	adnlID := make([]byte, 32)
	adnlID[31] = id
	return &dhtNode{adnlId: adnlID, failedFrom: time.Now().UnixNano()}
}

func bucketHasNode(nodes dhtNodeList, id []byte) bool {
	for _, node := range nodes {
		if node != nil && bytes.Equal(node.adnlId, id) {
			return true
		}
	}
	return false
}

func bucketBackupNodes(b *Bucket) dhtNodeList {
	b.mx.RLock()
	defer b.mx.RUnlock()

	return append(dhtNodeList{}, b.backup...)
}

func TestBucketAddActiveDoesNotEvictFullActive(t *testing.T) {
	bucket := newBucket(2)
	first := newBucketTestNode(1)
	second := newBucketTestNode(2)
	incoming := newBucketTestNode(3)

	bucket.addNode(first, true)
	bucket.addNode(second, true)
	bucket.addNode(incoming, true)

	active := bucket.getActiveNodes()
	if len(active) != 2 {
		t.Fatalf("expected 2 active nodes, got %d", len(active))
	}
	if !bucketHasNode(active, first.adnlId) || !bucketHasNode(active, second.adnlId) {
		t.Fatal("existing active nodes were evicted")
	}
	if bucketHasNode(active, incoming.adnlId) {
		t.Fatal("incoming node replaced a full active bucket")
	}

	backup := bucketBackupNodes(bucket)
	if !bucketHasNode(backup, incoming.adnlId) {
		t.Fatal("incoming node was not kept in backup")
	}
}

func TestBucketBackupDoesNotEvictReadyPeer(t *testing.T) {
	bucket := newBucket(1)
	active := newBucketTestNode(1)
	readyBackup := newBucketTestNode(2)
	incoming := newBucketTestNode(3)

	readyBackup.markPingSuccess()

	bucket.addNode(active, true)
	bucket.addNode(readyBackup, false)
	bucket.addNode(incoming, true)

	backup := bucketBackupNodes(bucket)
	if len(backup) != 1 {
		t.Fatalf("expected 1 backup node, got %d", len(backup))
	}
	if !bucketHasNode(backup, readyBackup.adnlId) {
		t.Fatal("ready backup peer was evicted")
	}
	if bucketHasNode(backup, incoming.adnlId) {
		t.Fatal("incoming peer replaced ready backup peer")
	}
}

func TestBucketBackupReplacesStaleUnreadyPeer(t *testing.T) {
	bucket := newBucket(1)
	active := newBucketTestNode(1)
	staleBackup := newBucketTestNode(2)
	incoming := newBucketTestNode(3)

	atomic.StoreInt64(&staleBackup.failedFrom, time.Now().Add(-2*backupReplaceAfter).UnixNano())

	bucket.addNode(active, true)
	bucket.addNode(staleBackup, false)
	bucket.addNode(incoming, true)

	backup := bucketBackupNodes(bucket)
	if len(backup) != 1 {
		t.Fatalf("expected 1 backup node, got %d", len(backup))
	}
	if bucketHasNode(backup, staleBackup.adnlId) {
		t.Fatal("stale unready backup peer was not replaced")
	}
	if !bucketHasNode(backup, incoming.adnlId) {
		t.Fatal("incoming peer did not replace stale unready backup peer")
	}
}

func TestBucketPromoteReadyDemotesUnreadyActive(t *testing.T) {
	bucket := newBucket(2)
	readyActive := newBucketTestNode(1)
	unreadyActive := newBucketTestNode(2)
	readyBackup := newBucketTestNode(3)

	readyActive.markPingSuccess()
	readyBackup.markPingSuccess()

	bucket.active = dhtNodeList{readyActive, unreadyActive}
	bucket.backup = dhtNodeList{readyBackup}

	bucket.promoteReady()

	active := bucket.getActiveNodes()
	if len(active) != 2 {
		t.Fatalf("expected 2 active nodes, got %d", len(active))
	}
	if !bucketHasNode(active, readyActive.adnlId) || !bucketHasNode(active, readyBackup.adnlId) {
		t.Fatal("ready backup was not promoted into freed active slot")
	}
	if bucketHasNode(active, unreadyActive.adnlId) {
		t.Fatal("unready active peer stayed in active bucket")
	}
}
