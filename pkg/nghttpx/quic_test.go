package nghttpx

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateQUICSecretFile verifies CreateQUICSecretFile.
func TestCreateQUICSecretFile(t *testing.T) {
	const (
		checksum = "82250fc8b3025b041828098302262e06b68465d82a3de999e785033be6f1150a"
	)

	content := []byte("hello quic")

	f := CreateQUICSecretFile("/foo/bar", content)

	assert.Equal(t, "/foo/bar/quic-secret.txt", f.Path)
	assert.Equal(t, content, f.Content)
	assert.Equal(t, checksum, hex.EncodeToString(f.Checksum))
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
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
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
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.want, km)
		})
	}
}

func TestNewInitialQUICKeyingMaterials(t *testing.T) {
	o, err := NewInitialQUICKeyingMaterials()
	require.NoError(t, err)

	keys := bytes.SplitN(o, []byte("\n"), 2)
	require.Len(t, keys, 2)

	for i, key := range keys {
		k := make([]byte, 1)
		_, err := hex.Decode(k, key[:2])
		require.NoError(t, err)
		assert.Equal(t, byte(i<<(8-idNBits)), k[0]&idMask)
	}
}
