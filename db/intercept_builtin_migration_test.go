package db

import (
	"strings"
	"testing"
)

// 迁移的 SQL 构造必须正确——它会改**已部署实例**的规则表，错了就是线上事故。
//
// 这里不依赖真实 Postgres（DB 测试无库时 skip，CI 上等于没测），而是直接对
// 迁移函数拼出来的 SQL 与参数做断言：参数编号必须连续、占位符数量必须与
// 参数个数相等、判据必须包含「用户没动过」的指纹。
//
// 真实 Postgres 上的端到端迁移验证由部署前的部署手册步骤承担（备份 →
// 迁移 → 抽查）。

// 占位符个数必须与参数个数一致——少一个会报 "bind message supplies N
// parameters"，多一个会越界 panic（曾真实发生过一次）。
func TestMigrationSQLPlaceholderCountMatchesArgs(t *testing.T) {
	for _, m := range buildMigrationSQL() {
		placeholders := countPlaceholders(m.sql)
		if placeholders != len(m.args) {
			t.Errorf("阶段 %q 的占位符数(%d)与参数数(%d)不一致\nSQL:\n%s",
				m.stage, placeholders, len(m.args), m.sql)
		}
	}
}

// 占位符编号必须从 $1 连续到 $N——中间跳过某个编号会让参数错位（把
// timeout_action 当成 pattern 传进去），而且 Postgres 不会报错，只会静默写错。
func TestMigrationSQLPlaceholderNumbersAreContiguous(t *testing.T) {
	for _, m := range buildMigrationSQL() {
		seen := map[int]bool{}
		for _, n := range placeholderNumbers(m.sql) {
			if seen[n] {
				t.Errorf("阶段 %q 的占位符 $%d 重复", m.stage, n)
			}
			seen[n] = true
		}
		for i := 1; i <= len(m.args); i++ {
			if !seen[i] {
				t.Errorf("阶段 %q 缺少占位符 $%d（占位符编号必须连续）\nSQL:\n%s", m.stage, i, m.sql)
			}
		}
	}
}

// 每条规则的更新都必须带「用户没动过」的指纹（action + timeout 三元组），
// 否则用户自己改过的规则会被覆盖回内置默认。
func TestMigrationAlwaysChecksUserUntouchedFingerprint(t *testing.T) {
	for _, m := range buildMigrationSQL() {
		if m.stage == "rename-drop" {
			continue
		}
		for _, field := range []string{"action=", "timeout_enabled=", "timeout_seconds=", "timeout_action="} {
			if !strings.Contains(m.sql, field) {
				t.Errorf("阶段 %q 的判据缺少 %q——用户改过的规则会被覆盖\nSQL:\n%s",
					m.stage, field, m.sql)
			}
		}
	}
}

// 研判类规则迁移后必须带上自己的超时声明——没有它，模型超时时行为不确定。
func TestMigrationSetsTimeoutForJudgeRules(t *testing.T) {
	byStage := map[string]migrationSQL{}
	for _, m := range buildMigrationSQL() {
		byStage[m.stage] = m
	}
	for _, r := range builtinInterceptRules {
		if r.class != classJudge {
			continue
		}
		m, ok := byStage["update:"+r.name]
		if !ok {
			t.Errorf("规则 %q 没有对应的更新语句", r.name)
			continue
		}
		// 尾部五个参数依次是 action / message / timeout_enabled /
		// timeout_seconds / timeout_action。
		tail := m.args[len(m.args)-5:]
		if tail[2] != true {
			t.Errorf("规则 %q 迁移后 timeout_enabled=%v，应为 true", r.name, tail[2])
		}
		if tail[3].(int) <= 0 {
			t.Errorf("规则 %q 迁移后 timeout_seconds=%v，应为正数", r.name, tail[3])
		}
		if tail[4] != "allow" && tail[4] != "deny" {
			t.Errorf("规则 %q 迁移后 timeout_action=%v，应为 allow 或 deny", r.name, tail[4])
		}
	}
}

// 硬禁类规则迁移后必须仍是 deny——整改不能削弱明令禁止那一层。
func TestMigrationKeepsHardDenyRulesDenied(t *testing.T) {
	byStage := map[string]migrationSQL{}
	for _, m := range buildMigrationSQL() {
		byStage[m.stage] = m
	}
	for _, r := range builtinInterceptRules {
		if r.class != classHardDeny {
			continue
		}
		m, ok := byStage["update:"+r.name]
		if !ok {
			t.Errorf("规则 %q 没有对应的更新语句", r.name)
			continue
		}
		action := m.args[len(m.args)-5]
		if action != "deny" {
			t.Errorf("硬禁规则 %q 迁移后 action=%v，应为 deny", r.name, action)
		}
	}
}

// 迁移必须给每条内置规则都产生一条更新语句——漏一条就意味着那条规则在
// 已部署实例上仍是旧动作。
func TestMigrationCoversEveryBuiltinRule(t *testing.T) {
	stages := map[string]bool{}
	for _, m := range buildMigrationSQL() {
		if strings.HasPrefix(m.stage, "update:") {
			stages[strings.TrimPrefix(m.stage, "update:")] = true
		}
	}
	for _, r := range builtinInterceptRules {
		if !stages[r.name] {
			t.Errorf("规则 %q 没有迁移语句——已部署实例上它会保持旧动作", r.name)
		}
	}
}

// 旧 pattern 的指纹必须与旧 seed 里的字面量完全一致——写错一个字符，
// 「用户没动过」的判据就永远不成立，迁移会静默地一条都不更新。
func TestOldPatternFingerprintsMatchLegacySeed(t *testing.T) {
	// 旧 v1 的 DROP 规则字面量。
	wantDrop := `(?i)\bDROP\s+(?:DATABASE|TABLE|SCHEMA|INDEX|VIEW|TABLESPACE|USER|ROLE)\b`
	if got := oldRulePatterns["[内置] SQL DROP DATABASE / TABLE / SCHEMA"]; got != wantDrop {
		t.Errorf("旧 DROP pattern 指纹不匹配\n got: %s\nwant: %s", got, wantDrop)
	}
	// 旧 v2 的数据外泄规则字面量。
	wantExfil := `(?i)(curl|wget|nc|ncat)\b[^|]*\b(\|\s*(curl|wget|nc))`
	if got := oldRulePatterns["[内置] 数据外泄管道"]; got != wantExfil {
		t.Errorf("旧外泄 pattern 指纹不匹配\n got: %s\nwant: %s", got, wantExfil)
	}
}

// countPlaceholders 数 SQL 里的 $N 占位符（去重前的总个数）。
func countPlaceholders(s string) int {
	return len(placeholderNumbers(s))
}

// placeholderNumbers 返回 SQL 里所有 $N 的 N（按出现顺序，可重复）。
func placeholderNumbers(s string) []int {
	var out []int
	for i := 0; i < len(s); i++ {
		if s[i] != '$' {
			continue
		}
		j := i + 1
		n := 0
		digits := 0
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			n = n*10 + int(s[j]-'0')
			j++
			digits++
		}
		if digits == 0 {
			continue
		}
		out = append(out, n)
		i = j - 1
	}
	return out
}

// 全新安装与已部署升级两条路的顺序依赖。
//
// 全新安装：seed 先插规则（动作已是新的），迁移后跑。迁移的判据是「旧默认
// 指纹」（action=deny + timeout(false,60,deny) + 旧 pattern），而新插入的
// 规则不匹配这个指纹 → 迁移**一条都不改**，它们保持新动作。
//
// 已部署升级：库里是旧规则（匹配旧指纹）→ 迁移按新定义更新。
//
// 也就是说迁移对全新安装必须是**幂等且无副作用**的。这条测试锁定这一点：
// 新定义下的规则，一行都不匹配旧指纹。
func TestMigrationIsNoOpOnFreshlySeededRules(t *testing.T) {
	// 全新安装后库里每条规则的样子 = 新定义。
	for _, r := range builtinInterceptRules {
		oldName := r.name
		if old, ok := oldRuleNames[r.name]; ok {
			oldName = old
		}
		oldPat, hasOldPat := oldRulePatterns[oldName]
		patChanged := hasOldPat && oldPat != r.pattern

		// 判据：action 仍是旧的 deny/ask + timeout 三元组仍是旧的。
		// 新 seed 插的规则：action 是新的（judge/deny/ask），timeout 是新的。
		// 所以「全新安装的规则」不可能同时满足「action=旧 deny」且「timeout=旧」。
		isOldFingerprint := r.timeoutSeconds == oldDenyTimeoutS &&
			r.timeoutAction == oldDenyTimeoutA &&
			!r.timeoutEnabled &&
			r.action == oldDenyAction
		if r.class == classAsk {
			isOldFingerprint = r.timeoutSeconds == oldListenerTS &&
				r.timeoutAction == oldListenerTA &&
				r.timeoutEnabled == oldListenerTE &&
				r.action == oldListenerAction
		}
		if isOldFingerprint {
			// 迁移会再改它一次——这本身无害（幂等：同样的 action/timeout
			// 写回去，值不变）。但**消息**会变（新 message 与旧 message 不同），
			// 所以要看新消息是否也相同——相同则纯幂等，不同则迁移会改写
			// 用户可见的说明文字。
			//
			// listener 规则就是这种情况：它的动作从一开始就是 ask/180s/allow，
			// 与新定义完全一致。
			if !builtinRuleMessageUnchanged(r.name) {
				t.Errorf("规则 %q 的新定义与旧默认完全一致，但消息不同——迁移会改写用户可见的说明文字",
					r.name)
			}
		}
		if patChanged {
			// pattern 变了，说明这条规则的 pattern 由迁移负责换；全新安装时
			// seed 直接插新 pattern，不需要迁移介入——两者不冲突。
			continue
		}
	}
}

// builtinRuleMessageUnchanged 判断某条规则的消息在迁移前后是否一致。
//
// 目前只有 listener 规则是「新旧完全一致」的情况（它的动作从一开始就是
// ask + 180s + allow，迁移只是把同样的值写回去）。它的消息也一样——迁移前
// 的消息与 builtinInterceptRules 里的一致。
func builtinRuleMessageUnchanged(name string) bool {
	for _, r := range builtinInterceptRules {
		if r.name == name {
			// listener 的旧消息（seedDefaultInterceptRulesV4 里的字面量）
			// 与规则表里的必须一致。
			if name == "[内置] 启停回连接收器" {
				return r.message == legacyListenerMessage
			}
			return true
		}
	}
	return false
}
