package overlay

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/xssnick/tonutils-go/tl"
)

func TestWrapUnwrapQueryAndMessage(t *testing.T) {
	overlayID := bytes.Repeat([]byte{0x11}, 32)
	payload := tl.Raw([]byte{1, 2, 3})

	qObj, qOver := UnwrapQuery(WrapQuery(overlayID, payload))
	if !reflect.DeepEqual(qObj, payload) {
		t.Fatalf("unexpected unwrapped query payload")
	}
	if !bytes.Equal(qOver, overlayID) {
		t.Fatalf("unexpected query overlay id")
	}

	mObj, mOver := UnwrapMessage(WrapMessage(overlayID, payload))
	if !reflect.DeepEqual(mObj, payload) {
		t.Fatalf("unexpected unwrapped message payload")
	}
	if !bytes.Equal(mOver, overlayID) {
		t.Fatalf("unexpected message overlay id")
	}
}

func TestUnwrapInvalidInput(t *testing.T) {
	cases := []tl.Serializable{
		nil,
		tl.Raw([]byte{1}),
		[]tl.Serializable{Query{Overlay: bytes.Repeat([]byte{1}, 32)}},
		[]tl.Serializable{Message{Overlay: bytes.Repeat([]byte{1}, 32)}},
		[]tl.Serializable{Message{Overlay: bytes.Repeat([]byte{1}, 32)}, tl.Raw([]byte{1})},
		[]tl.Serializable{Query{Overlay: bytes.Repeat([]byte{1}, 32)}, tl.Raw([]byte{1})},
	}

	for i, in := range cases {
		obj, over := UnwrapQuery(in)
		if i == len(cases)-1 {
			if obj == nil || over == nil {
				t.Fatalf("case %d: expected query unwrap to work", i)
			}
		} else if obj != nil || over != nil {
			t.Fatalf("case %d: expected query unwrap nil,nil", i)
		}

		obj, over = UnwrapMessage(in)
		if i == len(cases)-2 {
			if obj == nil || over == nil {
				t.Fatalf("case %d: expected message unwrap to work", i)
			}
		} else if obj != nil || over != nil {
			t.Fatalf("case %d: expected message unwrap nil,nil", i)
		}
	}
}
