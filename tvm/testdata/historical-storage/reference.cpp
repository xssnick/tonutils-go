#include "common/bigint.hpp"
#include <iostream>
#include <cstdlib>
#include <random>
#include <vector>

// Standalone fatal-check sink; the reference arithmetic is linked unchanged.
namespace td::detail {
void process_check_error(const char* message, const char* file, int line) {
  std::cerr << file << ':' << line << ": " << message << '\n';
  std::abort();
}
}

struct Window {
  long long bit_price, cell_price;
  unsigned delta;
};

void vector(long long cells, long long bits, const std::vector<Window>& windows, bool comma) {
  td::BigInt256 total{0};
  std::cout << (comma ? ",\n" : "") << "{\"cells\":" << cells << ",\"bits\":" << bits << ",\"windows\":[";
  for (unsigned i = 0; i < windows.size(); ++i) {
    const auto& w = windows[i];
    td::BigInt256 c{cells}, b{bits};
    c.mul_short(w.cell_price);
    b.mul_short(w.bit_price);
    b += c;
    b.mul_short(w.delta);
    if (b.sgn() < 0) std::abort();
    total += b;
    std::cout << (i ? "," : "") << "{\"bit_price\":" << w.bit_price
      << ",\"cell_price\":" << w.cell_price << ",\"delta\":" << w.delta << "}";
  }
  total.rshift(16, 1);
  const auto sign = total.sgn();
  total.normalize();
  std::cout << "],\"normalized_fee\":\"" << total.to_dec_string()
    << "\",\"collected_fee\":\"" << (sign == 0 ? "0" : total.to_dec_string()) << "\"}";
}

int main(int argc, char**) {
  if (argc > 1) {
    std::cout << "[\n";
    vector(218, 6250, {{1000, 500000, 1627999052 - 1588340301}}, false);
    for (long long cells : {0LL, 1LL, 65535LL, 65536LL, 65537LL, (1LL<<52)-1, 1LL<<52, (1LL<<52)+1}) {
      vector(cells, 0, {{0, 1, 1}}, true);
    }
    // A sum can cross 2^52 without growing its denormalized one-word length.
    vector(1LL<<51, 0, {{0, 1, 1}, {0, 1, 1}}, true);
    vector(1LL<<52, 0, {{0, 1, 65535}}, true);
    vector(1LL<<52, 0, {{0, 1, 65536}}, true);
    vector(1LL<<52, 0, {{0, 1, 65537}}, true);
    // Multiplication by zero does not discard an existing leading zero limb.
    vector(1LL<<52, 1, {{0, 0, 2}, {0, 1, 1}}, true);
    std::mt19937_64 rng(13503401);
    for (int i = 0; i < 200; ++i) {
      const auto cells = static_cast<long long>(rng() & ((1ULL << (i%43+1))-1));
      const auto bits = static_cast<long long>(rng() & ((1ULL << (i%48+1))-1));
      std::vector<Window> windows;
      for (int n = 0; n < i%4+1; ++n) {
        windows.push_back({static_cast<long long>(rng() & ((1ULL<<24)-1)),
          static_cast<long long>(rng() & ((1ULL<<28)-1)), static_cast<unsigned>(rng() % 100000000+1)});
      }
      vector(cells, bits, windows, true);
    }
    std::cout << "\n]\n";
    return 0;
  }
  td::BigInt256 cells{218}, bits{6250};
  cells.mul_short(500000);
  bits.mul_short(1000);
  bits += cells;
  bits.mul_short(1627999052 - 1588340301);
  std::cout << "partial_raw=" << bits.dump() << " sign=" << bits.sgn() << '\n';
  td::BigInt256 total{0};
  total += bits;
  total.rshift(16, 1);
  std::cout << "old_fee=" << total.dump() << " sign=" << total.sgn() << '\n';
  total.normalize();
  std::cout << "normalized_fee=" << total.dump() << " sign=" << total.sgn() << '\n';
}
