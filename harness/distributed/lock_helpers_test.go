package distributed

import (
	"testing"
)

func TestSplitPath(t *testing.T) {
	cases := []struct {
		full   string
		prefix string
		want   []string
	}{
		{"/locks/res1", "/locks/", []string{"res1"}},
		{"/locks/res1/tok123", "/locks/", []string{"res1", "tok123"}},
		{"/locks/", "/locks/", nil},
		{"/locks//tok", "/locks/", []string{"tok"}},
	}
	for _, tc := range cases {
		got := splitPath(tc.full, tc.prefix)
		if len(got) != len(tc.want) {
			t.Errorf("splitPath(%q, %q) = %v, want %v", tc.full, tc.prefix, got, tc.want)
			continue
		}
		for i, v := range tc.want {
			if got[i] != v {
				t.Errorf("splitPath[%d] = %q, want %q", i, got[i], v)
			}
		}
	}
}

func TestSplitN(t *testing.T) {
	cases := []struct {
		s    string
		sep  string
		want []string
	}{
		{"a/b/c", "/", []string{"a", "b", "c"}},
		{"abc", "/", []string{"abc"}},
		{"", "/", []string{""}},
		{"a//b", "/", []string{"a", "", "b"}},
		{"hello world test", " ", []string{"hello", "world", "test"}},
	}
	for _, tc := range cases {
		got := splitN(tc.s, tc.sep)
		if len(got) != len(tc.want) {
			t.Errorf("splitN(%q, %q) = %v, want %v", tc.s, tc.sep, got, tc.want)
			continue
		}
		for i, v := range tc.want {
			if got[i] != v {
				t.Errorf("splitN[%d] = %q, want %q", i, got[i], v)
			}
		}
	}
}

func TestIndexOf(t *testing.T) {
	cases := []struct {
		s    string
		sub  string
		want int
	}{
		{"hello/world", "/", 5},
		{"hello", "/", -1},
		{"", "/", -1},
		{"//", "/", 0},
		{"abc", "abc", 0},
		{"abcabc", "bc", 1},
	}
	for _, tc := range cases {
		got := indexOf(tc.s, tc.sub)
		if got != tc.want {
			t.Errorf("indexOf(%q, %q) = %d, want %d", tc.s, tc.sub, got, tc.want)
		}
	}
}
