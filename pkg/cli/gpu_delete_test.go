package cli

import (
	"reflect"
	"testing"
)

func TestFilterSimsClusters(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "empty", in: nil, want: []string{}},
		{name: "no sims", in: []string{"kind", "other"}, want: []string{}},
		{name: "only sims", in: []string{"sims-nvidia", "sims-amd"}, want: []string{"sims-nvidia", "sims-amd"}},
		{name: "mixed", in: []string{"sims-nvidia", "kind", "sims-amd", "user-test"}, want: []string{"sims-nvidia", "sims-amd"}},
		{name: "prefix sims- preserved order", in: []string{"sims-z", "sims-a"}, want: []string{"sims-z", "sims-a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := filterSimsClusters(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
