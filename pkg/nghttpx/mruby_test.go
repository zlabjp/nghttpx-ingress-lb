package nghttpx

import (
	"bytes"
	"fmt"
	"testing"
)

// TestCreatePerPatternMrubyChecksumFile verifies CreatePerPatternMrubyChecksumFile.
func TestCreatePerPatternMrubyChecksumFile(t *testing.T) {
	const (
		checksum = "931feef4efcddfbb1084be5b259238d2802ac027e97e1c067ba8f4b8121f5693"
	)

	content := []byte("hello mruby")

	f := CreatePerPatternMrubyChecksumFile("/foo/bar", content)

	if got, want := f.Path, fmt.Sprintf("/foo/bar/mruby/%v.rb", checksum); got != want {
		t.Errorf("f.path = %v, want %v", got, want)
	}
	if got, want := f.Content, content; !bytes.Equal(got, want) {
		t.Errorf("f.Content = %q, want %q", got, want)
	}
	if got, want := f.Checksum, checksum; got != want {
		t.Errorf("f.Checksum = %v, want %v", got, want)
	}
}
