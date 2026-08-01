package emv

import "fmt"

// CRC16 computes CRC-16/CCITT-FALSE: polynomial 0x1021, initial value 0xFFFF,
// no input or output reflection, no final XOR. That is the variant field 63 of
// the EMV QR Code specification requires, and the one BACEN's BR Code manual
// restates.
func CRC16(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc ^= uint16(b) << 8
		for range 8 {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// checksum returns the four uppercase hex digits that close a payload.
func checksum(payload string) string {
	return fmt.Sprintf("%04X", CRC16([]byte(payload)))
}
