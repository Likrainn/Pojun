package db

import (
	"regexp"
	"testing"
)

// 内置规则表与真实分流行为的一致性。
//
// 上一份测试（intercept_builtin_rules_test.go）断言的是「分类字段对不对」，
// 这一份断言的是「按分类分组后，每条规则实际会走到哪条路径」——把分类从
// 声明变成可验证的行为。
//
// 判别方式：明令禁止类命中 → 不需要模型就能判定（action=deny 直接拦）；
// 研判类命中 → action=judge，交给模型。

// 明令禁止类规则命中时，规则本身的 action 必须是 deny——模型在这条路径上
// 永远不会被调用。
func TestHardDenyRulesResolveWithoutModel(t *testing.T) {
	for _, r := range builtinInterceptRules {
		if r.class != classHardDeny {
			continue
		}
		if r.action != "deny" {
			t.Errorf("明令禁止类规则 %q 的 action=%q，模型路径不会被走到（应为 deny）", r.name, r.action)
		}
	}
}

// 研判类规则命中时，规则本身的 action 必须是 judge——模型会被调用。
func TestJudgeRulesRouteToModel(t *testing.T) {
	for _, r := range builtinInterceptRules {
		if r.class != classJudge {
			continue
		}
		if r.action != "judge" {
			t.Errorf("研判类规则 %q 的 action=%q，不会走到模型（应为 judge）", r.name, r.action)
		}
	}
}

// 人工确认类规则命中时走 ask——既不是模型裁决也不是直接拒绝。
func TestAskRulesRouteToHuman(t *testing.T) {
	for _, r := range builtinInterceptRules {
		if r.class != classAsk {
			continue
		}
		if r.action != "ask" {
			t.Errorf("人工确认类规则 %q 的 action=%q（应为 ask）", r.name, r.action)
		}
	}
}

// 关键行为断言：同一条命令，命中「明令禁止」类规则时被拒；命中「研判」类
// 规则时走模型——这正是用户要的「哪些明令禁止、哪些交给 Agent 研判」。
func TestSameCommandDifferentRulesDifferentPaths(t *testing.T) {
	byName := builtinRulesByName()

	// rm -rf 同时出现在两条规则里：一条硬禁（清空系统关键目录）、一条研判
	// （递归强制删除 rm -rf）。按 priority DESC、id ASC 的匹配顺序，先命中的
	// 决定走向——这里验证两条规则的分类确实不同，也就是**走向不同**。
	hard := byName["[内置] 清空系统关键目录"]
	judge := byName["[内置] 递归强制删除 rm -rf"]
	if hard.class != classHardDeny || judge.class != classJudge {
		t.Fatalf("两条 rm 规则的分类不对：清空=%s 递归=%s", hard.class, judge.class)
	}
	if hard.action != "deny" || judge.action != "judge" {
		t.Fatalf("两条 rm 规则的动作不对：清空=%q 递归=%q", hard.action, judge.action)
	}

	// 两条规则都能匹配 rm -rf /etc/* —— 走向由 priority 决定。
	re := regexp.MustCompile(hard.pattern)
	if !re.MatchString(`rm -rf /etc/*`) {
		t.Error("硬禁规则应匹配 rm -rf /etc/*")
	}
	re2 := regexp.MustCompile(judge.pattern)
	if !re2.MatchString(`rm -rf /etc/*`) {
		t.Error("研判规则应匹配 rm -rf /etc/*")
	}
	// 硬禁那条 priority 更高（100），所以在真实库里先命中 → 直接拒绝。
	if hard.priority < judge.priority {
		t.Errorf("硬禁规则的 priority(%d)必须不低于研判规则(%d)，否则走向不确定",
			hard.priority, judge.priority)
	}
}

// 内置规则表必须覆盖硬禁与研判两类，且每类都非空——
// 缺一类就说明「明令禁止 vs 交 Agent 研判」有一边是空的。
// classAsk 不在断言里：内置 ask 规则只有过「启停回连接收器」一条，其目标工具
// 从未实现（2026-10-29 随 listener stub 一起移除）；ask 动作本身保留给用户
// 自建规则与模型裁决的 fail_action 使用。
func TestBuiltinRuleClassesAreAllPopulated(t *testing.T) {
	counts := map[string]int{}
	for _, r := range builtinInterceptRules {
		counts[r.class]++
	}
	for _, class := range []string{classHardDeny, classJudge} {
		if counts[class] == 0 {
			t.Errorf("分类 %q 没有任何内置规则——「明令禁止/交 Agent 研判」有一边是空的", class)
		}
	}
}
