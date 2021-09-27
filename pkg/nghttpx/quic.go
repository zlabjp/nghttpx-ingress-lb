package nghttpx

import (
	"fmt"
	"path/filepath"
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
