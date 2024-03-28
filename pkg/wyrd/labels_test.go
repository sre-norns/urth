package wyrd_test

import (
	"fmt"
	"testing"

	"github.com/sre-norns/urth/pkg/wyrd"
	"github.com/stretchr/testify/require"
)

func TestLabesInterface(t *testing.T) {
	require.Equal(t, "", wyrd.Labels{}.Get("key"))
	require.Equal(t, "value", wyrd.Labels{"key": "value"}.Get("key"))

	require.Equal(t, false, wyrd.Labels{"key": "value"}.Has("key-2"))
	require.Equal(t, false, wyrd.Labels{}.Has("key"))
	require.Equal(t, true, wyrd.Labels{"key": "value"}.Has("key"))
}

func TestLabels_Merging(t *testing.T) {
	testCases := map[string]struct {
		given  []wyrd.Labels
		expect wyrd.Labels
	}{
		"nil": {
			given:  []wyrd.Labels{},
			expect: wyrd.Labels{},
		},
		"identity": {
			given: []wyrd.Labels{
				{"key": "value"},
			},
			expect: wyrd.Labels{"key": "value"},
		},
		"two": {
			given: []wyrd.Labels{
				{"key-1": "value-1"},
				{"key-2": "value-2"},
			},
			expect: wyrd.Labels{
				"key-1": "value-1",
				"key-2": "value-2",
			},
		},
		"key-override": {
			given: []wyrd.Labels{
				{"key-1": "value-1", "key-2": "value-2"},
				{"key-2": "value-Wooh"},
			},
			expect: wyrd.Labels{
				"key-1": "value-1",
				"key-2": "value-Wooh",
			},
		},
		"mixed-bag": {
			given: []wyrd.Labels{
				{"key-1": "value-1", "key-2": "value-2"},
				{"key-2": "value-Wooh", "key-3": "value-3"},
				{"key-2": "value-Naah", "key-4": "value-3"},
			},
			expect: wyrd.Labels{
				"key-1": "value-1",
				"key-2": "value-Naah",
				"key-3": "value-3",
				"key-4": "value-3",
			},
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(fmt.Sprintf("merging:%s", name), func(t *testing.T) {
			got := wyrd.MergeLabels(test.given...)
			require.EqualValues(t, test.expect, got)
		})
	}
}

func TestLabelSelector_AsLabels(t *testing.T) {
	testCases := map[string]struct {
		given       wyrd.LabelSelector
		expect      string
		expectError bool
	}{
		"empty": {
			given:  wyrd.LabelSelector{},
			expect: "",
		},
		"labels-only-1": {
			given: wyrd.LabelSelector{
				MatchLabels: wyrd.Labels{
					"key": "value",
				},
			},
			expect: "key=value",
		},
		"labels-only-2": {
			given: wyrd.LabelSelector{
				MatchLabels: wyrd.Labels{
					"environment": "production",
					"tier":        "frontend",
				},
			},
			expect: "environment=production,tier=frontend",
		},
		"key-exist": {
			given: wyrd.LabelSelector{
				MatchSelector: []wyrd.Selector{
					{Key: "key", Op: wyrd.LabelSelectorOpExists, Values: []string{"bogus"}},
				},
			},
			expect: "key",
		},

		"keys-multy": {
			given: wyrd.LabelSelector{
				MatchSelector: []wyrd.Selector{
					{Key: "key", Op: wyrd.LabelSelectorOpExists, Values: []string{"bogus"}},
					{Key: "tier", Op: wyrd.LabelSelectorOpNotIn, Values: []string{"frontend", "backend"}},
					{Key: "role", Op: wyrd.LabelSelectorOpDoesNotExist},
				},
			},
			expect: "key,tier notin (frontend, backend),!role",
		},

		"keys-mix": {
			given: wyrd.LabelSelector{
				MatchLabels: wyrd.Labels{
					"key":       "value",
					"other_key": "xyz",
				},
				MatchSelector: []wyrd.Selector{
					{Key: "key", Op: wyrd.LabelSelectorOpExists, Values: []string{"bogus"}},
					{Key: "tier", Op: wyrd.LabelSelectorOpNotIn, Values: []string{"frontend", "backend"}},
				},
			},
			expect: "key=value,other_key=xyz,key,tier notin (frontend, backend)",
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			got, err := test.given.AsLabels()
			if test.expectError {
				require.Error(t, err, "expected error: %v", test.expectError)
			} else {
				require.NoError(t, err, "expected error: %v", test.expectError)
				require.Equal(t, test.expect, got)
			}
		})
	}
}
