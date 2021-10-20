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

// TestVerifyQUICKeyingMaterials verifies VerifyQUICKeyingMaterials.
func TestVerifyQUICKeyingMaterials(t *testing.T) {
	tests := []struct {
		desc    string
		km      []byte
		wantErr bool
	}{
		{
			desc: "Empty input",
		},
		{
			desc:    "Line is too short",
			km:      []byte("deadbeef"),
			wantErr: true,
		},
		{
			desc:    "Not a hex string",
			km:      []byte("foo"),
			wantErr: true,
		},
		{
			desc: "Valid keying materials",
			km: []byte(`
# comment
00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233
30112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233
c0112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233
`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			err := VerifyQUICKeyingMaterials(tt.km)
			if err != nil {
				if !tt.wantErr {
					t.Fatalf("verifyQUICKeyingMaterials: %v", err)
				}
				return
			}

			if tt.wantErr {
				t.Fatal("verifyQUICKeyingMaterials should fail")
			}
		})
	}
}

// TestUpdateQUICKeyingMaterialsFunc verifies updateQUICKeyingMaterialsFunc.
func TestUpdateQUICKeyingMaterialsFunc(t *testing.T) {
	const newKM = "0a112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233"

	newKMFunc := func() []byte {
		b, err := hex.DecodeString(newKM)
		if err != nil {
			panic(err)
		}

		return b
	}

	tests := []struct {
		desc string
		km   []byte
		want []byte
	}{
		{
			desc: "Empty input",
			want: []byte(newKM),
		},
		{
			desc: "Make sure that ID is incremented",
			km: []byte(`# comment
40112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233

00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233`),
			want: []byte(`8a112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233
40112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233
00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233`),
		},
		{
			desc: "Make sure that ID is wrapped",
			km: []byte(`# comment
c0112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233
80112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233`),
			want: []byte(`0a112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233
c0112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233
80112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233`),
		},
		{
			desc: "Make sure that keying materials are limited to the latest 4 keys",
			km: []byte(`
80112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233
40112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233
00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233
c0112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233
`),
			want: []byte(`ca112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233
80112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233
40112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233
00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, want := updateQUICKeyingMaterialsFunc(tt.km, newKMFunc), tt.want; !bytes.Equal(got, want) {
				t.Errorf("updateQUICKeyingMaterialsFunc(...) = %s, want %s", got, want)
			}
		})
	}
}
