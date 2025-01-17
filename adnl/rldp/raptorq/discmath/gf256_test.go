package discmath

import (
	"math/rand"
	"testing"
	"time"
)

func BenchmarkOctVecMulAdd(b *testing.B) {
	// Инициализируем некие данные
	rand.Seed(time.Now().UnixNano())

	// Размер входных данных. Меняйте на нужный.
	const size = 1024 * 16 // 16 KB

	x := make([]byte, size)
	y := make([]byte, size)
	for i := 0; i < size; i++ {
		x[i] = byte(rand.Intn(256))
		y[i] = byte(rand.Intn(256))
	}

	multiplier := uint8(rand.Intn(256))

	// Сбрасываем таймер, чтобы инициализация не учитывалась в бенчмарке
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Копируем x в temp, чтобы каждый прогон начинался с одинаковых исходных данных
		// (и результат XOR был сопоставим)
		temp := make([]byte, size)
		copy(temp, x)

		// Запускаем саму функцию
		OctVecMulAdd(temp, y, multiplier)
	}
}
