package agent

import "testing"

func TestDefaultStructuredOutputMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		provider string
		want     string
	}{
		{provider: "openai", want: StructuredOutputJSONSchema},
		{provider: "zhipu", want: StructuredOutputJSONObject},
		{provider: "bigmodel", want: StructuredOutputJSONObject},
		{provider: "glm", want: StructuredOutputJSONObject},
		{provider: "", want: StructuredOutputJSONObject},
	}
	for _, test := range tests {
		if got := DefaultStructuredOutputMode(test.provider); got != test.want {
			t.Errorf("DefaultStructuredOutputMode(%q) = %q, want %q", test.provider, got, test.want)
		}
	}
}
