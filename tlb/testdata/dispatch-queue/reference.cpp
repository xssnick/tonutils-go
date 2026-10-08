// Generates the cell-hash fixtures in TestDispatchQueueBalancesAcrossMixedForks.
// Build with TON testnet 2fc0ce583a4e9a1d4c6ab9731672cc5d8d6d590d, using
// the include paths and static libraries of its test targets (C++20).
#include "block/block-parse.h"
#include "vm/vm.h"
#include <iostream>
#include <initializer_list>
using td::Ref;
Ref<vm::Cell> extra(std::initializer_list<std::pair<unsigned, unsigned>> entries) {
  vm::Dictionary d{32};
  for (auto [id, value] : entries) {
    vm::CellBuilder cb;
    cb.store_long(1, 5);
    cb.store_long(value, 8);
    td::BitArray<32> key{id};
    if (!d.set(key, vm::load_cell_slice_ref(cb.finalize()))) throw "extra set";
  }
  return d.get_root_cell();
}
Ref<vm::Cell> account(std::initializer_list<unsigned long long> lts, const block::CurrencyCollection* balance) {
  vm::Dictionary messages{64};
  for (auto lt : lts) {
    vm::CellBuilder body;
    body.store_long(0xab, 8);
    vm::CellBuilder enqueued;
    enqueued.store_long(lt, 64);
    enqueued.store_ref(body.finalize());
    td::BitArray<64> key{(long long)lt};
    if (!messages.set(key, vm::load_cell_slice_ref(enqueued.finalize()))) throw "message set";
  }
  vm::CellBuilder cb;
  cb.store_long(balance ? 0 : 1, balance ? 3 : 1);
  cb.store_ref(messages.get_root_cell());
  cb.store_long(lts.size(), 48);
  if (balance && !block::tlb::t_CurrencyCollection.pack(cb, *balance)) throw "balance pack";
  return cb.finalize();
}
void print(const char* name, const vm::AugmentedDictionary& queue) {
  auto wrapped = queue.get_wrapped_dict_root();
  std::cout << name << "=" << wrapped->get_hash().to_hex() << std::endl;
  vm::CellBuilder extra;
  extra.append_cellslice(*queue.get_root_extra());
  std::cout << name << "_extra=" << extra.finalize()->get_hash().to_hex() << std::endl;
}
int main() {
  vm::init_vm().ensure();
  block::CurrencyCollection a{100, extra({{1,10},{2,20}})};
  block::CurrencyCollection b{200, extra({{2,5},{3,7}})};
  auto a_cell = account({800,850}, &a);
  auto b_cell = account({900}, &b);
  auto old_cell = account({700}, nullptr);
  std::cout << "account_a=" << a_cell->get_hash().to_hex() << std::endl;
  std::cout << "account_b=" << b_cell->get_hash().to_hex() << std::endl;
  std::cout << "account_old=" << old_cell->get_hash().to_hex() << std::endl;
  vm::AugmentedDictionary queue{256, block::tlb::aug_DispatchQueue};
  td::BitArray<256> key;
  key.set_zero(); (key.bits() + 248).store_uint(1,8);
  if (!queue.set(key, vm::load_cell_slice_ref(a_cell))) throw "queue set a";
  print("new_single", queue);
  key.set_zero(); (key.bits() + 248).store_uint(2,8);
  if (!queue.set(key, vm::load_cell_slice_ref(b_cell))) throw "queue set b";
  print("new_fork", queue);
  key.set_zero();
  if (!queue.set(key, vm::load_cell_slice_ref(old_cell))) throw "queue set old";
  print("mixed", queue);
  if (queue.lookup_delete(key.bits(), 256).is_null()) throw "queue delete old";
  print("restored", queue);
  key.set_zero(); (key.bits() + 248).store_uint(1,8);
  if (queue.lookup_delete(key.bits(), 256).is_null()) throw "queue delete a";
  print("new_b", queue);
  key.set_zero(); (key.bits() + 248).store_uint(2,8);
  if (queue.lookup_delete(key.bits(), 256).is_null()) throw "queue delete b";
  print("empty", queue);
}
