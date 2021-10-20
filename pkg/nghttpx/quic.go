package nghttpx

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
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
func CreateQUICSecretFile(dir string, quicKeyingMaterials []byte) *ChecksumFile {
	checksum := Checksum(quicKeyingMaterials)
	return &ChecksumFile{
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
		return fmt.Errorf("failed to write QUIC secret file: %v", err)
	}

	return nil
}

// VerifyQUICKeyingMaterials verifies that km is a well formatted QUIC keying material.
func VerifyQUICKeyingMaterials(km []byte) error {
	sc := bufio.NewScanner(bytes.NewBuffer(km))

	for sc.Scan() {
		l := sc.Text()
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}

		if ln := len(l); ln != QUICKeyingMaterialsEncodedSize {
			return fmt.Errorf("each line of QUIC keying materials must be %v bytes long: %v", QUICKeyingMaterialsEncodedSize, ln)
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

// NewQUICKeyingMaterial returns new QUIC keying material.
func NewQUICKeyingMaterial() []byte {
	b := make([]byte, QUICKeyingMaterialsSize)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}

	return b
}

// UpdateQUICKeyingMaterials updates the existing QUIC keying materials km.  km may be nil or empty.
func UpdateQUICKeyingMaterials(km []byte) []byte {
	return updateQUICKeyingMaterialsFunc(km, NewQUICKeyingMaterial)
}

func updateQUICKeyingMaterialsFunc(km []byte, newKeyingMaterialFunc func() []byte) []byte {
	var keys []string

	sc := bufio.NewScanner(bytes.NewBuffer(km))

	for sc.Scan() {
		l := sc.Text()
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}

		if len(l) != QUICKeyingMaterialsEncodedSize {
			panic(fmt.Sprintf("not %v bytes long", QUICKeyingMaterialsEncodedSize))
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
