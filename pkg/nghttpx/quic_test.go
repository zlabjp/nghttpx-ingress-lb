package nghttpx

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// TestCreateQUICSecretFile verifies CreateQUICSecretFile.
func TestCreateQUICSecretFile(t *testing.T) {
	const (
		checksum = "82250fc8b3025b041828098302262e06b68465d82a3de999e785033be6f1150a"
	)

	content := []byte("hello quic")

	f := CreateQUICSecretFile("/foo/bar", content)

	if got, want := f.Path, "/foo/bar/quic-secret.txt"; got != want {
		t.Errorf("f.Path = %v, want %v", got, want)
	}
	if got, want := f.Content, content; !bytes.Equal(got, want) {
		t.Errorf("f.Content = %q, want %q", got, want)
	}
	if got, want := hex.EncodeToString(f.Checksum), checksum; got != want {
		t.Errorf("f.Checksum = %v, want %v", got, want)
	}
}
