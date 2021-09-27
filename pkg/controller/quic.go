package controller

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

// verifyQUICKeyingMaterials verifies that km is a well formatted QUIC keying material.
func verifyQUICKeyingMaterials(km []byte) error {
	sc := bufio.NewScanner(bytes.NewBuffer(km))

	for sc.Scan() {
		l := sc.Text()
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}

		if ln := len(l); ln != 136 {
			return fmt.Errorf("each line of QUIC keying materials must be 136 bytes long: %v", ln)
		}

		if _, err := hex.DecodeString(l); err != nil {
			return fmt.Errorf("could not decode QUIC keying materials from hex string: %v", err)
		}
	}

	if err := sc.Err(); err != nil {
		return fmt.Errorf("could not read QUIC keying materials: %v", err)
	}

	return nil
}

// newQUICKeyingMaterial returns new QUIC keying material.
func newQUICKeyingMaterial() []byte {
	b := make([]byte, 32+32+4)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}

	return b
}

// updateQUICKeyingMaterials updates the existing QUIC keying materials km.  km may be nil or empty.
func updateQUICKeyingMaterials(km []byte) []byte {
	return updateQUICKeyingMaterialsFunc(km, newQUICKeyingMaterial)
}

func updateQUICKeyingMaterialsFunc(km []byte, newKeyingMaterialFunc func() []byte) []byte {
	var keys []string

	sc := bufio.NewScanner(bytes.NewBuffer(km))

	for sc.Scan() {
		l := sc.Text()
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}

		if len(l) != 136 {
			panic("not 136 bytes long")
		}

		if _, err := hex.DecodeString(l); err != nil {
			panic(err)
		}

		keys = append(keys, l)

		if len(keys) == 3 {
			break
		}
	}

	if len(keys) == 0 {
		return []byte(hex.EncodeToString(newKeyingMaterialFunc()))
	}

	p := keys[0]
	b, err := hex.DecodeString(p[0:2])
	if err != nil {
		panic(err)
	}

	nextID := (b[0] + 0x40) & 0xc0

	k := newKeyingMaterialFunc()
	k[0] = (k[0] & 0x3f) | nextID

	newKeys := []string{hex.EncodeToString(k)}
	newKeys = append(newKeys, keys...)

	return []byte(strings.Join(newKeys, "\n"))
}
