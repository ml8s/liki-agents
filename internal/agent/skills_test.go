package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidateSkillToolsetAllow(t *testing.T) {
	cases := []struct {
		name    string
		tools   []string
		wantErr string
	}{
		{"exact set", []string{"list_skills", "load_skill", "load_skill_resource"}, ""},
		{"missing one", []string{"list_skills", "load_skill"}, "missing builtin tool"},
		{"empty", nil, "missing builtin tool"},
		{"unknown extra", []string{"list_skills", "load_skill", "load_skill_resource", "extra"}, "unknown builtin tool"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSkillToolsetAllow(tc.tools)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func minimalSkillAgent() *AgentDefinition {
	return &AgentDefinition{
		Name:        "main",
		Version:     "1.0.0",
		Description: "test agent",
		Mode:        AgentModeChat,
		Instruction: FileReference{Path: "instruction.md"},
	}
}

func TestAgentSkillsBindingValidation(t *testing.T) {
	fullAllow := map[string][]string{
		"skilltoolset": {"list_skills", "load_skill", "load_skill_resource"},
	}
	cases := []struct {
		name    string
		mutate  func(*AgentDefinition)
		wantErr string
	}{
		{
			"valid frontmatter default",
			func(a *AgentDefinition) {
				a.Skills = &SkillsBinding{Root: "/skills"}
				a.Tools.Allow = fullAllow
			},
			"",
		},
		{
			"valid explicit complete preload",
			func(a *AgentDefinition) {
				a.Skills = &SkillsBinding{Root: "/skills", Preload: "complete"}
				a.Tools.Allow = fullAllow
			},
			"",
		},
		{
			"empty root",
			func(a *AgentDefinition) {
				a.Skills = &SkillsBinding{Root: "  "}
				a.Tools.Allow = fullAllow
			},
			"skills.root is required",
		},
		{
			"relative root",
			func(a *AgentDefinition) {
				a.Skills = &SkillsBinding{Root: "skills"}
				a.Tools.Allow = fullAllow
			},
			"must be an absolute path",
		},
		{
			"invalid preload",
			func(a *AgentDefinition) {
				a.Skills = &SkillsBinding{Root: "/skills", Preload: "lazy"}
				a.Tools.Allow = fullAllow
			},
			"must be one of frontmatter, complete",
		},
		{
			"skills without allow triple",
			func(a *AgentDefinition) {
				a.Skills = &SkillsBinding{Root: "/skills"}
				a.Tools.Allow = map[string][]string{"test": {"test_tool"}}
			},
			"skills binding requires tools.allow[skilltoolset]",
		},
		{
			"allow triple without skills",
			func(a *AgentDefinition) {
				a.Tools.Allow = fullAllow
			},
			"tools.allow[skilltoolset] requires a skills binding",
		},
		{
			"partial allow triple",
			func(a *AgentDefinition) {
				a.Skills = &SkillsBinding{Root: "/skills"}
				a.Tools.Allow = map[string][]string{"skilltoolset": {"list_skills"}}
			},
			"missing builtin tool",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def := minimalSkillAgent()
			tc.mutate(def)
			err := def.validate(map[string]struct{}{})
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestNewSkillToolset(t *testing.T) {
	t.Run("valid tree frontmatter preload", func(t *testing.T) {
		root := t.TempDir()
		writeMinimalSkillTree(t, root)
		ts, err := newSkillToolset(&SkillsBinding{Root: root})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if ts == nil {
			t.Fatal("toolset is nil")
		}
	})
	t.Run("valid tree complete preload", func(t *testing.T) {
		root := t.TempDir()
		writeMinimalSkillTree(t, root)
		ts, err := newSkillToolset(&SkillsBinding{Root: root, Preload: "complete"})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if ts == nil {
			t.Fatal("toolset is nil")
		}
	})
	t.Run("missing root fails fast", func(t *testing.T) {
		_, err := newSkillToolset(&SkillsBinding{Root: filepath.Join(t.TempDir(), "absent")})
		if err == nil || !strings.Contains(err.Error(), "preload skills from") {
			t.Fatalf("error = %v, want preload failure", err)
		}
	})
	t.Run("frontmatter name mismatch fails fast", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "dirname")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		skill := "---\nname: othername\ndescription: Mismatched name triggers ADK frontmatter preload failure.\n---\n\n# x\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skill), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := newSkillToolset(&SkillsBinding{Root: root})
		if err == nil || !strings.Contains(err.Error(), "preload skills from") {
			t.Fatalf("error = %v, want preload failure", err)
		}
	})
	t.Run("nil binding rejected", func(t *testing.T) {
		if _, err := newSkillToolset(nil); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestSkillBindingOverride(t *testing.T) {
	definition := minimalSkillAgent()
	definition.Skills = &SkillsBinding{Root: "/skills", Preload: "complete"}

	binding := skillBinding(Config{}, definition)
	if binding == nil || binding.Root != "/skills" || binding.Preload != "complete" {
		t.Fatalf("binding without override = %+v, want root /skills preload complete", binding)
	}
	binding = skillBinding(Config{SkillsRoot: "/fixture/skills"}, definition)
	if binding == nil || binding.Root != "/fixture/skills" || binding.Preload != "complete" {
		t.Fatalf("binding with override = %+v, want root /fixture/skills preload complete", binding)
	}
	if definition.Skills.Root != "/skills" {
		t.Fatalf("deployment definition mutated: root = %q", definition.Skills.Root)
	}
	if got := skillBinding(Config{SkillsRoot: "/fixture/skills"}, minimalSkillAgent()); got != nil {
		t.Fatalf("binding without skills = %+v, want nil", got)
	}
}

// TestDevDeploymentSkillsBootsWithOverride boots the canonical dev fixture
// the same way scripts/dev.sh and the dev compose stack do: the deployment
// pins the container path /skills and the development-only override rebinds
// it to the fixture that actually ships in the repository.
func TestDevDeploymentSkillsBootsWithOverride(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	devRoot := filepath.Join(filepath.Dir(file), "..", "..", "dev", "agent-deployment")
	deployment, err := LoadAgentDeployment(filepath.Join(devRoot, "deployment.json"))
	if err != nil {
		t.Fatalf("load dev deployment: %v", err)
	}
	definition, ok := deployment.Agent("main")
	if !ok {
		t.Fatal("dev deployment has no agent main")
	}
	if definition.Skills == nil || definition.Skills.Root != "/skills" {
		t.Fatalf("dev fixture skills = %+v, want root /skills", definition.Skills)
	}
	if _, err := os.Stat(filepath.Join(devRoot, "skills", "liki", "SKILL.md")); err != nil {
		t.Fatalf("dev skills fixture: %v", err)
	}
	if _, statErr := os.Stat("/skills"); statErr != nil {
		toolset, err := newSkillToolset(skillBinding(Config{}, definition))
		if err == nil {
			t.Fatalf("toolset without override = %v, want fail-fast on missing /skills", toolset)
		}
	}
	toolset, err := newSkillToolset(skillBinding(Config{SkillsRoot: filepath.Join(devRoot, "skills")}, definition))
	if err != nil {
		t.Fatalf("newSkillToolset with override: %v", err)
	}
	if toolset == nil {
		t.Fatal("newSkillToolset with override = nil")
	}
}
