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
			desc:    "Empty input",
			wantErr: true,
		},
		{
			desc:    "Single key",
			km:      []byte("00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233"),
			wantErr: true,
		},
		{
			desc: "Line is too short",
			km: []byte("" +
				"00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
				"40112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff0011223",
			),
			wantErr: true,
		},
		{
			desc: "Not a hex string",
			km: []byte("" +
				"00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
				"40112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112foo",
			),
			wantErr: true,
		},
		{
			desc: "Valid keying materials",
			km: []byte("" +
				"# comment\n" +
				"00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
				"40112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
				"c0112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233",
			),
		},
		{
			desc: "Duplicated ID",
			km: []byte("" +
				"00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
				"40112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
				"40112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233",
			),
			wantErr: true,
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

// TestUpdateQUICKeyingMaterialsFunc verifies UpdateQUICKeyingMaterialsFunc.
func TestUpdateQUICKeyingMaterialsFunc(t *testing.T) {
	const newKM = "0a112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233"

	newKMFunc := func() ([]byte, error) {
		b, err := hex.DecodeString(newKM)
		if err != nil {
			panic(err)
		}

		return b, nil
	}

	tests := []struct {
		desc      string
		km        []byte
		want      []byte
		wantError bool
	}{
		{
			desc:      "Empty input",
			wantError: true,
		},
		{
			desc: "Make sure that ID is incremented",
			km: []byte("" +
				"# comment\n" +
				"00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
				"20112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233",
			),
			want: []byte("" +
				"20112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
				"00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
				"4a112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233",
			),
		},
		{
			desc: "Make sure that ID is wrapped",
			km: []byte("" +
				"# comment\n" +
				"c0112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
				"e0112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233",
			),
			want: []byte("" +
				"e0112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
				"c0112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
				"0a112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233",
			),
		},
		{
			desc: "Make sure that keying materials are limited to the latest 8 keys",
			km: []byte("" +
				"c0112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
				"a0112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
				"80112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
				"60112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
				"40112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
				"20112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
				"00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
				"e0112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233",
			),
			want: []byte("" +
				"e0112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
				"c0112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
				"a0112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
				"80112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
				"60112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
				"40112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
				"20112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
				"0a112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233",
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			km, err := UpdateQUICKeyingMaterialsFunc(tt.km, newKMFunc)
			if err != nil {
				if tt.wantError {
					return
				}

				t.Fatalf("UpdateQUICKeyingMaterialsFunc: %v", err)
			}

			if tt.wantError {
				t.Fatal("UpdateQUICKeyingMaterialsFunc should fail")
			}

			if got, want := km, tt.want; !bytes.Equal(got, want) {
				t.Errorf("UpdateQUICKeyingMaterialsFunc(...) = %s, want %s", got, want)
			}
		})
	}
}

func TestNewInitialQUICKeyingMaterials(t *testing.T) {
	o, err := NewInitialQUICKeyingMaterials()
	if err != nil {
		t.Fatalf("NewInitialQUICKeyingMaterials: %v", err)
	}

	keys := bytes.SplitN(o, []byte("\n"), 2)
	if got, want := len(keys), 2; got != want {
		t.Fatalf("len(keys) = %v, want %v", got, want)
	}

	for i, key := range keys {
		k := make([]byte, 1)
		if _, err := hex.Decode(k, key[:2]); err != nil {
			t.Fatalf("hex.DecodeString: %v", err)
		}

		if got, want := k[0]&idMask, byte(i<<(8-idNBits)); got != want {
			t.Errorf("id[%v] = %#02x, want %#02x", i, got, want)
		}
	}
}
