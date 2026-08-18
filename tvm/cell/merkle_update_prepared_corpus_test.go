package cell

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"testing"
)

// The corpus behind TestPreparedMerkleUpdateDifferential and
// FuzzPreparedMerkleUpdate. Every case is a pure function of its spec, so the
// two sides of the differential can be built INDEPENDENTLY — sharing a tree
// between them would hide exactly the materialization differences the fused
// path is supposed to preserve.

type preparedShape uint8

const (
	shapeBinary preparedShape = iota
	shapeFan4
	shapeChain
	shapeWide
	shapeSingle
	shapeDAG
	shapeDict
	shapeNestedProof
	shapeDupDepths
	preparedShapeCount
)

func (s preparedShape) String() string {
	switch s {
	case shapeBinary:
		return "binary"
	case shapeFan4:
		return "fan4"
	case shapeChain:
		return "chain"
	case shapeWide:
		return "wide"
	case shapeSingle:
		return "single"
	case shapeDAG:
		return "dag"
	case shapeDict:
		return "dict"
	case shapeNestedProof:
		return "nested_proof"
	case shapeDupDepths:
		return "dup_depths"
	}
	return "unknown"
}

type preparedBuilder uint8

const (
	builderDiff      preparedBuilder = iota // no pruning above the first divergence
	builderDiffPrune                        // prune every unchanged subtree, level 1
	builderDepth                            // Merkle-depth aware pruning (levels md+1)
	builderReadSet                          // ReadSet.CreateMerkleUpdate
	preparedBuilderCount
)

func (b preparedBuilder) String() string {
	switch b {
	case builderDiff:
		return "diff"
	case builderDiffPrune:
		return "diff_prune"
	case builderDepth:
		return "depth"
	case builderReadSet:
		return "readset"
	}
	return "unknown"
}

type preparedSpec struct {
	shape   preparedShape
	size    int
	changes int // 0 = none, -1 = root only, -2 = deepest only, n>0 = n leaves
	builder preparedBuilder
	// mutation is an index into preparedMutations, or -1 for an accepted case.
	mutation int
	mutateTo bool // place the mutation in the destination body instead
	seed     int64
}

func (s preparedSpec) name() string {
	mutation := "none"
	if s.mutation >= 0 {
		side := "src"
		if s.mutateTo {
			side = "dst"
		}
		mutation = side + "_" + preparedMutations[s.mutation].name
	}
	return fmt.Sprintf("%s/size=%d/changes=%d/%s/mut=%s", s.shape, s.size, s.changes, s.builder, mutation)
}

type preparedCase struct {
	from   *Cell
	to     *Cell
	update *Cell
	// skip is set when the spec does not describe a constructible case; the
	// differential then reports nothing rather than pretending to have run.
	skip string
}

// ---------------------------------------------------------------- shapes

func preparedTree(tb testing.TB, spec preparedSpec) (*Cell, *Cell) {
	tb.Helper()

	rnd := rand.New(rand.NewSource(spec.seed))
	switch spec.shape {
	case shapeBinary:
		// buildBinaryTree stores the subtree size in eight bits, so 128 leaves
		// is the widest tree it can label.
		values := randomLeafValues(rnd, 1<<uint(clampInt(spec.size, 1, 7)))
		return preparedPair(values, spec, func(v []uint16) *Cell { return buildBinaryTree(v) })
	case shapeFan4:
		values := randomLeafValues(rnd, clampInt(spec.size, 4, 256))
		return preparedPair(values, spec, buildFanTree)
	case shapeChain:
		values := randomLeafValues(rnd, clampInt(spec.size, 2, 512))
		return preparedPair(values, spec, buildChainTree)
	case shapeWide:
		values := randomLeafValues(rnd, clampInt(spec.size, 4, 64))
		return preparedPair(values, spec, buildWideTree)
	case shapeSingle:
		from := BeginCell().MustStoreUInt(0xA5A5, 16).EndCell()
		to := from
		if spec.changes != 0 {
			to = BeginCell().MustStoreUInt(0x5A5A, 16).EndCell()
		}
		return from, to
	case shapeDAG:
		return buildDAGPair(tb, spec, rnd)
	case shapeDict:
		return buildDictPair(tb, spec, rnd)
	case shapeNestedProof:
		return buildNestedProofPair(tb, spec, rnd)
	case shapeDupDepths:
		return buildDupDepthPair(tb, spec, rnd)
	}
	return nil, nil
}

func preparedPair(values []uint16, spec preparedSpec, build func([]uint16) *Cell) (*Cell, *Cell) {
	from := build(values)
	next := preparedMutatedValues(values, spec)
	return from, build(next)
}

func preparedMutatedValues(values []uint16, spec preparedSpec) []uint16 {
	rnd := rand.New(rand.NewSource(spec.seed ^ 0x5deece66d))
	switch {
	case spec.changes == 0:
		return values
	case spec.changes == -1:
		next := append([]uint16{}, values...)
		next[0] ^= 0x4001
		return next
	case spec.changes == -2:
		next := append([]uint16{}, values...)
		next[len(next)-1] ^= 0x4001
		return next
	default:
		return mutateLeafValues(rnd, values, clampInt(spec.changes, 1, len(values)))
	}
}

func buildFanTree(values []uint16) *Cell {
	if len(values) <= 1 {
		return BeginCell().MustStoreUInt(uint64(values[0]), 16).EndCell()
	}
	step := (len(values) + 3) / 4
	builder := BeginCell().MustStoreUInt(uint64(len(values)), 16)
	for i := 0; i < len(values); i += step {
		end := i + step
		if end > len(values) {
			end = len(values)
		}
		builder.MustStoreRef(buildFanTree(values[i:end]))
	}
	return builder.EndCell()
}

func buildChainTree(values []uint16) *Cell {
	c := BeginCell().MustStoreUInt(uint64(values[len(values)-1]), 16).EndCell()
	for i := len(values) - 2; i >= 0; i-- {
		c = BeginCell().MustStoreUInt(uint64(values[i]), 16).MustStoreRef(c).EndCell()
	}
	return c
}

func buildWideTree(values []uint16) *Cell {
	// A root of four refs, each a chain: the shape where one changed leaf
	// leaves three whole branches untouched.
	quarter := (len(values) + 3) / 4
	builder := BeginCell().MustStoreUInt(uint64(len(values)), 16)
	for i := 0; i < 4; i++ {
		start := i * quarter
		end := start + quarter
		if start >= len(values) {
			start = len(values) - 1
		}
		if end > len(values) {
			end = len(values)
		}
		builder.MustStoreRef(buildChainTree(values[start:end]))
	}
	return builder.EndCell()
}

// buildDAGPair puts one subtree behind several references, including two
// distinct *Cell pointers carrying equal hashes and one occurrence nested
// deeper than the others.
func buildDAGPair(tb testing.TB, spec preparedSpec, rnd *rand.Rand) (*Cell, *Cell) {
	tb.Helper()

	shared := buildBinaryTree(randomLeafValues(rnd, 4))
	twin, err := FromBOC(shared.ToBOC())
	if err != nil {
		tb.Fatalf("re-parse the shared subtree: %v", err)
	}
	deep := BeginCell().MustStoreUInt(0x11, 8).MustStoreRef(shared).EndCell()
	tail := buildBinaryTree(randomLeafValues(rnd, 4))

	build := func(tag uint64, leaf *Cell) *Cell {
		return BeginCell().MustStoreUInt(tag, 16).
			MustStoreRef(shared).
			MustStoreRef(twin).
			MustStoreRef(deep).
			MustStoreRef(leaf).
			EndCell()
	}
	from := build(0xAAAA, tail)
	if spec.changes == 0 {
		return from, from
	}
	return from, build(0xBBBB, buildBinaryTree(randomLeafValues(rnd, 4)))
}

func buildDictPair(tb testing.TB, spec preparedSpec, rnd *rand.Rand) (*Cell, *Cell) {
	tb.Helper()

	entries := clampInt(spec.size, 4, 2048)
	fromDict := NewDict(64)
	keys := make([]uint64, 0, entries)
	for len(keys) < entries {
		key := rnd.Uint64()
		keys = append(keys, key)
		if err := fromDict.Set(merkleUpdateBenchKey(key), merkleUpdateBenchValue(key^0x55aa)); err != nil {
			tb.Fatalf("set dict entry: %v", err)
		}
	}
	toDict := fromDict.Copy()
	changes := clampInt(spec.changes, 0, entries)
	if spec.changes < 0 {
		changes = 1
	}
	for i := 0; i < changes; i++ {
		key := keys[i*7919%entries]
		if err := toDict.Set(merkleUpdateBenchKey(key), merkleUpdateBenchValue(key^uint64(i+1))); err != nil {
			tb.Fatalf("set changed dict entry: %v", err)
		}
	}
	return fromDict.AsCell(), toDict.AsCell()
}

// buildNestedProofPair embeds a real MerkleProof cell inside the state tree.
// It is the only way to make childEffectiveLevelFor advance the Merkle depth
// past zero, and without it the entire merkleDepth machinery — and with it the
// known-map identity trap and the virtualization trap — go untested.
func buildNestedProofPair(tb testing.TB, spec preparedSpec, rnd *rand.Rand) (*Cell, *Cell) {
	tb.Helper()

	build := func(values []uint16, tag uint64) *Cell {
		body := buildBinaryTree(values)
		proof, err := CreateMerkleProof(body)
		if err != nil {
			tb.Fatalf("create nested merkle proof: %v", err)
		}
		return BeginCell().MustStoreUInt(tag, 16).
			MustStoreRef(buildBinaryTree(values)).
			MustStoreRef(proof).
			EndCell()
	}
	values := randomLeafValues(rnd, 1<<uint(clampInt(spec.size, 2, 6)))
	from := build(values, 0xC0DE)
	if spec.changes == 0 {
		return from, from
	}
	return build(preparedMutatedValues(values, spec), 0xC0DE), from
}

// buildDupDepthPair reaches the same subtree at Merkle depth 0 and, through a
// nested proof, at Merkle depth 1.
func buildDupDepthPair(tb testing.TB, spec preparedSpec, rnd *rand.Rand) (*Cell, *Cell) {
	tb.Helper()

	shared := buildBinaryTree(randomLeafValues(rnd, 4))
	build := func(tag uint64, tail *Cell) *Cell {
		body := BeginCell().MustStoreUInt(0x77, 8).MustStoreRef(shared).MustStoreRef(tail).EndCell()
		proof, err := CreateMerkleProof(body)
		if err != nil {
			tb.Fatalf("create nested merkle proof: %v", err)
		}
		return BeginCell().MustStoreUInt(tag, 16).
			MustStoreRef(shared).
			MustStoreRef(proof).
			EndCell()
	}
	tail := buildBinaryTree(randomLeafValues(rnd, 4))
	from := build(0x1234, tail)
	if spec.changes == 0 {
		return from, from
	}
	return from, build(0x1234, buildBinaryTree(randomLeafValues(rnd, 4)))
}

// ---------------------------------------------------------------- builders

func buildPreparedCase(tb testing.TB, spec preparedSpec) preparedCase {
	tb.Helper()

	from, to := preparedTree(tb, spec)
	if from == nil || to == nil {
		return preparedCase{skip: "shape is not constructible"}
	}

	var updateFrom, updateTo *Cell
	var err error
	switch spec.builder {
	case builderDiff:
		updateFrom, updateTo, err = buildOrdinaryMerkleUpdateBodiesFromDiffAt(from, to, false)
	case builderDiffPrune:
		updateFrom, updateTo, err = buildOrdinaryMerkleUpdateBodiesFromDiffAt(from, to, true)
	case builderDepth:
		updateFrom, updateTo, err = buildMerkleUpdateBodiesAtDepth(from, to, 0, false)
	case builderReadSet:
		return buildReadSetPreparedCase(tb, spec, from, to)
	}
	if err != nil {
		return preparedCase{skip: "update bodies are not constructible: " + err.Error()}
	}
	if updateFrom == nil || updateTo == nil {
		return preparedCase{skip: "update bodies are nil"}
	}
	if spec.mutation >= 0 {
		if spec.mutateTo {
			updateTo = applyPreparedMutation(tb, spec.mutation, updateTo)
		} else {
			updateFrom = applyPreparedMutation(tb, spec.mutation, updateFrom)
		}
		if updateFrom == nil || updateTo == nil {
			return preparedCase{skip: "mutation is not applicable to this shape"}
		}
	}
	update, err := CreateMerkleUpdate(updateFrom, updateTo)
	if err != nil {
		return preparedCase{skip: "merkle update cell is not constructible: " + err.Error()}
	}
	return preparedCase{from: from, to: to, update: update}
}

// buildMerkleUpdateBodiesAtDepth is the Merkle-depth aware body builder: a
// boundary replacing a subtree seen at depth md is a level md+1 pruned branch,
// not a level 1 one. It also refuses to prune the direct child of a nested
// Merkle cell, whose stored hash names that child.
func buildMerkleUpdateBodiesAtDepth(from, to *Cell, merkleDepth int, prune bool) (*Cell, *Cell, error) {
	if from == nil || to == nil {
		return nil, nil, fmt.Errorf("nil subtree")
	}
	if from.HashKeyAt(merkleDepth) == to.HashKeyAt(merkleDepth) {
		if !prune || from.refsCount() == 0 {
			return from, to, nil
		}
		prunedFrom, err := CreatePrunedBranch(from, merkleDepth+1, merkleDepth)
		if err != nil {
			return nil, nil, err
		}
		prunedTo, err := CreatePrunedBranch(to, merkleDepth+1, merkleDepth)
		if err != nil {
			return nil, nil, err
		}
		return prunedFrom, prunedTo, nil
	}
	if from.refsCount() != to.refsCount() || from.IsSpecial() != to.IsSpecial() ||
		from.GetType() != to.GetType() {
		return nil, nil, fmt.Errorf("shape mismatch at merkle depth %d", merkleDepth)
	}
	if from.refsCount() == 0 {
		return from, to, nil
	}

	special := from.IsSpecial()
	childDepth := merkleChildDepth(from, merkleDepth)
	refsFrom := make([]*Cell, from.refsCount())
	refsTo := make([]*Cell, to.refsCount())
	for i := range refsFrom {
		nextFrom, nextTo, err := buildMerkleUpdateBodiesAtDepth(from.ref(i), to.ref(i), childDepth, !special)
		if err != nil {
			return nil, nil, err
		}
		refsFrom[i] = nextFrom
		refsTo[i] = nextTo
	}
	updateFrom, err := copyCellWithRefs(from, refsFrom)
	if err != nil {
		return nil, nil, err
	}
	updateTo, err := copyCellWithRefs(to, refsTo)
	if err != nil {
		return nil, nil, err
	}
	return updateFrom, updateTo, nil
}

// buildReadSetPreparedCase produces the update through the producer this
// consumer has to round-trip with: the read set that recorded the transition.
func buildReadSetPreparedCase(tb testing.TB, spec preparedSpec, from, _ *Cell) preparedCase {
	tb.Helper()

	rs := NewReadSet(from)
	to, err := readSetPreparedTransition(rs.Root(), spec)
	if err != nil {
		return preparedCase{skip: "read set transition is not constructible: " + err.Error()}
	}
	update, applied, err := rs.CreateMerkleUpdateApplied(to)
	if err != nil {
		return preparedCase{skip: "read set update is not constructible: " + err.Error()}
	}
	if applied.HashKey() != to.HashKey() {
		tb.Fatalf("read set applied root differs from the destination it was built for")
	}
	if spec.mutation >= 0 {
		return preparedCase{skip: "read set cases carry no mutation"}
	}
	return preparedCase{from: from, to: to, update: update}
}

// readSetPreparedTransition reads part of the tree through the trace and
// rebuilds only what it read, which is what a collation does.
func readSetPreparedTransition(root *Cell, spec preparedSpec) (*Cell, error) {
	var walk func(c *Cell, depth int) (*Cell, error)
	walk = func(c *Cell, depth int) (*Cell, error) {
		if depth > 3 || c.RefsNum() == 0 {
			return c, nil
		}
		refs := make([]*Cell, c.RefsNum())
		for i := range refs {
			ref, err := c.PeekRef(i)
			if err != nil {
				return nil, err
			}
			if i == 0 || spec.changes < 0 {
				if refs[i], err = walk(ref, depth+1); err != nil {
					return nil, err
				}
				continue
			}
			refs[i] = ref
		}
		builder := BeginCell()
		if err := storeBitSpan(builder, cellBits(c)); err != nil {
			return nil, err
		}
		if err := builder.StoreUInt(uint64(depth), 4); err != nil {
			return nil, err
		}
		for _, ref := range refs {
			if err := builder.StoreRef(ref); err != nil {
				return nil, err
			}
		}
		return builder.EndCell(), nil
	}
	// The walk must go through the traced root so the reads are recorded.
	return walk(root, 0)
}

// ---------------------------------------------------------------- mutations

// preparedMutations is one forged cell per validateLoadedCell rejection branch.
// Each is spliced once into the source body and once into the destination body,
// because those are two different error wrappers.
type preparedMutation struct {
	name  string
	forge func(tb testing.TB, victim *Cell) *Cell
}

var preparedMutations = []preparedMutation{
	{"ordinary_mask", func(tb testing.TB, victim *Cell) *Cell {
		return forgeCell(tb, victim.data, victim.bitsSz, false, LevelMask{Mask: 1}, nil)
	}},
	{"special_short", func(tb testing.TB, _ *Cell) *Cell {
		return forgeCell(tb, []byte{0x01}, 4, true, LevelMask{}, nil)
	}},
	{"pruned_with_ref", func(tb testing.TB, victim *Cell) *Cell {
		data := prunedPayload(victim, 1)
		return forgeCell(tb, data, uint16(len(data)*8), true, LevelMask{Mask: 1},
			[]*Cell{BeginCell().MustStoreUInt(1, 8).EndCell()})
	}},
	{"pruned_short_data", func(tb testing.TB, _ *Cell) *Cell {
		return forgeCell(tb, []byte{0x01}, 8, true, LevelMask{Mask: 1}, nil)
	}},
	{"pruned_mask_byte", func(tb testing.TB, victim *Cell) *Cell {
		data := prunedPayload(victim, 1)
		data[1] = 2
		return forgeCell(tb, data, uint16(len(data)*8), true, LevelMask{Mask: 1}, nil)
	}},
	{"pruned_level_zero", func(tb testing.TB, victim *Cell) *Cell {
		data := prunedPayload(victim, 0)
		return forgeCell(tb, data, uint16(len(data)*8), true, LevelMask{}, nil)
	}},
	{"pruned_bit_size", func(tb testing.TB, victim *Cell) *Cell {
		data := prunedPayload(victim, 1)
		return forgeCell(tb, data, uint16(len(data)*8-8), true, LevelMask{Mask: 1}, nil)
	}},
	{"library_with_ref", func(tb testing.TB, _ *Cell) *Cell {
		data := make([]byte, 1+hashSize)
		data[0] = byte(LibraryCellType)
		return forgeCell(tb, data, uint16(len(data)*8), true, LevelMask{}, []*Cell{BeginCell().EndCell()})
	}},
	{"library_bit_size", func(tb testing.TB, _ *Cell) *Cell {
		data := make([]byte, 1+hashSize-1)
		data[0] = byte(LibraryCellType)
		return forgeCell(tb, data, uint16(len(data)*8), true, LevelMask{}, nil)
	}},
	{"library_mask", func(tb testing.TB, _ *Cell) *Cell {
		data := make([]byte, 1+hashSize)
		data[0] = byte(LibraryCellType)
		return forgeCell(tb, data, uint16(len(data)*8), true, LevelMask{Mask: 1}, nil)
	}},
	{"proof_bit_size", func(tb testing.TB, victim *Cell) *Cell {
		data := proofPayload(victim)
		return forgeCell(tb, data[:len(data)-1], uint16((len(data)-1)*8), true, LevelMask{}, []*Cell{victim})
	}},
	{"proof_ref_count", func(tb testing.TB, victim *Cell) *Cell {
		data := proofPayload(victim)
		return forgeCell(tb, data, uint16(len(data)*8), true, LevelMask{}, []*Cell{victim, victim})
	}},
	{"proof_hash", func(tb testing.TB, victim *Cell) *Cell {
		data := proofPayload(victim)
		data[1] ^= 0xFF
		return forgeCell(tb, data, uint16(len(data)*8), true, LevelMask{}, []*Cell{victim})
	}},
	{"proof_depth", func(tb testing.TB, victim *Cell) *Cell {
		data := proofPayload(victim)
		binary.BigEndian.PutUint16(data[1+hashSize:], victim.getDepth(0)+1)
		return forgeCell(tb, data, uint16(len(data)*8), true, LevelMask{}, []*Cell{victim})
	}},
	{"proof_mask", func(tb testing.TB, victim *Cell) *Cell {
		data := proofPayload(victim)
		return forgeCell(tb, data, uint16(len(data)*8), true, LevelMask{Mask: 1}, []*Cell{victim})
	}},
	{"update_bit_size", func(tb testing.TB, victim *Cell) *Cell {
		data := nestedUpdatePayload(victim)
		return forgeCell(tb, data[:len(data)-1], uint16((len(data)-1)*8), true, LevelMask{}, []*Cell{victim, victim})
	}},
	{"update_ref_count", func(tb testing.TB, victim *Cell) *Cell {
		data := nestedUpdatePayload(victim)
		return forgeCell(tb, data, uint16(len(data)*8), true, LevelMask{}, []*Cell{victim})
	}},
	{"update_first_hash", func(tb testing.TB, victim *Cell) *Cell {
		data := nestedUpdatePayload(victim)
		data[1] ^= 0xFF
		return forgeCell(tb, data, uint16(len(data)*8), true, LevelMask{}, []*Cell{victim, victim})
	}},
	{"update_second_hash", func(tb testing.TB, victim *Cell) *Cell {
		data := nestedUpdatePayload(victim)
		data[1+hashSize] ^= 0xFF
		return forgeCell(tb, data, uint16(len(data)*8), true, LevelMask{}, []*Cell{victim, victim})
	}},
	{"update_first_depth", func(tb testing.TB, victim *Cell) *Cell {
		data := nestedUpdatePayload(victim)
		binary.BigEndian.PutUint16(data[1+hashSize*2:], victim.getDepth(0)+1)
		return forgeCell(tb, data, uint16(len(data)*8), true, LevelMask{}, []*Cell{victim, victim})
	}},
	{"update_second_depth", func(tb testing.TB, victim *Cell) *Cell {
		data := nestedUpdatePayload(victim)
		binary.BigEndian.PutUint16(data[1+hashSize*2+depthSize:], victim.getDepth(0)+1)
		return forgeCell(tb, data, uint16(len(data)*8), true, LevelMask{}, []*Cell{victim, victim})
	}},
	{"update_mask", func(tb testing.TB, victim *Cell) *Cell {
		data := nestedUpdatePayload(victim)
		return forgeCell(tb, data, uint16(len(data)*8), true, LevelMask{Mask: 1}, []*Cell{victim, victim})
	}},
	{"unknown_special", func(tb testing.TB, _ *Cell) *Cell {
		data := make([]byte, 8)
		data[0] = 0x7F
		return forgeCell(tb, data, uint16(len(data)*8), true, LevelMask{}, nil)
	}},
}

func prunedPayload(victim *Cell, mask byte) []byte {
	data := make([]byte, 2+hashSize+depthSize)
	data[0] = byte(PrunedCellType)
	data[1] = mask
	copy(data[2:], victim.getHash(0))
	binary.BigEndian.PutUint16(data[2+hashSize:], victim.getDepth(0))
	return data
}

func proofPayload(victim *Cell) []byte {
	data := make([]byte, 1+hashSize+depthSize)
	data[0] = byte(MerkleProofCellType)
	copy(data[1:], victim.getHash(0))
	binary.BigEndian.PutUint16(data[1+hashSize:], victim.getDepth(0))
	return data
}

func nestedUpdatePayload(victim *Cell) []byte {
	data := make([]byte, 1+hashSize*2+depthSize*2)
	data[0] = byte(MerkleUpdateCellType)
	copy(data[1:], victim.getHash(0))
	copy(data[1+hashSize:], victim.getHash(0))
	binary.BigEndian.PutUint16(data[1+hashSize*2:], victim.getDepth(0))
	binary.BigEndian.PutUint16(data[1+hashSize*2+depthSize:], victim.getDepth(0))
	return data
}

// forgeCell builds a cell the ordinary constructors would refuse. It is the
// only way to reach the validateLoadedCell branches from a test: the eager BOC
// parser enforces them at deserialization, which is precisely why Apply alone
// cannot be trusted to reach them on a lazy or virtualized tree.
func forgeCell(tb testing.TB, data []byte, bitsSz uint16, special bool, mask LevelMask, refs []*Cell) *Cell {
	tb.Helper()

	payload := append([]byte(nil), data...)
	c := &Cell{data: payload, bitsSz: bitsSz}
	c.setSpecial(special)
	c.setLevelMask(mask)
	if len(refs) > 0 {
		c.setRefs(refs)
	}
	if err := c.calculateHashes(); err != nil {
		// A forged shape the hash machinery itself refuses is not a case: the
		// tree could never carry it.
		return nil
	}
	return c
}

// applyPreparedMutation replaces the first reference of body with a forged
// cell, rebuilding body around it without re-deriving its level mask — the
// point is to produce a tree the walks must reject, so nothing on the way may
// repair it.
func applyPreparedMutation(tb testing.TB, mutation int, body *Cell) *Cell {
	tb.Helper()

	if body.refsCount() == 0 {
		return nil
	}
	victim := body.ref(0)
	forged := preparedMutations[mutation].forge(tb, victim)
	if forged == nil {
		return nil
	}
	refs := append([]*Cell(nil), body.rawRefs()...)
	refs[0] = forged
	return rawRebuild(tb, body, refs)
}

// rawRebuild replaces a cell's references while keeping its bits, special flag
// and declared level mask, so a mutation that makes the mask disagree with the
// refs stays visible instead of being normalized away.
func rawRebuild(tb testing.TB, src *Cell, refs []*Cell) *Cell {
	tb.Helper()

	c := src.copy()
	c.clearVirtualization()
	mask := src.getLevelMask()
	c.setRefs(refs)
	c.setSpecial(src.IsSpecial())
	c.setLevelMask(mask)
	if err := c.calculateHashes(); err != nil {
		return nil
	}
	return c
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
