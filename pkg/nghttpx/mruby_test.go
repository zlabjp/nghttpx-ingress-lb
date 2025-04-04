package nghttpx

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCreatePerPatternMrubyChecksumFile verifies CreatePerPatternMrubyChecksumFile.
func TestCreatePerPatternMrubyChecksumFile(t *testing.T) {
	const (
		checksum = "931feef4efcddfbb1084be5b259238d2802ac027e97e1c067ba8f4b8121f5693"
	)

	content := []byte("hello mruby")

	f := CreatePerPatternMrubyChecksumFile("/foo/bar", content)

	assert.Equal(t, "/foo/bar/mruby/"+string(checksum)+".rb", f.Path)
	assert.Equal(t, content, f.Content)
	assert.Equal(t, checksum, hex.EncodeToString(f.Checksum))
}
