package funcs_test

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm"
	funcs "github.com/xssnick/tonutils-go/tvm/op/funcs"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestVMBuildsRegisteredFuncsOps(t *testing.T) {
	vmx := tvm.NewTVM()
	if vmx == nil {
		t.Fatal("NewTVM returned nil")
	}

	ops := []vm.OP{
		funcs.NOW(),
		funcs.SENDRAWMSG(),
		funcs.SETCP(0),
		funcs.SETCPX(),
		funcs.GETGASFEE(),
		funcs.GETSTORAGEFEE(),
		funcs.GETFORWARDFEE(),
		funcs.GETORIGINALFWDFEE(),
		funcs.GETGASFEESIMPLE(),
		funcs.GETFORWARDFEESIMPLE(),
		funcs.GETEXTRABALANCE(),
		funcs.SHA256U(),
		funcs.HASHEXT(0),
		funcs.HASHBU(),
		funcs.PREVBLOCKSINFOTUPLE(),
		funcs.UNPACKEDCONFIGTUPLE(),
		funcs.DUEPAYMENT(),
		funcs.PREVMCBLOCKS(),
		funcs.PREVKEYBLOCK(),
		funcs.PREVMCBLOCKS_100(),
		funcs.INMSGPARAMS(),
		funcs.INMSG_BOUNCE(),
		funcs.INMSG_BOUNCED(),
		funcs.INMSG_SRC(),
		funcs.INMSG_FWDFEE(),
		funcs.INMSG_LT(),
		funcs.INMSG_UTIME(),
		funcs.INMSG_ORIGVALUE(),
		funcs.INMSG_VALUE(),
		funcs.INMSG_VALUEEXTRA(),
		funcs.INMSG_STATEINIT(),
		funcs.INMSGPARAM(3),
		funcs.GETPRECOMPILEDGAS(),
		funcs.RANDU256(),
		funcs.RAND(),
		funcs.SETRAND(),
		funcs.ADDRAND(),
	}

	for i, op := range ops {
		if op == nil {
			t.Fatalf("op %d is nil", i)
		}
		if len(op.GetPrefixes()) == 0 {
			t.Fatalf("op %d returned no prefixes", i)
		}
		if op.SerializeText() == "" {
			t.Fatalf("op %d returned empty text form", i)
		}
	}
}
