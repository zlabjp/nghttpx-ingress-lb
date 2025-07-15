package flags

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNamespacedNameSet(t *testing.T) {
	tests := []struct {
		desc    string
		value   string
		want    NamespacedName
		wantErr bool
	}{
		{
			desc:    "empty input",
			wantErr: true,
		},
		{
			desc:  "good input",
			value: "foo/bar",
			want: NamespacedName{
				Name:      "bar",
				Namespace: "foo",
			},
		},
		{
			desc:    "without separator",
			value:   "foobar",
			wantErr: true,
		},
		{
			desc:    "empty name",
			value:   "foo/",
			wantErr: true,
		},
		{
			desc:    "empty namespace",
			value:   "/bar",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			nn := NamespacedName{}

			err := nn.Set(tt.value)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, nn)
		})
	}
}

func TestNamespacedNameString(t *testing.T) {
	tests := []struct {
		desc string
		nn   NamespacedName
		want string
	}{
		{
			desc: "empty",
		},
		{
			desc: "foo/bar",
			nn: NamespacedName{
				Name:      "bar",
				Namespace: "foo",
			},
			want: "foo/bar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.nn.String())
		})
	}
}
