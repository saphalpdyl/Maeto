package controlplane

const (
	low  uint16 = 0xf001
	high uint16 = 0xffff
	n    uint16 = high - low + 1 // 4095 = 3^2 * 5 * 7 * 13

	// Hull-Dobell full-period LCG params mod n:
	//   a-1 must be divisible by every prime factor of n  -> a-1 = 1365 = 3*5*7*13
	//   c must be coprime to n
	a uint16 = 1366
	c uint16 = 1247
)

// Function that generates a hex number from low -> high
// Used to generate determinstic hex bits for SIDs from a sequential
// cursor
func permute(cursor uint16) uint16 {
	x := cursor % n
	x = (a*x + c) % n
	return uint16(low + x)
}
