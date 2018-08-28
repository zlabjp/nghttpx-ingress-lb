package nghttpx

import (
	"fmt"
	"path/filepath"
)

const (
	// mrubyDir is the directory where per-backend mruby script is stored.
	mrubyDir = "mruby"
)

// CreatePerBackendMrubyChecksumFile creates ChecksumFile for given mruby content.
func CreatePerBackendMrubyChecksumFile(dir string, mruby []byte) *ChecksumFile {
	checksum := Checksum(mruby)
	return &ChecksumFile{
		Path:     filepath.Join(dir, mrubyDir, checksum+".rb"),
		Content:  mruby,
		Checksum: Checksum(mruby),
	}
}

// writeMrubyFile writes mruby script file.  If ingConfig.MrubyFile is nil, this function does nothing, and succeeds.
func writeMrubyFile(ingConfig *IngressConfig) error {
	if ingConfig.MrubyFile == nil {
		return nil
	}

	f := ingConfig.MrubyFile
	if err := WriteFile(f.Path, f.Content); err != nil {
		return fmt.Errorf("failed to write mruby file: %v", err)
	}

	return nil
}

// writePerBackendMrubyFile writes per-backend mruby script file.
func writePerBackendMrubyFile(ingConfig *IngressConfig) error {
	if err := MkdirAll(filepath.Join(ingConfig.ConfDir, mrubyDir)); err != nil {
		return err
	}
	for _, upstream := range ingConfig.Upstreams {
		for i := range upstream.Backends {
			backend := &upstream.Backends[i]
			if backend.Mruby == nil {
				continue
			}
			if err := WriteFile(backend.Mruby.Path, backend.Mruby.Content); err != nil {
				return fmt.Errorf("failed to write per-backend mruby file: %v", err)
			}
		}
	}

	return nil
}
