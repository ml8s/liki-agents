package agent

import "google.golang.org/adk/v2/model"

// modelResolver constructs provider model clients lazily and memoizes them by
// model name so each distinct per-Agent model is built once per runtime.
type modelResolver struct {
	build  func(name string) (model.LLM, error)
	models map[string]model.LLM
}

func newModelResolver(build func(string) (model.LLM, error)) *modelResolver {
	return &modelResolver{build: build, models: make(map[string]model.LLM)}
}

func (r *modelResolver) resolve(name string) (model.LLM, error) {
	if cached, ok := r.models[name]; ok {
		return cached, nil
	}
	built, err := r.build(name)
	if err != nil {
		return nil, err
	}
	r.models[name] = built
	return built, nil
}
