package slices

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIndexPtrFunc(t *testing.T) {
	tests := []struct {
		desc string
		s    []int
		t    int
		want int
	}{
		{
			desc: "Empty array",
			want: -1,
		},
		{
			desc: "Contains",
			s:    []int{1, 2, 5, 3, 4},
			t:    3,
			want: 3,
		},
		{
			desc: "Duplicates",
			s:    []int{1, 3, 5, 3, 4},
			t:    3,
			want: 1,
		},
		{
			desc: "Not contain",
			s:    []int{1, 2, 5, 3, 4},
			t:    -1,
			want: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			assert.Equal(t, tt.want, IndexPtrFunc(tt.s, func(n *int) bool { return *n == tt.t }))
		})
	}
}

func TestContainsPtrFunc(t *testing.T) {
	tests := []struct {
		desc string
		s    []int
		t    int
		want bool
	}{
		{
			desc: "Empty array",
		},
		{
			desc: "Contains",
			s:    []int{1, 2, 5, 3, 4},
			t:    3,
			want: true,
		},
		{
			desc: "Not contain",
			s:    []int{1, 2, 5, 3, 4},
			t:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			assert.Equal(t, tt.want, ContainsPtrFunc(tt.s, func(n *int) bool { return *n == tt.t }))
		})
	}
}

func TestEqualPtrFunc(t *testing.T) {
	int64Equal := func(a *int, b *int64) bool {
		return int64(*a) == *b
	}

	tests := []struct {
		desc string
		a    []int
		b    []int64
		pred func(*int, *int64) bool
		want bool
	}{
		{
			desc: "Both nil",
			pred: int64Equal,
			want: true,
		},
		{
			desc: "Empty slice and nil",
			pred: int64Equal,
			a:    make([]int, 0),
			want: true,
		},
		{
			desc: "Equal int values",
			pred: int64Equal,
			a:    []int{1, 2, 3, 4},
			b:    []int64{1, 2, 3, 4},
			want: true,
		},
		{
			desc: "Not equal int values",
			pred: int64Equal,
			a:    []int{1, 2, 3, 4},
			b:    []int64{1, 2, 4, 4},
		},
		{
			desc: "The size of slices do not match; a is larger than b",
			pred: int64Equal,
			a:    []int{1, 2, 3, 4},
			b:    []int64{1, 2, 3},
		},
		{
			desc: "The size of slices do not match; b is larger than a",
			pred: int64Equal,
			a:    []int{1, 2, 3},
			b:    []int64{1, 2, 3, 4},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			assert.Equal(t, tt.want, EqualPtrFunc(tt.a, tt.b, tt.pred))
		})
	}
}

func TestTransform(t *testing.T) {
	tests := []struct {
		desc string
		src  []int
		want []string
	}{
		{
			desc: "empty src",
		},
		{
			desc: "non-empty src",
			src:  []int{0, 1, 2, 3, 4, 999},
			want: []string{"0", "1", "2", "3", "4", "999"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			assert.Equal(t, tt.want, Transform(tt.src, func(a int) string {
				return strconv.Itoa(a)
			}))
		})
	}
}

func TestTransformTo(t *testing.T) {
	tests := []struct {
		desc string
		src  []int
		dst  []string
		want []string
	}{
		{
			desc: "empty src",
		},
		{
			desc: "non-empty src",
			src:  []int{0, 1, 2, 3, 4, 999},
			want: []string{"0", "1", "2", "3", "4", "999"},
		},
		{
			desc: "non-empty dst",
			src:  []int{0, 1, 2},
			dst:  []string{"a", "b"},
			want: []string{"a", "b", "0", "1", "2"},
		},
		{
			desc: "allocated dst",
			src:  []int{0, 1, 2},
			dst:  make([]string, 0, 3),
			want: []string{"0", "1", "2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			assert.Equal(t, tt.want, TransformTo(tt.dst, tt.src, func(a int) string {
				return strconv.Itoa(a)
			}))
		})
	}
}
