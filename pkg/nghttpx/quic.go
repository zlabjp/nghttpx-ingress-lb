package nghttpx

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"math/bits"
	"path/filepath"
	"strings"
)

const (
	// QUICKeyingMaterialsSize is the size of QUIC keying materials in a binary form.
	QUICKeyingMaterialsSize = 68
	// QUICKeyingMaterialsEncodedSize is the size of QUIC keying materials in a hex encoded form.
	QUICKeyingMaterialsEncodedSize = QUICKeyingMaterialsSize * 2
)

// CreateQUICSecretFile creates given QUIC keying materials file.
func CreateQUICSecretFile(dir string, quicKeyingMaterials []byte) *PrivateChecksumFile {
	checksum := Checksum(quicKeyingMaterials)

	return &PrivateChecksumFile{
		Path:     filepath.Join(dir, "quic-secret.txt"),
		Content:  quicKeyingMaterials,
		Checksum: checksum,
	}
}

// writeQUICSecretFile writes QUIC secret file.  If ingConfig.QUICSecretFile is nil, this function does nothing, and succeeds.
func writeQUICSecretFile(ingConfig *IngressConfig) error {
	if ingConfig.QUICSecretFile == nil {
		return nil
	}

	f := ingConfig.QUICSecretFile
	if err := WriteFile(f.Path, f.Content); err != nil {
		return fmt.Errorf("unable to write QUIC secret file: %w", err)
	}

	return nil
}

// VerifyQUICKeyingMaterials verifies that km is a well formatted QUIC keying material.
func VerifyQUICKeyingMaterials(km []byte) error {
	sc := bufio.NewScanner(bytes.NewBuffer(km))

	var idBits uint8

	for sc.Scan() {
		l := sc.Text()
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}

		if ln := len(l); ln != QUICKeyingMaterialsEncodedSize {
			return fmt.Errorf("each line of QUIC keying materials must be %v bytes long: %v", QUICKeyingMaterialsEncodedSize, ln)
		}

		b, err := hex.DecodeString(l)
		if err != nil {
			return fmt.Errorf("unable to decode QUIC keying materials from hex string: %w", err)
		}

		id := b[0] >> 6
		mask := uint8(1 << id)

		if (idBits & mask) != 0 {
			return fmt.Errorf("duplicate ID found: %#x", id)
		}

		idBits |= mask
	}

	if err := sc.Err(); err != nil {
		return fmt.Errorf("unable to read QUIC keying materials: %w", err)
	}

	if n := bits.OnesCount8(idBits); n < 2 {
		return fmt.Errorf("requires at least 2 keys: %v keys found", n)
	}

	return nil
}

// NewQUICKeyingMaterial returns new QUIC keying material.
func NewQUICKeyingMaterial() ([]byte, error) {
	b := make([]byte, QUICKeyingMaterialsSize)

	if err := GenerateCryptoKey(b, []byte("quic key")); err != nil {
		return nil, err
	}

	return b, nil
}

func NewInitialQUICKeyingMaterials() ([]byte, error) {
	keys := make([]string, 2)

	for i := range keys {
		b, err := NewQUICKeyingMaterial()
		if err != nil {
			return nil, err
		}

		b[0] = (b[0] & 0x3f) | byte((i << 6))

		keys[i] = hex.EncodeToString(b)
	}

	return []byte(strings.Join(keys, "\n")), nil
}

// UpdateQUICKeyingMaterials calls UpdateQUICKeyingMaterialsFunc with NewQUICKeyingMaterial.
func UpdateQUICKeyingMaterials(km []byte) ([]byte, error) {
	return UpdateQUICKeyingMaterialsFunc(km, NewQUICKeyingMaterial)
}

// UpdateQUICKeyingMaterialsFunc generates new keying material via newKeyingMaterialFunc, and rotates keying materials, then returns new
// QUIC keying materials.  VerifyQUICKeyingMaterials should be called against km and ensure that it succeeds before calling this function.
//
// km must include at least 2 keying materials.  New keying material is placed to the last.  Because the first keying material is used for
// encryption, new keying material is not used for encryption immediately.  It is started to be used for encryption after the next rotation
// in order to ensure that all controllers see this keying material.  The first 2 bits identifies the key, therefore at most 4 keying
// materials are retained.  The oldest keying materials are discarded if the number of keys exceeds such limit.
//
// The rotation works as follows:
//
// 1. Move the last keying material (which is the new keying material generated in the previous update) to the first.
// 2. Discard oldest keying materials if the number of keys exceeds 3.
// 3. Generate new keying material and place it to the last.
func UpdateQUICKeyingMaterialsFunc(km []byte, newKeyingMaterialFunc func() ([]byte, error)) ([]byte, error) {
	var keys []string

	sc := bufio.NewScanner(bytes.NewBuffer(km))

	for sc.Scan() {
		l := sc.Text()
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}

		if len(l) != QUICKeyingMaterialsEncodedSize {
			return nil, fmt.Errorf("not %v bytes long", QUICKeyingMaterialsEncodedSize)
		}

		if _, err := hex.DecodeString(l); err != nil {
			return nil, err
		}

		keys = append(keys, l)
	}

	if len(keys) < 2 {
		return nil, fmt.Errorf("requires at least 2 keys: %v keys found", len(keys))
	}

	// The last key is the new key generated in the last update.
	lastKM := keys[len(keys)-1]

	b, err := hex.DecodeString(lastKM[0:2])
	if err != nil {
		return nil, err
	}

	nextID := (b[0] + 0x40) & 0xc0

	newKM, err := newKeyingMaterialFunc()
	if err != nil {
		return nil, err
	}

	newKM[0] = (newKM[0] & 0x3f) | nextID

	var newKeysLen int
	if len(keys) < 4 {
		newKeysLen = len(keys) + 1
	} else {
		newKeysLen = 4
	}

	newKeys := make([]string, newKeysLen)
	newKeys[0] = lastKM
	copy(newKeys[1:], keys[:newKeysLen-2])
	newKeys[newKeysLen-1] = hex.EncodeToString(newKM)

	return []byte(strings.Join(newKeys, "\n")), nil
}
