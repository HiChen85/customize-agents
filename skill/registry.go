package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type SkillSource int

const (
	SourceProject SkillSource = iota
	SourceUser
)

type SkillIndex struct {
	Name        string
	Description string
	Trigger     string
	Source      SkillSource
	Dir         string
}

type SkillRegistry struct {
	index      []*SkillIndex
	loaded     map[string]*Skill
	mu         sync.RWMutex
	projectDir string
	userDir    string
}

func NewSkillRegistry(projectDir, userDir string) *SkillRegistry {
	return &SkillRegistry{
		loaded:     make(map[string]*Skill),
		projectDir: projectDir,
		userDir:    userDir,
	}
}

func (r *SkillRegistry) BuildIndex() error {
	var entries []*SkillIndex

	projectEntries := r.scanDir(r.projectDir, SourceProject)
	entries = append(entries, projectEntries...)

	userEntries := r.scanDir(r.userDir, SourceUser)

	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		seen[strings.ToLower(e.Name)] = true
	}
	for _, e := range userEntries {
		if !seen[strings.ToLower(e.Name)] {
			entries = append(entries, e)
			seen[strings.ToLower(e.Name)] = true
		}
	}

	r.mu.Lock()
	r.index = entries
	r.mu.Unlock()

	return nil
}

func (r *SkillRegistry) scanDir(dir string, source SkillSource) []*SkillIndex {
	if dir == "" {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var result []*SkillIndex
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillPath := filepath.Join(dir, entry.Name(), "SKILL.md")
		data, err := os.ReadFile(skillPath)
		if err != nil {
			continue
		}
		fm, _, err := ParseFrontmatter(data)
		if err != nil {
			continue
		}
		result = append(result, &SkillIndex{
			Name:        fm.Name,
			Description: fm.Description,
			Trigger:     fm.Trigger,
			Source:      source,
			Dir:         filepath.Join(dir, entry.Name()),
		})
	}
	return result
}

func (r *SkillRegistry) GetIndex() []*SkillIndex {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*SkillIndex, len(r.index))
	copy(result, r.index)
	return result
}

func (r *SkillRegistry) Activate(name string) (*Skill, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if s, ok := r.loaded[strings.ToLower(name)]; ok {
		return s, nil
	}

	var target *SkillIndex
	for _, idx := range r.index {
		if strings.EqualFold(idx.Name, name) {
			target = idx
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("skill not found: %s", name)
	}

	s, err := LoadSkill(target.Dir)
	if err != nil {
		return nil, fmt.Errorf("load skill %s: %w", name, err)
	}

	r.loaded[strings.ToLower(name)] = s
	return s, nil
}

func (r *SkillRegistry) IsActive(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.loaded[strings.ToLower(name)]
	return ok
}

func (r *SkillRegistry) ActiveSkills() []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*Skill, 0, len(r.loaded))
	for _, s := range r.loaded {
		result = append(result, s)
	}
	return result
}

func (r *SkillRegistry) BuildIndexPrompt() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.index) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n## Available Skills\n\n")
	sb.WriteString("Use the activate_skill tool to load a skill when relevant to your current task.\n\n")
	sb.WriteString("| Skill | Description | When to use |\n")
	sb.WriteString("|-------|-------------|-------------|\n")

	for _, idx := range r.index {
		trigger := idx.Trigger
		if trigger == "" {
			trigger = idx.Description
		}
		fmt.Fprintf(&sb, "| %s | %s | %s |\n", idx.Name, idx.Description, trigger)
	}

	return sb.String()
}
