package main

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
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
	exampleCell, _ := prepareExampleCell()

	fmt.Println("Original cell tree structure:")
	fmt.Println(exampleCell.Dump())

	// ProofTrace follows the actual reads you performed and turns them into a proof skeleton.
	trace := cell.NewProofTrace()
	root := exampleCell.BeginParse().SetObserver(trace)
	if _, err := root.LoadUInt(24); err != nil {
		panic(fmt.Errorf("failed to load ExampleStruct.A: %w", err))
	}
	if _, err := root.LoadRefCell(); err != nil {
		panic(fmt.Errorf("failed to skip ExampleStruct.Inner ref: %w", err))
	}
	if _, err := root.LoadDict(32); err != nil {
		panic(fmt.Errorf("failed to load ExampleStruct.DictA: %w", err))
	}

	proof, err := exampleCell.CreateProof(trace.Skeleton())
	if err != nil {
		panic(err)
	}

	fmt.Println("\n\nProof of `DictA` field access path (dictionary content is still pruned):")
	fmt.Println(proof.Dump())

	trace = cell.NewProofTrace()
	root = exampleCell.BeginParse().SetObserver(trace)
	if _, err = root.LoadUInt(24); err != nil {
		panic(fmt.Errorf("failed to load ExampleStruct.A: %w", err))
	}
	innerObserved, err := root.LoadRef()
	if err != nil {
		panic(fmt.Errorf("failed to load ExampleStruct.Inner ref: %w", err))
	}
	trace.MarkRecursive(innerObserved) // keep whole Inner subtree readable in proof

	proof, err = exampleCell.CreateProof(trace.Skeleton())
	if err != nil {
		panic(err)
	}

	fmt.Println("\n\nProof of `Inner` field (with content):")
	fmt.Println(proof.Dump())

	trace = cell.NewProofTrace()
	root = exampleCell.BeginParse().SetObserver(trace)
	if _, err = root.LoadUInt(24); err != nil {
		panic(fmt.Errorf("failed to load ExampleStruct.A: %w", err))
	}
	innerObserved, err = root.LoadRef()
	if err != nil {
		panic(fmt.Errorf("failed to load ExampleStruct.Inner ref: %w", err))
	}
	if _, err = innerObserved.LoadUInt(32); err != nil {
		panic(err)
	}
	deepObserved, err := innerObserved.LoadRef()
	if err != nil {
		panic(err)
	}
	deepValue, err := deepObserved.LoadBigInt(128)
	if err != nil {
		panic(err)
	}

	proof, err = exampleCell.CreateProof(trace.Skeleton())
	if err != nil {
		panic(err)
	}

	fmt.Printf("\n\nProof of `Inner.Deep` field (`C = %s`):\n", deepValue.String())
	fmt.Println(proof.Dump())

	// Now we build a complex proof:
	// we prove two values from two dictionaries on different levels.
	trace = cell.NewProofTrace()

	root = exampleCell.BeginParse().SetObserver(trace)
	if _, err = root.LoadUInt(24); err != nil {
		panic(fmt.Errorf("failed to load ExampleStruct.A: %w", err))
	}
	if _, err = root.LoadRefCell(); err != nil {
		panic(fmt.Errorf("failed to skip ExampleStruct.Inner ref: %w", err))
	}
	dictA, err := root.LoadDict(32)
	if err != nil {
		panic(fmt.Errorf("failed to load ExampleStruct.DictA: %w", err))
	}
	valA, err := dictA.LoadValueByIntKey(big.NewInt(778))
	if err != nil {
		panic(err)
	}
	trace.MarkRecursive(valA) // keep full value tree inside proof

	root = exampleCell.BeginParse().SetObserver(trace)
	if _, err = root.LoadUInt(24); err != nil {
		panic(fmt.Errorf("failed to load ExampleStruct.A: %w", err))
	}
	innerObserved, err = root.LoadRef()
	if err != nil {
		panic(fmt.Errorf("failed to load ExampleStruct.Inner ref: %w", err))
	}
	if _, err = innerObserved.LoadUInt(32); err != nil {
		panic(err)
	}
	if _, err = innerObserved.LoadRefCell(); err != nil {
		panic(err)
	}
	dictB, err := innerObserved.LoadDict(128)
	if err != nil {
		panic(err)
	}
	valB, err := dictB.LoadValueByIntKey(big.NewInt(333))
	if err != nil {
		panic(err)
	}
	trace.MarkRecursive(valB) // keep second value tree too

	proof, err = exampleCell.CreateProof(trace.Skeleton())
	if err != nil {
		panic(err)
	}

	fmt.Println("\n\nProof of 2 keys in 2 dictionaries on diff depth:")
	fmt.Println(proof.Dump())

	fmt.Println("Checking and parsing proof:")
	expectedHash := exampleCell.Hash()

	proofBody, err := cell.UnwrapProof(proof, expectedHash)
	if err != nil {
		panic(err)
	}

	fmt.Println("proof verified, now we can trust its body and parse it")

	var dataProof ExampleStruct
	// LoadFromCellAsProof loads only non-pruned branches into the target struct.
	if err = tlb.LoadFromCellAsProof(&dataProof, proofBody.BeginParse()); err != nil {
		panic(err)
	}

	valA, err = dataProof.DictA.LoadValueByIntKey(big.NewInt(778))
	if err != nil {
		panic(err)
	}
	valB, err = dataProof.Inner.DictB.LoadValueByIntKey(big.NewInt(333))
	if err != nil {
		panic(err)
	}

	fmt.Println("Trusted data inside proof mapped to struct:")
	fmt.Println("A:", dataProof.A)
	fmt.Println("DictA[778]:", valA.String())
	fmt.Println("DictB[333]:", valB.String())
	fmt.Println("Not pruned dictA keys:\n", dataProof.DictA.String())
}
