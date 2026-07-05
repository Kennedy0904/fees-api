package fees

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

func newID(prefix string) string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic(fmt.Sprintf("generate id: %v", err))
	}
	return prefix + "_" + hex.EncodeToString(bytes[:])
}

func NewBillID() string {
	return newID("bill")
}

func NewLineItemID() string {
	return newID("line")
}
