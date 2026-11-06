package pow

import "encoding/binary"

var roundConstants = [...]uint64{
	0x0000000000000001, 0x0000000000008082, 0x800000000000808A, 0x8000000080008000,
	0x000000000000808B, 0x0000000080000001, 0x8000000080008081, 0x8000000000008009,
	0x000000000000008A, 0x0000000000000088, 0x0000000080008009, 0x000000008000000A,
	0x000000008000808B, 0x800000000000008B, 0x8000000000008089, 0x8000000000008003,
	0x8000000000008002, 0x8000000000000080, 0x000000000000800A, 0x800000008000000A,
	0x8000000080008081, 0x8000000000008080, 0x0000000080000001, 0x8000000080008008,
}

var rotations = [...]uint{0, 1, 62, 28, 27, 36, 44, 6, 55, 20, 3, 10, 43, 25, 39, 41, 45, 15, 21, 8, 18, 2, 61, 56, 14}

func rotateLeft(x uint64, n uint) uint64 {
	if n == 0 {
		return x
	}
	return (x << n) | (x >> (64 - n))
}

// HashV1 is DeepSeek's reduced-round Keccak hash used by its browser PoW.
func HashV1(input []byte) [32]byte {
	const rate = 136
	var state [25]uint64
	offset := 0
	for offset+rate <= len(input) {
		absorb(&state, input[offset:offset+rate])
		permuteV1(&state)
		offset += rate
	}
	var block [rate]byte
	n := copy(block[:], input[offset:])
	block[n] = 0x06
	block[rate-1] |= 0x80
	absorb(&state, block[:])
	permuteV1(&state)
	var digest [32]byte
	for i := 0; i < 4; i++ {
		binary.LittleEndian.PutUint64(digest[i*8:], state[i])
	}
	return digest
}

func absorb(state *[25]uint64, block []byte) {
	for i := 0; i < 17; i++ {
		state[i] ^= binary.LittleEndian.Uint64(block[i*8:])
	}
}

func permuteV1(a *[25]uint64) {
	for round := 1; round < 24; round++ {
		var c, d, b [25]uint64
		for x := 0; x < 5; x++ {
			c[x] = a[x] ^ a[x+5] ^ a[x+10] ^ a[x+15] ^ a[x+20]
		}
		for x := 0; x < 5; x++ {
			d[x] = c[(x+4)%5] ^ rotateLeft(c[(x+1)%5], 1)
		}
		for y := 0; y < 5; y++ {
			for x := 0; x < 5; x++ {
				a[x+5*y] ^= d[x]
			}
		}
		for y := 0; y < 5; y++ {
			for x := 0; x < 5; x++ {
				b[y+5*((2*x+3*y)%5)] = rotateLeft(a[x+5*y], rotations[x+5*y])
			}
		}
		for y := 0; y < 5; y++ {
			for x := 0; x < 5; x++ {
				a[x+5*y] = b[x+5*y] ^ (^b[(x+1)%5+5*y] & b[(x+2)%5+5*y])
			}
		}
		a[0] ^= roundConstants[round]
	}
}
