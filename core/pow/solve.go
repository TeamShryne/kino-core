package pow

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
)

var ErrNoSolution = errors.New("proof-of-work challenge has no solution in its difficulty range")

// Solve searches [0, difficulty) and checks ctx regularly so Android can stop
// the CPU work when the user cancels a request.
func Solve(ctx context.Context, salt string, expireAt int64, challengeHex string, difficulty uint64) (uint64, error) {
	targetBytes, err := hex.DecodeString(challengeHex)
	if err != nil || len(targetBytes) != 32 {
		return 0, fmt.Errorf("invalid challenge digest")
	}
	if difficulty == 0 {
		return 0, ErrNoSolution
	}
	target := [4]uint64{}
	for i := range target {
		target[i] = uint64(targetBytes[i*8]) | uint64(targetBytes[i*8+1])<<8 | uint64(targetBytes[i*8+2])<<16 | uint64(targetBytes[i*8+3])<<24 |
			uint64(targetBytes[i*8+4])<<32 | uint64(targetBytes[i*8+5])<<40 | uint64(targetBytes[i*8+6])<<48 | uint64(targetBytes[i*8+7])<<56
	}

	prefix := []byte(fmt.Sprintf("%s_%d_", salt, expireAt))
	if len(prefix) >= 136 {
		return 0, fmt.Errorf("challenge prefix exceeds supported hash input")
	}
	var base [25]uint64
	for i, b := range prefix {
		base[i>>3] ^= uint64(b) << ((i & 7) * 8)
	}
	workers := runtime.GOMAXPROCS(0)
	if workers > 4 {
		workers = 4 // cap CPU use on mobile devices
	}
	if workers < 1 {
		workers = 1
	}
	if difficulty < uint64(workers) {
		workers = 1
	}
	chunk := difficulty / uint64(workers)
	var found atomic.Bool
	var answer atomic.Uint64
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		start := uint64(worker) * chunk
		end := start + chunk
		if worker == workers-1 {
			end = difficulty
		}
		wg.Add(1)
		go func(start, end uint64) {
			defer wg.Done()
			var state [25]uint64
			var digits [20]byte
			for candidate := start; candidate < end; candidate++ {
				if found.Load() || ctx.Err() != nil {
					return
				}
				state = base
				text := strconv.AppendUint(digits[:0], candidate, 10)
				offset := len(prefix)
				for _, b := range text {
					state[offset>>3] ^= uint64(b) << ((offset & 7) * 8)
					offset++
				}
				state[offset>>3] ^= uint64(0x06) << ((offset & 7) * 8)
				state[16] ^= 0x8000000000000000
				permuteV1(&state)
				if state[0] == target[0] && state[1] == target[1] && state[2] == target[2] && state[3] == target[3] {
					if found.CompareAndSwap(false, true) {
						answer.Store(candidate)
					}
					return
				}
			}
		}(start, end)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if !found.Load() {
		return 0, ErrNoSolution
	}
	return answer.Load(), nil
}
