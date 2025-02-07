package main

import (
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"math/big"
)

type ExampleStruct struct {
	A     uint32             `tlb:"## 24"`
	Inner ExampleInnerStruct `tlb:"^"`
	DictA *cell.Dictionary   `tlb:"dict 32"`
}

type ExampleDeepStruct struct {
	C *big.Int `tlb:"## 128"`
}

type ExampleInnerStruct struct {
	B     uint32            `tlb:"## 32"`
	Deep  ExampleDeepStruct `tlb:"^"`
	DictB *cell.Dictionary  `tlb:"dict 128"`
}

func prepareExampleCell() (*cell.Cell, *ExampleStruct) {
	data := ExampleStruct{
		A:     0xAABBCC,
		DictA: cell.NewDict(32),
		Inner: ExampleInnerStruct{
			B:     0xDDEEFF,
			Deep:  ExampleDeepStruct{C: big.NewInt(1)},
			DictB: cell.NewDict(128),
		},
	}

	valueData := cell.BeginCell().
		MustStoreUInt(0x11223311, 32).
		MustStoreRef(cell.BeginCell().
			MustStoreStringSnake("hello tonutils-go").
			EndCell()).
		EndCell()

	_ = data.DictA.SetIntKey(big.NewInt(777), valueData)
	_ = data.DictA.SetIntKey(big.NewInt(778), valueData)
	_ = data.DictA.SetIntKey(big.NewInt(1), valueData)

	_ = data.Inner.DictB.SetIntKey(big.NewInt(222), valueData)
	_ = data.Inner.DictB.SetIntKey(big.NewInt(333), valueData)
	_ = data.Inner.DictB.SetIntKey(big.NewInt(1), valueData)

	cl, err := tlb.ToCell(data)
	if err != nil {
		panic(err)
	}

	return cl, &data
}

func main() {
	exampleCell, data := prepareExampleCell()

	println("Original cell tree structure:")
	println(exampleCell.Dump())

	// proof skeleton is a branched tree of references
	// to cells which will stay untouched in proof
	sk := cell.CreateProofSkeleton()
	sk.ProofRef(1) // we want ref with index [1] (DictA field) to be presented in our proof

	proof, err := exampleCell.CreateProof(sk)
	if err != nil {
		panic(err)
	}

	////////////////////////////////////////////////////

	println("\n\nProof of `DictA` field (but content is pruned):")
	println(proof.Dump())

	sk = cell.CreateProofSkeleton()
	// now we want to keep full Inner field content in proof, including its all child cells, so we use SetRecursive()
	sk.ProofRef(0).SetRecursive()

	proof, err = exampleCell.CreateProof(sk)
	if err != nil {
		panic(err)
	}

	////////////////////////////////////////////////////

	println("\n\nProof of `Inner` field (with content):")
	println(proof.Dump())

	sk = cell.CreateProofSkeleton()
	// now we want proof of ExampleDeepStruct cell data
	sk.ProofRef(0).ProofRef(0)

	proof, err = exampleCell.CreateProof(sk)
	if err != nil {
		panic(err)
	}

	println("\n\nProof of `Inner.Deep` field:")
	println(proof.Dump())

	////////////////////////////////////////////////////

	// Now we build complex proof,
	// we will proof two keys from two dictionaries on different levels
	sk = cell.CreateProofSkeleton()

	skDictA := sk.ProofRef(1) // DictA
	key := cell.BeginCell().MustStoreUInt(778, 32).EndCell()
	_, skKey, err := data.DictA.LoadValueWithProof(key, skDictA)
	if err != nil {
		panic(err)
	}
	skKey.SetRecursive() // leave full key+value tree in proof

	skDictB := sk.ProofRef(0).ProofRef(1) // DictB
	key = cell.BeginCell().MustStoreUInt(333, 128).EndCell()
	_, skKey2, err := data.Inner.DictB.LoadValueWithProof(key, skDictB)
	if err != nil {
		panic(err)
	}
	skKey2.SetRecursive() // leave full key+value tree in proof

	proof, err = exampleCell.CreateProof(sk)
	if err != nil {
		panic(err)
	}

	println("\n\nProof of 2 keys in 2 dictionaries on diff depth:")
	println(proof.Dump())
}
