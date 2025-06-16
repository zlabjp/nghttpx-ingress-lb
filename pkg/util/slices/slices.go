package slices

// IndexPtrFunc behaves like slices.IndexFunc in the standard library, but f takes pointer to an element to save extra copying.
func IndexPtrFunc[S ~[]E, E any](s S, f func(*E) bool) int {
	for i := range s {
		if f(&s[i]) {
			return i
		}
	}

	return -1
}

// ContainsPtrFunc behaves like slices.ContainsFunc in the standard library, but f takes pointer to an element to save extra copying.
func ContainsPtrFunc[S ~[]E, E any](s S, f func(*E) bool) bool {
	return IndexPtrFunc(s, f) >= 0
}

// EqualPtrFunc behaves like slices.EqualFunc in the standard library, but f takes pointers to elements to save extra copying.
func EqualPtrFunc[S1 ~[]E1, S2 ~[]E2, E1, E2 any](s1 S1, s2 S2, eq func(*E1, *E2) bool) bool {
	if len(s1) != len(s2) {
		return false
	}

	for i := range s1 {
		if !eq(&s1[i], &s2[i]) {
			return false
		}
	}

	return true
}

// Transform applies f to each element in s, and returns a slice of E2 in order.  If s is empty, this function returns nil.
func Transform[S ~[]E1, E1, E2 any](s S, f func(E1) E2) []E2 {
	if len(s) == 0 {
		return nil
	}

	dst := make([]E2, len(s))

	for i := range s {
		dst[i] = f(s[i])
	}

	return dst
}

// TransformTo applies f to each element in s, appends it to the end of dst, and returns the updated slice.
func TransformTo[S ~[]E1, E1, E2 any](dst []E2, s S, f func(E1) E2) []E2 {
	if len(s) == 0 {
		return dst
	}

	for i := range s {
		dst = append(dst, f(s[i]))
	}

	return dst
}
