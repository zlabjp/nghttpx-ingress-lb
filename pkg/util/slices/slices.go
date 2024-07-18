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
