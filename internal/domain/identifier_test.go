package domain

import "testing"

func TestValidIdentifier(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "empty", value: "", want: false},
		{name: "simple", value: "run_1", want: true},
		{name: "non-ascii printable", value: "用户-123", want: true},
		{name: "control character", value: "run\n1", want: false},
		{name: "over limit", value: repeatPrintable(MaxIdentifierLength + 1), want: false},
		{name: "at limit", value: repeatPrintable(MaxIdentifierLength), want: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := ValidIdentifier(testCase.value); got != testCase.want {
				t.Fatalf("ValidIdentifier(%q) = %v, want %v", testCase.value, got, testCase.want)
			}
		})
	}
}

func repeatPrintable(n int) string {
	buffer := make([]byte, n)
	for index := range buffer {
		buffer[index] = 'a'
	}
	return string(buffer)
}
