package nghttpx

import (
	"testing"

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
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	if got, want := string(encoded), `{"Path":"/path/to/file","Content":"[redacted]","Checksum":"MDAxMTIyMzM="}`; got != want {
		t.Errorf("b = %v, want %v", got, want)
	}
}
