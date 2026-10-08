//go:build cgo && tvm_cross_emulator

package tvm

import (
 "bytes"
 "fmt"
 "math/rand"
 "testing"
 "github.com/xssnick/tonutils-go/tvm/cell"
 "github.com/xssnick/tonutils-go/tvm/tuple"
)

func TestReviewCellProbe(t *testing.T) {
 r := rand.New(rand.NewSource(123456))
 pool := []*cell.Cell{cell.BeginCell().EndCell()}
 value := cell.BeginCell().MustStoreUInt(43, 6).EndCell().MustBeginParse()
 for i:=0; i<3000; i++ {
  bits := uint(r.Intn(20)); b := cell.BeginCell().MustStoreUInt(r.Uint64()&((1<<bits)-1),bits)
  for j:=r.Intn(5); j>0; j-- { b.MustStoreRef(pool[r.Intn(len(pool))]) }
  root:=b.EndCell(); pool=append(pool,root)
  n := int64(r.Intn(12)); key:=int64(r.Intn(1<<n)); keySlice:=cell.BeginCell().MustStoreUInt(uint64(key),uint(n)).EndCell().MustBeginParse()
  for _,op := range []uint16{0xf40e,0xf416,0xf426,0xf436,0xf45b,0xf486,0xf48e,0xf496,0xf49e,0xf47c,0xf47e,0xf4b3,0xf4b7,0xf470,0xf473,0xf4a8} {
   vals:=[]any{key,root,n}
   switch op {case 0xf416,0xf426,0xf436: vals=[]any{value,key,root,n}; case 0xf486,0xf48e,0xf496,0xf49e: vals=[]any{root,n}; case 0xf4b3,0xf4b7: vals=[]any{key,n,root,n}; case 0xf470: vals=[]any{value,keySlice,root,n};case 0xf473,0xf4a8:vals=[]any{keySlice,root,n} }
   code:=cell.BeginCell().MustStoreUInt(0x30,8).MustStoreUInt(uint64(op),16).EndCell()
   gs,err:=buildCrossStack(vals...);if err!=nil {t.Fatal(err)}; rs,err:=buildCrossStack(vals...);if err!=nil {t.Fatal(err)}
   g,err:=runGoCrossCode(code,testEmptyCell(),tuple.Tuple{},gs); if err!=nil {t.Fatal(err)}
   ref,err:=runReferenceCrossCode(code,testEmptyCell(),tuple.Tuple{},rs);if err!=nil {t.Fatal(err)}
   if g.exitCode!=ref.exitCode || g.gasUsed!=ref.gasUsed || !bytes.Equal(g.stack.Hash(),ref.stack.Hash()) { t.Fatalf("i=%d op=%x n=%d key=%d root=%x\ngo=%d/%d %s\nref=%d/%d %s",i,op,n,key,root.ToBOC(),g.exitCode,g.gasUsed,g.stack.Dump(),ref.exitCode,ref.gasUsed,ref.stack.Dump()) }
  }
  if i%1000==0 {fmt.Println("probe",i)}
 }
}

func TestReviewCellShrink(t *testing.T) {
 left:=cell.BeginCell().MustStoreUInt(0b0100,4).MustStoreUInt(42,8).EndCell()
 right:=cell.BeginCell().MustStoreUInt(0b001,3).EndCell()
 root:=cell.BeginCell().MustStoreUInt(0,2).MustStoreRef(left).MustStoreRef(right).EndCell()
 for _,op:=range []uint16{0xf496,0xf486,0xf45b} {
  vals:=[]any{root,int64(2)};if op==0xf45b {vals=[]any{int64(0),root,int64(2)}}
  code:=cell.BeginCell().MustStoreUInt(0x30,8).MustStoreUInt(uint64(op),16).EndCell()
  gs,_:=buildCrossStack(vals...);rs,_:=buildCrossStack(vals...)
  g,e:=runGoCrossCode(code,testEmptyCell(),tuple.Tuple{},gs);if e!=nil {t.Fatal(e)}
  ref,e:=runReferenceCrossCode(code,testEmptyCell(),tuple.Tuple{},rs);if e!=nil {t.Fatal(e)}
  t.Logf("op=%x root=%x go=%d/%d %s ref=%d/%d %s",op,root.ToBOC(),g.exitCode,g.gasUsed,g.stack.Dump(),ref.exitCode,ref.gasUsed,ref.stack.Dump())
 }
}
