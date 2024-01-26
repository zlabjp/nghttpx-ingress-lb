package nghttpx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDeleteAssetFiles(t *testing.T) {
	tempDir := t.TempDir()

	modTime := time.Now()

	files := []string{"alpha", "bravo", "charlie"}
	for _, n := range files {
		if err := os.WriteFile(filepath.Join(tempDir, n), []byte(n), 0600); err != nil {
			t.Fatalf("Unable to write file: %v", err)
		}
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
			if err := deleteAssetFiles(context.Background(), tempDir, tt.t, tt.threshold); err != nil {
				t.Fatalf("deleteAssetFiles: %v", err)
			}

			if tt.wantDelete {
				for _, n := range files {
					fileName := filepath.Join(tempDir, n)
					if _, err := os.Stat(fileName); !errors.Is(err, os.ErrNotExist) {
						t.Errorf("File %v must be deleted", fileName)
					}
				}
			} else {
				for _, n := range files {
					fileName := filepath.Join(tempDir, n)
					if _, err := os.Stat(fileName); err != nil {
						t.Errorf("os.Stat(%q): %v", fileName, err)
					}
				}
			}
		})
	}
}
