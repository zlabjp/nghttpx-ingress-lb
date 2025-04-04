package nghttpx

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/json"
)

// TestPrivateChecksumFileMarshalJSON verifies PrivateChecksumFile.MarshalJSON.
func TestPrivateChecksumFileMarshalJSON(t *testing.T) {
	c := PrivateChecksumFile{
		Path:     "/path/to/file",
		Content:  []byte("private info"),
		Checksum: []byte("00112233"),
	}

	encoded, err := json.Marshal(c)
	require.NoError(t, err)
	assert.JSONEq(t, `{"Path":"/path/to/file","Content":"[redacted]","Checksum":"MDAxMTIyMzM="}`, string(encoded))
}
