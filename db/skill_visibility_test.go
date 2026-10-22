package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Autumn-27/norma/skill"
)

// 内置 skill 必须能被 norma 的 loader 正确加载。
//
// 为什么要测这个：skill 的 frontmatter 是 YAML，格式写错了（描述里有换行、
// 冒号没引号）会**静默**解析失败——norma 拿不到 name/description，于是
// 这个 skill 既不显示、模型也永远看不到它。而这类问题在页面上完全看不出来
// （页面的 skill 列表是 DB 侧的可见性绑定，跟文件解析是两回事）。
//
// 所以这里用 norma 真实的 LoadDir 加载仓库里的 skills/ 目录，逐个断言
// name/description 都拿到了。
func TestShippedSkillsLoadWithNormaLoader(t *testing.T) {
	skillDir := shippedSkillDir(t)
	reg, err := skill.LoadDir(skillDir)
	if err != nil {
		t.Fatalf("加载 skill 目录失败: %v", err)
	}
	all := reg.List()
	if len(all) == 0 {
		t.Fatal("一个 skill 都没加载到")
	}
	for _, s := range all {
		if strings.TrimSpace(s.Name) == "" {
			t.Errorf("skill 缺少 name（frontmatter 解析失败）")
		}
		if strings.TrimSpace(s.Description) == "" {
			t.Errorf("skill %q 缺少 description——模型靠它判断要不要调用", s.Name)
		}
	}
}

// vuln-hunting 必须能被加载，且描述里说清「什么时候用」——
// 描述是模型决定「要不要点开这个 skill」的唯一依据。
func TestVulnHuntingSkillIsLoadable(t *testing.T) {
	skillDir := shippedSkillDir(t)
	reg, err := skill.LoadDir(skillDir)
	if err != nil {
		t.Fatalf("加载 skill 目录失败: %v", err)
	}
	var found *skill.Skill
	for _, s := range reg.List() {
		if s.Name == "vuln-hunting" {
			cp := s
			found = &cp
			break
		}
	}
	if found == nil {
		t.Fatal("vuln-hunting skill 未被加载——frontmatter 解析失败或目录名不对")
	}
	// 描述必须包含「什么时候用」的触发线索，否则模型不会想到用它。
	for _, want := range []string{"挖掘", "漏洞"} {
		if !strings.Contains(found.Description, want) {
			t.Errorf("vuln-hunting 的描述缺少 %q: %s", want, found.Description)
		}
	}
}

// vuln-hunting 的 reference.md 必须在 skill 目录里——SKILL.md 里引用了它，
// 缺了会让 agent 按需展开时找不到内容。
func TestVulnHuntingHasReferenceFile(t *testing.T) {
	skillDir := shippedSkillDir(t)
	p := filepath.Join(skillDir, "vuln-hunting", "reference.md")
	if _, err := os.Stat(p); err != nil {
		t.Errorf("vuln-hunting/reference.md 缺失: %v", err)
	}
}

// vuln-hunting 必须绑定给 planner 与 worker——
// planner 决定派不派「挖未知漏洞」的意图，worker 执行时需要方法论。
func TestVulnHuntingVisibilityBound(t *testing.T) {
	agents, ok := builtinSkillVisibility["vuln-hunting"]
	if !ok {
		t.Fatal("vuln-hunting 没有可见性绑定——谁都用不到它")
	}
	has := func(k string) bool {
		for _, a := range agents {
			if a == k {
				return true
			}
		}
		return false
	}
	if !has("planner") {
		t.Error("vuln-hunting 未绑给 planner——它没法据此派挖掘类意图")
	}
	if !has("worker") {
		t.Error("vuln-hunting 未绑给 worker——它执行挖掘意图时看不到方法论")
	}
}

// shippedSkillDir 定位仓库里的 skills/ 目录。
//
// 测试在 db 包里跑，而 skills/ 在仓库根——用相对路径往上找。
func shippedSkillDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		cand := filepath.Join(dir, "skills")
		if st, err := os.Stat(cand); err == nil && st.IsDir() {
			return cand
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("找不到仓库的 skills/ 目录")
	return ""
}
