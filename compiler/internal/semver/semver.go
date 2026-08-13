// Package semver 提供语义化版本解析与约束匹配（Cargo 风格子集）。
// 支持约束形式：
//
//	"1.2.3"   精确版本
//	"1.2"     主.次 匹配（>=1.2.0 <1.3.0）
//	"1"       主版本匹配（>=1.0.0 <2.0.0）
//	"^1.2.3"  兼容匹配（>=1.2.3 <2.0.0；主版本 0 时: ^0.2.3 => >=0.2.3 <0.3.0）
//	"~1.2.3"  补丁匹配（>=1.2.3 <1.3.0）
//	"*"       任意版本
package semver

import (
	"fmt"
	"strconv"
	"strings"
)

// Version 解析后的语义化版本
type Version struct {
	Major int
	Minor int
	Patch int
	Raw   string
}

// Parse 解析 "1.2.3" 形式的版本字符串。
// 容忍前导 'v'（git tag 常见），忽略后缀（如 "-beta.1" 当作补丁 0 处理）。
func Parse(s string) (Version, error) {
	raw := strings.TrimSpace(s)
	raw = strings.TrimPrefix(raw, "v")
	raw = strings.TrimPrefix(raw, "V")
	// 去掉预发布/构建元数据后缀
	if i := strings.IndexAny(raw, "-+"); i >= 0 {
		raw = raw[:i]
	}
	parts := strings.Split(raw, ".")
	v := Version{Raw: s}
	if len(parts) == 0 || parts[0] == "" {
		return v, fmt.Errorf("invalid version %q", s)
	}
	var err error
	if v.Major, err = strconv.Atoi(parts[0]); err != nil {
		return v, fmt.Errorf("invalid major in %q", s)
	}
	if len(parts) > 1 {
		if v.Minor, err = strconv.Atoi(parts[1]); err != nil {
			return v, fmt.Errorf("invalid minor in %q", s)
		}
	}
	if len(parts) > 2 {
		if v.Patch, err = strconv.Atoi(parts[2]); err != nil {
			return v, fmt.Errorf("invalid patch in %q", s)
		}
	}
	return v, nil
}

// MustParse 同 Parse，失败时 panic（用于编译期常量约束）。
func MustParse(s string) Version {
	v, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return v
}

// Compare 比较两个版本：a < b 返回 -1，相等 0，a > b 返回 1。
func Compare(a, b Version) int {
	if a.Major != b.Major {
		if a.Major < b.Major {
			return -1
		}
		return 1
	}
	if a.Minor != b.Minor {
		if a.Minor < b.Minor {
			return -1
		}
		return 1
	}
	if a.Patch != b.Patch {
		if a.Patch < b.Patch {
			return -1
		}
		return 1
	}
	return 0
}

// Constraint 版本约束
type Constraint struct {
	raw     string
	kind    string // "exact" | "caret" | "tilde" | "wildcard" | "any"
	version Version
}

// ParseConstraint 解析约束字符串。
func ParseConstraint(s string) (Constraint, error) {
	raw := strings.TrimSpace(s)
	if raw == "" || raw == "*" || raw == "x" || raw == "X" {
		return Constraint{raw: raw, kind: "any"}, nil
	}
	c := Constraint{raw: raw}
	switch {
	case strings.HasPrefix(raw, "^"):
		c.kind = "caret"
		raw = strings.TrimPrefix(raw, "^")
	case strings.HasPrefix(raw, "~"):
		c.kind = "tilde"
		raw = strings.TrimPrefix(raw, "~")
	case strings.HasPrefix(raw, "="):
		c.kind = "exact"
		raw = strings.TrimPrefix(raw, "=")
	case strings.ContainsAny(raw, "xX*"):
		c.kind = "wildcard"
		// "1.x" / "1.2.x" / "1.*" → 把 x/* 段替换为 0 以便 Parse 成版本
		raw = strings.ReplaceAll(raw, "*", "x")
		raw = strings.ReplaceAll(raw, "X", "x")
		parts := strings.Split(raw, ".")
		for i, p := range parts {
			if p == "x" {
				parts[i] = "0"
			}
		}
		raw = strings.Join(parts, ".")
	default:
		// 缺省精确：只有 3 段完整数字才视为精确
		parts := strings.Split(raw, ".")
		allDigits := true
		for _, p := range parts {
			if p == "" {
				allDigits = false
				break
			}
			if _, err := strconv.Atoi(p); err != nil {
				allDigits = false
				break
			}
		}
		if allDigits && len(parts) == 3 {
			c.kind = "exact"
		} else {
			c.kind = "wildcard"
		}
	}
	v, err := Parse(raw)
	if err != nil {
		return c, fmt.Errorf("invalid constraint %q: %w", s, err)
	}
	c.version = v
	return c, nil
}

// Matches 判断版本是否满足约束。
func (c Constraint) Matches(v Version) bool {
	switch c.kind {
	case "any":
		return true
	case "exact":
		return Compare(v, c.version) == 0
	case "tilde":
		// ~1.2.3 => >=1.2.3 <1.3.0; ~1.2 => >=1.2.0 <1.3.0; ~1 => >=1.0.0 <2.0.0
		if v.Major != c.version.Major {
			return false
		}
		if v.Minor != c.version.Minor {
			return false
		}
		return v.Patch >= c.version.Patch
	case "caret":
		// ^1.2.3 => >=1.2.3 <2.0.0; ^0.2.3 => >=0.2.3 <0.3.0; ^0.0.3 => ==0.0.3
		if c.version.Major > 0 {
			return v.Major == c.version.Major &&
				(v.Minor > c.version.Minor || (v.Minor == c.version.Minor && v.Patch >= c.version.Patch))
		}
		// 主版本 0
		if c.version.Minor > 0 {
			return v.Major == 0 && v.Minor == c.version.Minor && v.Patch >= c.version.Patch
		}
		return Compare(v, c.version) == 0
	case "wildcard":
		// 看解析出的段数："1" => 只锁主版本; "1.2" => 锁主.次
		if v.Major != c.version.Major {
			return false
		}
		// 原始约束包含几个数字段
		parts := strings.Split(strings.TrimPrefix(c.raw, "="), ".")
		digits := 0
		for _, p := range parts {
			if p != "" && !strings.ContainsAny(p, "xX*") {
				if _, err := strconv.Atoi(p); err == nil {
					digits++
				}
			}
		}
		if digits >= 2 && v.Minor != c.version.Minor {
			return false
		}
		return true
	}
	return false
}

// String 返回约束原文
func (c Constraint) String() string { return c.raw }

// BestMatch 从版本列表中选出满足约束的最高版本；无匹配返回空串。
func BestMatch(constraint Constraint, versions []string) string {
	var best Version
	var bestRaw string
	haveBest := false
	for _, vs := range versions {
		v, err := Parse(vs)
		if err != nil {
			continue
		}
		if !constraint.Matches(v) {
			continue
		}
		if !haveBest || Compare(v, best) > 0 {
			best = v
			bestRaw = vs
			haveBest = true
		}
	}
	return bestRaw
}
