package agent

import (
	"testing"

	"google.golang.org/adk/v2/model"
)

func TestModelResolverCachesPerModelName(t *testing.T) {
	t.Parallel()
	calls := 0
	resolver := newModelResolver(func(string) (model.LLM, error) {
		calls++
		return &fakeLLM{}, nil
	})
	first, err := resolver.resolve("glm-5.3-flash")
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolver.resolve("glm-5.3-flash")
	if err != nil {
		t.Fatal(err)
	}
	if first != second || calls != 1 {
		t.Fatalf("same name: same-instance=%v builds=%d, want one cached build", first == second, calls)
	}
	third, err := resolver.resolve("gpt-4.1")
	if err != nil {
		t.Fatal(err)
	}
	if third == first || calls != 2 {
		t.Fatalf("distinct name: same-instance=%v builds=%d, want a distinct build", third == first, calls)
	}
}
