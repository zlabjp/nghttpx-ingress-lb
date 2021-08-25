package nghttpx

import (
	"fmt"
	"path/filepath"
)

const (
	// mrubyDir is the directory where per-pattern mruby script is stored.
	mrubyDir = "mruby"
)

// CreatePerPatternMrubyChecksumFile creates ChecksumFile for given mruby content.
func CreatePerPatternMrubyChecksumFile(dir string, mruby []byte) *ChecksumFile {
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

// writePerPatternMrubyFile writes per-pattern mruby script file.
func writePerPatternMrubyFile(ingConfig *IngressConfig) error {
	if err := MkdirAll(filepath.Join(ingConfig.ConfDir, mrubyDir)); err != nil {
		return err
	}
	for _, upstream := range ingConfig.Upstreams {
		if upstream.Mruby == nil {
			continue
		}
		if err := WriteFile(upstream.Mruby.Path, upstream.Mruby.Content); err != nil {
			return fmt.Errorf("failed to write per-pattern mruby file: %v", err)
		}
	}

	if f := ingConfig.HealthzMruby; f != nil {
		if err := WriteFile(f.Path, f.Content); err != nil {
			return fmt.Errorf("failed to write healthz mruby file: %v", err)
		}
	}

	return nil
}
