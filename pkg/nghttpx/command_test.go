package nghttpx

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDeleteAssetFiles(t *testing.T) {
	tempDir := t.TempDir()

	modTime := time.Now()

	files := []string{"alpha", "bravo", "charlie"}
	for _, n := range files {
		require.NoError(t, os.WriteFile(filepath.Join(tempDir, n), []byte(n), 0o600))
	}

	tests := []struct {
		desc       string
		t          time.Time
		threshold  time.Duration
		wantDelete bool
	}{
		{
			desc:      "No files are stale",
			t:         modTime,
			threshold: time.Hour,
		},
		{
			desc:       "Files are stale",
			t:          modTime.Add(2 * time.Hour),
			threshold:  time.Hour,
			wantDelete: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			require.NoError(t, deleteAssetFiles(context.Background(), tempDir, tt.t, tt.threshold))

			if tt.wantDelete {
				for _, n := range files {
					fileName := filepath.Join(tempDir, n)
					_, err := os.Stat(fileName)
					require.ErrorIs(t, err, os.ErrNotExist)
				}
			} else {
				for _, n := range files {
					fileName := filepath.Join(tempDir, n)
					_, err := os.Stat(fileName)
					require.NoError(t, err)
				}
			}
		})
	}
}
