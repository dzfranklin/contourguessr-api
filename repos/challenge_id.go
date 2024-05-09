package repos

import (
	"encoding/base32"
	"encoding/binary"
)

var encoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)
var endianness = binary.BigEndian

func encodeChallengeID(id int) string {
	if id <= 0 || id >= 0xFFFFFFFF {
		panic("id out of bounds")
	}

	bytes := make([]byte, 4)
	endianness.PutUint32(bytes[:], uint32(id))

	for bytes[0] == 0 {
		bytes = bytes[1:]
	}

	return encoding.EncodeToString(bytes[:])
}

func decodeChallengeID(id string) (int, error) {
	bytes, err := encoding.DecodeString(id)
	if err != nil {
		return 0, err
	}
	for len(bytes) < 4 {
		bytes = append([]byte{0}, bytes...)
	}
	return int(endianness.Uint32(bytes)), nil
}
