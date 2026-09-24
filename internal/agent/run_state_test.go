package agent

import "testing"

func TestOutputTextFollowsRFC6901(t *testing.T) {
	document := map[string]any{
		"answer": "plain",
		"a/b":    "escaped slash",
		"c~d":    "escaped tilde",
		"items":  []any{"first", "second"},
		"nested": map[string]any{"answer": "nested"},
	}
	cases := []struct {
		pointer string
		want    string
	}{
		{pointer: "/answer", want: "plain"},
		{pointer: "/nested/answer", want: "nested"},
		{pointer: "/a~1b", want: "escaped slash"},
		{pointer: "/c~0d", want: "escaped tilde"},
		{pointer: "/items/1", want: "second"},
	}
	for _, testCase := range cases {
		t.Run(testCase.pointer, func(t *testing.T) {
			got, err := OutputText(document, testCase.pointer)
			if err != nil {
				t.Fatalf("OutputText() error = %v", err)
			}
			if got != testCase.want {
				t.Fatalf("OutputText() = %q, want %q", got, testCase.want)
			}
		})
	}
}
