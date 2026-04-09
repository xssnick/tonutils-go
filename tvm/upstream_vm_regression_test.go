package tvm

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
)

type upstreamVMRegressionCase struct {
	name string
	code *cell.Cell
}

const upstreamVMRegressionGasLimit = int64(1000)

func rawCodeCellFromHex(t *testing.T, src string) *cell.Cell {
	t.Helper()

	data, err := hex.DecodeString(src)
	if err != nil {
		t.Fatalf("decode hex failed: %v", err)
	}

	return cell.BeginCell().MustStoreSlice(data, uint(len(data))*8).EndCell()
}

func rawCodeCellFromBase64(t *testing.T, src string) *cell.Cell {
	t.Helper()

	data, err := base64.StdEncoding.DecodeString(src)
	if err != nil {
		t.Fatalf("decode base64 failed: %v", err)
	}
	if len(data) > 127 {
		data = data[:127]
	}

	return cell.BeginCell().MustStoreSlice(data, uint(len(data))*8).EndCell()
}

func upstreamVMRegressionCases(t *testing.T) []upstreamVMRegressionCase {
	t.Helper()

	// Selected from cppnode/ton/crypto/test/vm.cpp.
	return []upstreamVMRegressionCase{
		{name: "memory_leak_old", code: rawCodeCellFromHex(t, "90787FDB3B")},
		{name: "memory_leak", code: rawCodeCellFromHex(t, "90707FDB3B")},
		{name: "bug_div_short_any", code: rawCodeCellFromHex(t, "6883FF73A98D")},
		{name: "assert_pfx_dict_lookup", code: rawCodeCellFromHex(t, "778B04216D73F43E018B04591277F473")},
		{name: "assert_lookup_prefix", code: rawCodeCellFromHex(t, "78E58B008B028B04010000016D90ED5272F43A755D77F4A8")},
		{name: "assert_code_not_null", code: rawCodeCellFromHex(t, "76ED40DE")},
		{name: "bug_exec_dict_getnear", code: rawCodeCellFromHex(t, "8B048B00006D72F47573655F6D656D6D656D8B007F")},
		{name: "bug_stack_overflow", code: rawCodeCellFromHex(t, "72A93AF8")},
		{name: "assert_extract_minmax_key", code: rawCodeCellFromHex(t, "6D6DEB21807AF49C2180EB21807AF41C")},
		{name: "memory_leak_new", code: rawCodeCellFromHex(t, "72E5ED40DB3603")},
		{name: "unhandled_exception_1", code: rawCodeCellFromHex(t, "70EDA2ED00")},
		{name: "unhandled_exception_4", code: rawCodeCellFromHex(t, "7F853EA1C8CB3E")},
		{name: "unhandled_exception_5", code: rawCodeCellFromHex(t, "738B04016D21F41476A721F49F")},
		{name: "infinity_loop_1", code: rawCodeCellFromBase64(t, "f3r4AJGQ6rDraIQ=")},
		{name: "infinity_loop_2", code: rawCodeCellFromBase64(t, "kpTt7ZLrig==")},
		{name: "oom_1", code: rawCodeCellFromBase64(t, "bXflX/BvDw==")},
	}
}

var upstreamVMRegressionLocalNoStackCompare = map[string]string{
	"oom_1": "case builds recursive stack structures, so deterministic coverage here is limited to exit code and gas",
}

func regressionStackFingerprint(stack *vmcore.Stack) string {
	if stack == nil {
		return "<nil-stack>"
	}

	cp := stack.Copy()
	n := cp.Len()
	parts := make([]string, 0, n)
	for i := 0; i < n; i++ {
		val, err := cp.PopAny()
		if err != nil {
			parts = append(parts, "<pop-error:"+err.Error()+">")
			continue
		}
		parts = append(parts, fingerprintRegressionValue(val, 0))
	}
	return strings.Join(parts, " | ")
}

func fingerprintRegressionValue(val any, depth int) string {
	if depth >= 12 {
		return fmt.Sprintf("%T(...)", val)
	}

	switch v := val.(type) {
	case nil:
		return "nil"
	case vmcore.NaN, *vmcore.NaN:
		return "nan"
	case *big.Int:
		if v == nil {
			return "int:<nil>"
		}
		return "int:" + v.String()
	case *cell.Cell:
		if v == nil {
			return "cell:<nil>"
		}
		return "cell:" + v.Dump()
	case *cell.Slice:
		if v == nil {
			return "slice:<nil>"
		}
		cl, err := v.WithoutObserver().ToCell()
		if err != nil {
			return "slice:<invalid:" + err.Error() + ">"
		}
		return "slice:" + cl.Dump()
	case *cell.Builder:
		if v == nil {
			return "builder:<nil>"
		}
		return "builder:" + v.Copy().EndCell().Dump()
	case tuple.Tuple:
		parts := make([]string, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			item, err := v.Index(i)
			if err != nil {
				parts = append(parts, "<index-error:"+err.Error()+">")
				continue
			}
			parts = append(parts, fingerprintRegressionValue(item, depth+1))
		}
		return "tuple:[" + strings.Join(parts, ",") + "]"
	case vmcore.Continuation:
		data := v.GetControlData()
		if data == nil {
			return fmt.Sprintf("cont:%T", v)
		}
		saved := 0
		for _, cont := range data.Save.C {
			if cont != nil {
				saved++
			}
		}
		for _, reg := range data.Save.D {
			if reg != nil {
				saved++
			}
		}
		return fmt.Sprintf("cont:%T{cp=%d,args=%d,save=%d,c7=%d}", v, data.CP, data.NumArgs, saved, data.Save.C7.Len())
	default:
		return fmt.Sprintf("%T:%v", val, val)
	}
}

func gasFromResult(res *ExecutionResult) int64 {
	if res == nil {
		return 0
	}
	return res.GasUsed
}

func TestUpstreamVMRegressionsAreDeterministic(t *testing.T) {
	for _, tt := range upstreamVMRegressionCases(t) {
		t.Run(tt.name, func(t *testing.T) {
			stack1, res1, err1 := runRawCodeWithGas(tt.code, upstreamVMRegressionGasLimit)
			stack2, res2, err2 := runRawCodeWithGas(tt.code, upstreamVMRegressionGasLimit)

			exit1 := exitCodeFromResult(res1, err1)
			exit2 := exitCodeFromResult(res2, err2)
			if exit1 != exit2 {
				t.Fatalf("exit code is not deterministic: first=%d second=%d", exit1, exit2)
			}

			gas1 := gasFromResult(res1)
			gas2 := gasFromResult(res2)
			if gas1 != gas2 {
				t.Fatalf("gas is not deterministic: first=%d second=%d", gas1, gas2)
			}

			if _, skip := upstreamVMRegressionLocalNoStackCompare[tt.name]; skip {
				return
			}

			fp1 := regressionStackFingerprint(stack1)
			fp2 := regressionStackFingerprint(stack2)
			if fp1 != fp2 {
				t.Fatalf("stack is not deterministic:\nfirst=%s\nsecond=%s", fp1, fp2)
			}
		})
	}
}
