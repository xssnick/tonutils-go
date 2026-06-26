//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"testing"

	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

const stackOpsParityDepthLimit = 900

type stackOpParityCase struct {
	name   string
	op     *cell.Builder
	values []int64
	out    int
}

type stackOpDepthParityCase struct {
	name   string
	op     *cell.Builder
	values []int64
	base   int
}

func TestTVMCrossEmulatorStackOpsOpcodeSpace(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	cases := buildStackOpsOpcodeSpaceCases()
	for _, batch := range splitStackOpParityBatches(cases) {
		t.Run(batch.name, func(t *testing.T) {
			runStackOpParityProgram(t, buildStackOpParityProgram(t, batch.cases), nil, 0)
		})
	}
}

func TestTVMCrossEmulatorStackOpsOpcodeSpaceAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	versions := crossEmulatorVersionAuditVersions(t, "TVM_STACKOPS_OPCODE_SPACE_VERSION_AUDIT")
	cases := buildStackOpsOpcodeSpaceCases()
	for _, batch := range splitStackOpParityBatches(cases) {
		batch := batch
		t.Run(batch.name, func(t *testing.T) {
			code := buildStackOpParityProgram(t, batch.cases)
			for _, version := range versions {
				version := version
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					runStackOpVersionedParityProgram(t, code, nil, version, 0)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorStackOpsOpcodeSpaceGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%stackOpsOpcodeSpaceFuzzClassCount), uint16(version), uint16(version*17), uint16(version*31))
	}
	for class := 0; class < stackOpsOpcodeSpaceFuzzClassCount; class++ {
		f.Add(uint8(MaxSupportedGlobalVersion), uint8(class), uint16(class*13), uint16(class*19), uint16(class*23))
	}
	f.Add(uint8(255), uint8(255), uint16(65535), uint16(32768), uint16(1))

	f.Fuzz(func(t *testing.T, rawVersion, rawClass uint8, rawA, rawB, rawC uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		tc := stackOpsOpcodeSpaceFuzzCase(rawClass, rawA, rawB, rawC)
		runStackOpVersionedParityProgram(t, buildStackOpParityProgram(t, []stackOpParityCase{tc}), nil, version, 0)
	})
}

func TestTVMCrossEmulatorStackOpsDynamicAndDepthEdges(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, tt := range buildStackOpsDynamicAndDepthEdgeCases() {
		t.Run(tt.name, func(t *testing.T) {
			runStackOpDepthParityCase(t, tt)
		})
	}

	t.Run("transient_stack_above_1024", func(t *testing.T) {
		runStackOpParityProgram(t, buildStackOpsTransientStackAbove1024Program(t), nil, 0)
	})
}

func TestTVMCrossEmulatorStackOpsDynamicAndDepthEdgesAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := buildStackOpsDynamicAndDepthEdgeCases()
	if len(tests) != stackOpsDynamicAndDepthEdgeCaseCount {
		t.Fatalf("stackops dynamic/depth edge case count = %d, want %d", len(tests), stackOpsDynamicAndDepthEdgeCaseCount)
	}
	versions := crossEmulatorVersionAuditVersions(t, "TVM_STACKOPS_DYNAMIC_DEPTH_VERSION_AUDIT")
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			for _, version := range versions {
				version := version
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					runStackOpDepthVersionedParityCase(t, tt, version)
				})
			}
		})
	}

	t.Run("transient_stack_above_1024", func(t *testing.T) {
		code := buildStackOpsTransientStackAbove1024Program(t)
		for _, version := range versions {
			version := version
			t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
				runStackOpVersionedParityProgramWithoutExpected(t, code, nil, version)
			})
		}
	})
}

func FuzzTVMCrossEmulatorStackOpsDynamicAndDepthEdgesGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%stackOpsDynamicAndDepthFuzzCaseCount))
	}
	for i := 0; i < stackOpsDynamicAndDepthFuzzCaseCount; i++ {
		f.Add(uint8(MaxSupportedGlobalVersion), uint8(i))
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		caseIdx := int(rawCase) % stackOpsDynamicAndDepthFuzzCaseCount
		if caseIdx == stackOpsDynamicAndDepthEdgeCaseCount {
			runStackOpVersionedParityProgramWithoutExpected(t, buildStackOpsTransientStackAbove1024Program(t), nil, version)
			return
		}

		tests := buildStackOpsDynamicAndDepthEdgeCases()
		if len(tests) != stackOpsDynamicAndDepthEdgeCaseCount {
			t.Fatalf("stackops dynamic/depth edge case count = %d, want %d", len(tests), stackOpsDynamicAndDepthEdgeCaseCount)
		}
		runStackOpDepthVersionedParityCase(t, tests[caseIdx], version)
	})
}

func TestTVMCrossEmulatorStackOpsVersionedDynamicEdges(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := stackOpsVersionedDynamicEdgeCases()
	versions := crossEmulatorVersionAuditVersions(t, "TVM_STACKOPS_VERSION_AUDIT")
	for _, tt := range tests {
		for _, version := range versions {
			t.Run(fmt.Sprintf("%s_v%d", tt.name, version), func(t *testing.T) {
				runStackOpsVersionedDynamicEdgeCase(t, tt, version)
			})
		}
	}
}

func FuzzTVMCrossEmulatorStackOpsVersionedDynamicEdges(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%stackOpsVersionedDynamicEdgeCaseCount))
	}
	for i := 0; i < stackOpsVersionedDynamicEdgeCaseCount; i++ {
		f.Add(uint8(MaxSupportedGlobalVersion), uint8(i))
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		tests := stackOpsVersionedDynamicEdgeCases()
		if len(tests) != stackOpsVersionedDynamicEdgeCaseCount {
			t.Fatalf("stackops versioned dynamic edge case count = %d, want %d", len(tests), stackOpsVersionedDynamicEdgeCaseCount)
		}
		tt := tests[int(rawCase)%len(tests)]
		runStackOpsVersionedDynamicEdgeCase(t, tt, version)
	})
}

const stackOpsVersionedDynamicEdgeCaseCount = 12

type stackOpsVersionedDynamicEdgeCase struct {
	name        string
	op          *cell.Builder
	values      []int64
	base        int
	versionExit func(int) int32
}

func stackOpsVersionedDynamicEdgeCases() []stackOpsVersionedDynamicEdgeCase {
	return []stackOpsVersionedDynamicEdgeCase{
		{
			name:        "pick_256",
			op:          stackRawOp(0x60, 8),
			values:      []int64{256},
			base:        300,
			versionExit: stackOpsLargeIndexVersionExit,
		},
		{
			name:        "roll_256",
			op:          stackRawOp(0x61, 8),
			values:      []int64{256},
			base:        300,
			versionExit: stackOpsLargeIndexVersionExit,
		},
		{
			name:        "rollrev_256",
			op:          stackRawOp(0x62, 8),
			values:      []int64{256},
			base:        300,
			versionExit: stackOpsLargeIndexVersionExit,
		},
		{
			name:        "revx_256_0",
			op:          stackRawOp(0x64, 8),
			values:      []int64{256, 0},
			base:        300,
			versionExit: stackOpsLargeIndexVersionExit,
		},
		{
			name:        "dropx_256",
			op:          stackRawOp(0x65, 8),
			values:      []int64{256},
			base:        300,
			versionExit: stackOpsLargeIndexVersionExit,
		},
		{
			name:        "xchgx_256",
			op:          stackRawOp(0x67, 8),
			values:      []int64{256},
			base:        300,
			versionExit: stackOpsLargeIndexVersionExit,
		},
		{
			name:        "chkdepth_256",
			op:          stackRawOp(0x69, 8),
			values:      []int64{256},
			base:        300,
			versionExit: stackOpsLargeIndexVersionExit,
		},
		{
			name:        "onlytopx_256",
			op:          stackRawOp(0x6a, 8),
			values:      []int64{256},
			base:        300,
			versionExit: stackOpsLargeIndexVersionExit,
		},
		{
			name:        "onlyx_256",
			op:          stackRawOp(0x6b, 8),
			values:      []int64{256},
			base:        300,
			versionExit: stackOpsLargeIndexVersionExit,
		},
		{
			name:        "blkswx_128_128",
			op:          stackRawOp(0x63, 8),
			values:      []int64{128, 128},
			base:        300,
			versionExit: func(int) int32 { return 0 },
		},
		{
			name:        "blkswx_255_1",
			op:          stackRawOp(0x63, 8),
			values:      []int64{255, 1},
			base:        300,
			versionExit: func(int) int32 { return 0 },
		},
		{
			name:        "blkswx_200_60",
			op:          stackRawOp(0x63, 8),
			values:      []int64{200, 60},
			base:        300,
			versionExit: func(int) int32 { return 0 },
		},
	}
}

func stackOpsLargeIndexVersionExit(version int) int32 {
	if version < 4 {
		return int32(vmerr.CodeRangeCheck)
	}
	return 0
}

func runStackOpsVersionedDynamicEdgeCase(t *testing.T, tt stackOpsVersionedDynamicEdgeCase, version int) {
	t.Helper()

	initial := sequentialVMStack(t, tt.base)
	instructions := make([]*cell.Builder, 0, len(tt.values)+1)
	for _, value := range tt.values {
		instructions = append(instructions, stackPushInt(value))
	}
	instructions = append(instructions, tt.op)

	runStackOpVersionedParityProgram(t, buildStackProgram(t, instructions), initial, version, tt.versionExit(version))
}

const stackOpsDynamicAndDepthEdgeCaseCount = 22
const stackOpsDynamicAndDepthFuzzCaseCount = stackOpsDynamicAndDepthEdgeCaseCount + 1

func buildStackOpsDynamicAndDepthEdgeCases() []stackOpDepthParityCase {
	return []stackOpDepthParityCase{
		{name: "pick_0", op: stackRawOp(0x60, 8), values: []int64{0}, base: 3},
		{name: "pick_255", op: stackRawOp(0x60, 8), values: []int64{255}, base: 256},
		{name: "pick_1020", op: stackRawOp(0x60, 8), values: []int64{1020}, base: 1021},
		{name: "roll_0", op: stackRawOp(0x61, 8), values: []int64{0}, base: 3},
		{name: "roll_256", op: stackRawOp(0x61, 8), values: []int64{256}, base: 257},
		{name: "rollrev_256", op: stackRawOp(0x62, 8), values: []int64{256}, base: 257},
		{name: "blkswx_empty_left", op: stackRawOp(0x63, 8), values: []int64{0, 7}, base: 7},
		{name: "blkswx_empty_right", op: stackRawOp(0x63, 8), values: []int64{7, 0}, base: 7},
		{name: "blkswx_510_511", op: stackRawOp(0x63, 8), values: []int64{510, 511}, base: 1021},
		{name: "revx_empty", op: stackRawOp(0x64, 8), values: []int64{0, 9}, base: 9},
		{name: "revx_510_511", op: stackRawOp(0x64, 8), values: []int64{510, 511}, base: 1021},
		{name: "dropx_0", op: stackRawOp(0x65, 8), values: []int64{0}, base: 3},
		{name: "dropx_512", op: stackRawOp(0x65, 8), values: []int64{512}, base: 512},
		{name: "xchgx_0", op: stackRawOp(0x67, 8), values: []int64{0}, base: 3},
		{name: "xchgx_255", op: stackRawOp(0x67, 8), values: []int64{255}, base: 256},
		{name: "xchgx_1021", op: stackRawOp(0x67, 8), values: []int64{1021}, base: 1022},
		{name: "depth_1021", op: stackRawOp(0x68, 8), base: 1021},
		{name: "chkdepth_1022", op: stackRawOp(0x69, 8), values: []int64{1022}, base: 1022},
		{name: "onlytopx_0", op: stackRawOp(0x6a, 8), values: []int64{0}, base: 3},
		{name: "onlytopx_1022", op: stackRawOp(0x6a, 8), values: []int64{1022}, base: 1022},
		{name: "onlyx_0", op: stackRawOp(0x6b, 8), values: []int64{0}, base: 3},
		{name: "onlyx_1022", op: stackRawOp(0x6b, 8), values: []int64{1022}, base: 1022},
	}
}

func buildStackOpsOpcodeSpaceCases() []stackOpParityCase {
	var cases []stackOpParityCase
	add := func(name string, op *cell.Builder, depth, out int) {
		cases = append(cases, stackOpParityCase{
			name:   name,
			op:     op,
			values: stackCaseValues(len(cases), depth),
			out:    out,
		})
	}

	add("nop", stackRawOp(0x00, 8), 3, 3)
	add("swap", stackRawOp(0x01, 8), 2, 2)
	for i := 2; i < 16; i++ {
		add(fmt.Sprintf("xchg0_short_%d", i), stackRawOp(uint64(i), 8), 16, 16)
	}
	for i := 1; i < 16; i++ {
		for j := i + 1; j < 16; j++ {
			add(fmt.Sprintf("xchg_%d_%d", i, j), stackRawOp(0x1000|uint64(i)<<4|uint64(j), 16), 16, 16)
		}
	}
	for i := 0; i < 256; i++ {
		add(fmt.Sprintf("xchg0_long_%d", i), stackRawOp(0x1100|uint64(i), 16), 256, 256)
	}
	for i := 2; i < 16; i++ {
		add(fmt.Sprintf("xchg1_short_%d", i), stackRawOp(0x10|uint64(i), 8), 16, 16)
	}
	add("dup", stackRawOp(0x20, 8), 1, 2)
	add("over", stackRawOp(0x21, 8), 2, 3)
	for i := 2; i < 16; i++ {
		add(fmt.Sprintf("push_short_%d", i), stackRawOp(0x20|uint64(i), 8), 16, 17)
	}
	add("drop", stackRawOp(0x30, 8), 2, 1)
	add("nip", stackRawOp(0x31, 8), 2, 1)
	for i := 2; i < 16; i++ {
		add(fmt.Sprintf("pop_short_%d", i), stackRawOp(0x30|uint64(i), 8), 16, 15)
	}
	for i := 0; i < 16; i++ {
		for j := 0; j < 16; j++ {
			for k := 0; k < 16; k++ {
				add(fmt.Sprintf("xchg3_short_%d_%d_%d", i, j, k), stackRawOp(0x4000|uint64(i)<<8|uint64(j)<<4|uint64(k), 16), 16, 16)
			}
		}
	}

	for _, group := range []struct {
		name   string
		prefix uint64
		depth  int
		out    int
	}{
		{name: "xchg2", prefix: 0x50, depth: 16, out: 16},
		{name: "xcpu", prefix: 0x51, depth: 16, out: 17},
		{name: "puxc", prefix: 0x52, depth: 16, out: 17},
		{name: "push2", prefix: 0x53, depth: 16, out: 18},
	} {
		for i := 0; i < 16; i++ {
			for j := 0; j < 16; j++ {
				add(fmt.Sprintf("%s_%d_%d", group.name, i, j), stackRawOp(group.prefix<<8|uint64(i)<<4|uint64(j), 16), group.depth, group.out)
			}
		}
	}

	for _, group := range []struct {
		name   string
		prefix uint64
		depth  int
		out    int
	}{
		{name: "xchg3_ext", prefix: 0x540, depth: 16, out: 16},
		{name: "xc2pu", prefix: 0x541, depth: 16, out: 17},
		{name: "xcpuxc", prefix: 0x542, depth: 16, out: 17},
		{name: "xcpu2", prefix: 0x543, depth: 16, out: 18},
		{name: "puxc2", prefix: 0x544, depth: 16, out: 17},
		{name: "puxcpu", prefix: 0x545, depth: 16, out: 18},
		{name: "pu2xc", prefix: 0x546, depth: 16, out: 18},
		{name: "push3", prefix: 0x547, depth: 16, out: 19},
	} {
		for i := 0; i < 16; i++ {
			for j := 0; j < 16; j++ {
				for k := 0; k < 16; k++ {
					add(
						fmt.Sprintf("%s_%d_%d_%d", group.name, i, j, k),
						stackRawOp(group.prefix<<12|uint64(i)<<8|uint64(j)<<4|uint64(k), 24),
						group.depth,
						group.out,
					)
				}
			}
		}
	}

	for i := 0; i < 16; i++ {
		for j := 0; j < 16; j++ {
			add(fmt.Sprintf("blkswap_%d_%d", i+1, j+1), stackRawOp(0x5500|uint64(i)<<4|uint64(j), 16), 32, 32)
		}
	}
	for i := 0; i < 256; i++ {
		add(fmt.Sprintf("push_long_%d", i), stackRawOp(0x5600|uint64(i), 16), 256, 257)
	}
	for i := 0; i < 256; i++ {
		add(fmt.Sprintf("pop_long_%d", i), stackRawOp(0x5700|uint64(i), 16), 256, 255)
	}
	add("rot", stackRawOp(0x58, 8), 3, 3)
	add("rotrev", stackRawOp(0x59, 8), 3, 3)
	add("2swap", stackRawOp(0x5a, 8), 4, 4)
	add("2drop", stackRawOp(0x5b, 8), 3, 1)
	add("2dup", stackRawOp(0x5c, 8), 2, 4)
	add("2over", stackRawOp(0x5d, 8), 4, 6)
	for i := 0; i < 16; i++ {
		for j := 0; j < 16; j++ {
			add(fmt.Sprintf("reverse_%d_%d", i+2, j), stackRawOp(0x5e00|uint64(i)<<4|uint64(j), 16), 32, 32)
		}
	}
	for i := 0; i < 16; i++ {
		depth := i + 1
		if i == 0 {
			depth = 1
		}
		add(fmt.Sprintf("blkdrop_%d", i), stackRawOp(0x5f00|uint64(i), 16), depth, depth-i)
	}
	for i := 1; i < 16; i++ {
		for j := 0; j < 16; j++ {
			add(fmt.Sprintf("blkpush_%d_%d", i, j), stackRawOp(0x5f00|uint64(i)<<4|uint64(j), 16), 16, 16+i)
		}
	}
	add("tuck", stackRawOp(0x66, 8), 2, 3)
	add("depth", stackRawOp(0x68, 8), 3, 4)
	for i := 1; i < 16; i++ {
		for j := 0; j < 16; j++ {
			add(fmt.Sprintf("blkdrop2_%d_%d", i, j), stackRawOp(0x6c00|uint64(i)<<4|uint64(j), 16), i+j+1, j+1)
		}
	}

	return cases
}

const stackOpsOpcodeSpaceFuzzClassCount = 20

func stackOpsOpcodeSpaceFuzzCase(rawClass uint8, rawA, rawB, rawC uint16) stackOpParityCase {
	build := func(name string, op *cell.Builder, depth, out int) stackOpParityCase {
		return stackOpParityCase{
			name:   name,
			op:     op,
			values: stackCaseValues(int(rawA)<<16|int(rawB), depth),
			out:    out,
		}
	}

	switch rawClass % stackOpsOpcodeSpaceFuzzClassCount {
	case 0:
		return build("fuzz_nop", stackRawOp(0x00, 8), 3, 3)
	case 1:
		return build("fuzz_swap", stackRawOp(0x01, 8), 2, 2)
	case 2:
		i := 2 + int(rawA%14)
		return build(fmt.Sprintf("fuzz_xchg0_short_%d", i), stackRawOp(uint64(i), 8), 16, 16)
	case 3:
		i := 1 + int(rawA%14)
		j := i + 1 + int(rawB%uint16(15-i))
		return build(fmt.Sprintf("fuzz_xchg_%d_%d", i, j), stackRawOp(0x1000|uint64(i)<<4|uint64(j), 16), 16, 16)
	case 4:
		i := int(rawA % 256)
		return build(fmt.Sprintf("fuzz_xchg0_long_%d", i), stackRawOp(0x1100|uint64(i), 16), 256, 256)
	case 5:
		i := 2 + int(rawA%14)
		return build(fmt.Sprintf("fuzz_xchg1_short_%d", i), stackRawOp(0x10|uint64(i), 8), 16, 16)
	case 6:
		i := 2 + int(rawA%14)
		return build(fmt.Sprintf("fuzz_push_short_%d", i), stackRawOp(0x20|uint64(i), 8), 16, 17)
	case 7:
		i := 2 + int(rawA%14)
		return build(fmt.Sprintf("fuzz_pop_short_%d", i), stackRawOp(0x30|uint64(i), 8), 16, 15)
	case 8:
		i := int(rawA % 16)
		j := int(rawB % 16)
		k := int(rawC % 16)
		return build(fmt.Sprintf("fuzz_xchg3_short_%d_%d_%d", i, j, k), stackRawOp(0x4000|uint64(i)<<8|uint64(j)<<4|uint64(k), 16), 16, 16)
	case 9:
		group := int(rawA % 4)
		prefixes := []uint64{0x50, 0x51, 0x52, 0x53}
		names := []string{"fuzz_xchg2", "fuzz_xcpu", "fuzz_puxc", "fuzz_push2"}
		outs := []int{16, 17, 17, 18}
		i := int(rawB % 16)
		j := int(rawC % 16)
		return build(fmt.Sprintf("%s_%d_%d", names[group], i, j), stackRawOp(prefixes[group]<<8|uint64(i)<<4|uint64(j), 16), 16, outs[group])
	case 10:
		group := int(rawA % 8)
		names := []string{"fuzz_xchg3_ext", "fuzz_xc2pu", "fuzz_xcpuxc", "fuzz_xcpu2", "fuzz_puxc2", "fuzz_puxcpu", "fuzz_pu2xc", "fuzz_push3"}
		outs := []int{16, 17, 17, 18, 17, 18, 18, 19}
		i := int(rawB % 16)
		j := int(rawC % 16)
		k := int((rawA>>4 ^ rawB ^ rawC) & 15)
		prefix := uint64(0x540 + group)
		return build(fmt.Sprintf("%s_%d_%d_%d", names[group], i, j, k), stackRawOp(prefix<<12|uint64(i)<<8|uint64(j)<<4|uint64(k), 24), 16, outs[group])
	case 11:
		i := int(rawA % 16)
		j := int(rawB % 16)
		return build(fmt.Sprintf("fuzz_blkswap_%d_%d", i+1, j+1), stackRawOp(0x5500|uint64(i)<<4|uint64(j), 16), 32, 32)
	case 12:
		i := int(rawA % 256)
		return build(fmt.Sprintf("fuzz_push_long_%d", i), stackRawOp(0x5600|uint64(i), 16), 256, 257)
	case 13:
		i := int(rawA % 256)
		return build(fmt.Sprintf("fuzz_pop_long_%d", i), stackRawOp(0x5700|uint64(i), 16), 256, 255)
	case 14:
		ops := []struct {
			name  string
			op    uint64
			depth int
			out   int
		}{
			{name: "fuzz_rot", op: 0x58, depth: 3, out: 3},
			{name: "fuzz_rotrev", op: 0x59, depth: 3, out: 3},
			{name: "fuzz_2swap", op: 0x5a, depth: 4, out: 4},
			{name: "fuzz_2drop", op: 0x5b, depth: 3, out: 1},
			{name: "fuzz_2dup", op: 0x5c, depth: 2, out: 4},
			{name: "fuzz_2over", op: 0x5d, depth: 4, out: 6},
		}
		op := ops[int(rawA)%len(ops)]
		return build(op.name, stackRawOp(op.op, 8), op.depth, op.out)
	case 15:
		i := int(rawA % 16)
		j := int(rawB % 16)
		return build(fmt.Sprintf("fuzz_reverse_%d_%d", i+2, j), stackRawOp(0x5e00|uint64(i)<<4|uint64(j), 16), 32, 32)
	case 16:
		i := int(rawA % 16)
		depth := i + 1
		return build(fmt.Sprintf("fuzz_blkdrop_%d", i), stackRawOp(0x5f00|uint64(i), 16), depth, depth-i)
	case 17:
		i := 1 + int(rawA%15)
		j := int(rawB % 16)
		return build(fmt.Sprintf("fuzz_blkpush_%d_%d", i, j), stackRawOp(0x5f00|uint64(i)<<4|uint64(j), 16), 16, 16+i)
	case 18:
		if rawA%2 == 0 {
			return build("fuzz_tuck", stackRawOp(0x66, 8), 2, 3)
		}
		return build("fuzz_depth", stackRawOp(0x68, 8), 3, 4)
	default:
		i := 1 + int(rawA%15)
		j := int(rawB % 16)
		return build(fmt.Sprintf("fuzz_blkdrop2_%d_%d", i, j), stackRawOp(0x6c00|uint64(i)<<4|uint64(j), 16), i+j+1, j+1)
	}
}

type stackOpParityBatch struct {
	name  string
	cases []stackOpParityCase
}

func splitStackOpParityBatches(cases []stackOpParityCase) []stackOpParityBatch {
	var batches []stackOpParityBatch
	var current []stackOpParityCase
	currentDepth := 0
	start := 0

	flush := func(end int) {
		if len(current) == 0 {
			return
		}
		batches = append(batches, stackOpParityBatch{
			name:  fmt.Sprintf("%05d_%05d_%s_to_%s", start, end-1, current[0].name, current[len(current)-1].name),
			cases: current,
		})
		current = nil
		currentDepth = 0
		start = end
	}

	for i, tc := range cases {
		maxDepth := len(tc.values)
		if tc.out > maxDepth {
			maxDepth = tc.out
		}
		if len(current) > 0 && currentDepth+maxDepth > stackOpsParityDepthLimit {
			flush(i)
		}
		current = append(current, tc)
		currentDepth += tc.out
	}
	flush(len(cases))

	return batches
}

func buildStackOpParityProgram(t *testing.T, cases []stackOpParityCase) *cell.Cell {
	t.Helper()
	var instructions []*cell.Builder
	for _, tc := range cases {
		for _, value := range tc.values {
			instructions = append(instructions, stackPushInt(value))
		}
		instructions = append(instructions, tc.op)
	}
	return buildStackProgram(t, instructions)
}

func buildStackProgram(t *testing.T, instructions []*cell.Builder) *cell.Cell {
	t.Helper()

	all := make([]*cell.Builder, 0, len(instructions)+1)
	all = append(all, stackRawOp(0x30, 8))
	all = append(all, instructions...)

	var chunks [][]*cell.Builder
	var chunk []*cell.Builder
	bits := uint(0)
	refs := 0
	flush := func() {
		if len(chunk) == 0 {
			return
		}
		chunks = append(chunks, chunk)
		chunk = nil
		bits = 0
		refs = 0
	}

	for _, instr := range all {
		needBits := instr.BitsUsed()
		needRefs := instr.RefsUsed()
		if len(chunk) > 0 && (bits+needBits+16 >= 1024 || refs+needRefs+1 > 4) {
			flush()
		}
		chunk = append(chunk, instr)
		bits += needBits
		refs += needRefs
	}
	flush()

	var next *cell.Cell
	for i := len(chunks) - 1; i >= 0; i-- {
		builder := cell.BeginCell()
		for _, instr := range chunks[i] {
			builder.MustStoreBuilder(instr)
		}
		if next != nil {
			builder.MustStoreBuilder(execop.JMPREF(next).Serialize())
		}
		next = builder.EndCell()
	}

	return next
}

func runStackOpDepthParityCase(t *testing.T, tc stackOpDepthParityCase) {
	t.Helper()

	initial := sequentialVMStack(t, tc.base)
	instructions := make([]*cell.Builder, 0, len(tc.values)+1)
	for _, value := range tc.values {
		instructions = append(instructions, stackPushInt(value))
	}
	instructions = append(instructions, tc.op)

	runStackOpParityProgram(t, buildStackProgram(t, instructions), initial, 0)
}

func runStackOpDepthVersionedParityCase(t *testing.T, tc stackOpDepthParityCase, version int) {
	t.Helper()

	initial := sequentialVMStack(t, tc.base)
	instructions := make([]*cell.Builder, 0, len(tc.values)+1)
	for _, value := range tc.values {
		instructions = append(instructions, stackPushInt(value))
	}
	instructions = append(instructions, tc.op)

	runStackOpVersionedParityProgramWithoutExpected(t, buildStackProgram(t, instructions), initial, version)
}

func buildStackOpsTransientStackAbove1024Program(t *testing.T) *cell.Cell {
	t.Helper()

	instructions := []*cell.Builder{stackPushInt(1)}
	for range 1024 {
		instructions = append(instructions, stackRawOp(0x20, 8))
	}
	for range 3 {
		instructions = append(instructions, stackRawOp(0x30, 8))
	}
	return buildStackProgram(t, instructions)
}

func runStackOpParityProgram(t *testing.T, code *cell.Cell, initial *vm.Stack, wantExit int32) {
	t.Helper()
	if initial == nil {
		initial = vm.NewStack()
	}

	goRes, err := runGoCrossCode(code, testEmptyCell(), tuple.Tuple{}, initial)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}

	refRes, err := runReferenceCrossCode(code, testEmptyCell(), tuple.Tuple{}, initial)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if goRes.exitCode != wantExit || refRes.exitCode != wantExit {
		t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, wantExit)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
	}

	goStackCell, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize go stack: %v", err)
	}
	refStackCell, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize reference stack: %v", err)
	}
	if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
		t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goStackCell.Dump(), refStackCell.Dump())
	}
}

func runStackOpVersionedParityProgram(t *testing.T, code *cell.Cell, initial *vm.Stack, globalVersion int, wantExit int32) {
	t.Helper()
	runStackOpVersionedParityProgramWithExpected(t, code, initial, globalVersion, &wantExit)
}

func runStackOpVersionedParityProgramWithoutExpected(t *testing.T, code *cell.Cell, initial *vm.Stack, globalVersion int) {
	t.Helper()
	runStackOpVersionedParityProgramWithExpected(t, code, initial, globalVersion, nil)
}

func runStackOpVersionedParityProgramWithExpected(t *testing.T, code *cell.Cell, initial *vm.Stack, globalVersion int, expectedExit *int32) {
	t.Helper()
	if initial == nil {
		initial = vm.NewStack()
	}

	goRes, err := runGoCrossCodeWithVersion(code, testEmptyCell(), tuple.Tuple{}, initial, globalVersion)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(globalVersion)))
	refCfg.GasLimit = referenceDefaultMaxGas
	refRes, err := runReferenceCrossCodeViaEmulator(code, testEmptyCell(), initial, *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if expectedExit != nil && (goRes.exitCode != *expectedExit || refRes.exitCode != *expectedExit) {
		t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, *expectedExit)
	}
	if goRes.exitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.exitCode, refRes.exitCode)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
	}

	goStackCell, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize go stack: %v", err)
	}
	refStackCell, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize reference stack: %v", err)
	}
	if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
		t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goStackCell.Dump(), refStackCell.Dump())
	}
}

func stackRawOp(value uint64, bits uint) *cell.Builder {
	return cell.BeginCell().MustStoreUInt(value, bits)
}

func stackPushInt(value int64) *cell.Builder {
	return stackop.PUSHINT(big.NewInt(value)).Serialize()
}

func stackCaseValues(caseID, depth int) []int64 {
	values := make([]int64, depth)
	base := int64(caseID%120)*300 + 1
	for i := range values {
		values[i] = base + int64(i)
	}
	return values
}

func sequentialVMStack(t *testing.T, depth int) *vm.Stack {
	t.Helper()
	stack := vm.NewStack()
	for i := 0; i < depth; i++ {
		if err := stack.PushInt(big.NewInt(int64(i + 1))); err != nil {
			t.Fatalf("failed to build stack: %v", err)
		}
	}
	return stack
}
