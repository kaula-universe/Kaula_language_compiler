package codegen

import (
	"fmt"
	"sort"
	"strings"

	"kaula/internal/ast"
	"kaula/internal/astutil"
)

// ============================================================================
// 静态分析统一入口
// ============================================================================

// StaticAnalysisResult 静态分析结果
type StaticAnalysisResult struct {
	UninitializedVars  map[string]bool
	ErrorPathLeaks     map[string]bool
	AliasGroups        map[int][]string
	StructFieldOrigins map[string]map[string]string
	CallbackParams     map[string]map[int]bool
}

// assignmentInfo 从语句中提取赋值信息
type assignmentInfo struct {
	target   ast.Expression
	value    ast.Expression
	isAssign bool
}

func extractAssignment(stmt ast.Statement) assignmentInfo {
	exprStmt, ok := stmt.(*ast.ExpressionStatement)
	if !ok {
		return assignmentInfo{}
	}
	bin, ok := exprStmt.Expression.(*ast.BinaryExpression)
	if !ok || bin.Operator != "ASSIGN" {
		return assignmentInfo{}
	}
	return assignmentInfo{target: bin.Left, value: bin.Right, isAssign: true}
}

// RunStaticAnalysis 运行全部静态分析 pass
func (cg *CodeGenerator) RunStaticAnalysis(funcName string, body []ast.Statement, params []string, paramTypes []string) *StaticAnalysisResult {
	result := &StaticAnalysisResult{
		UninitializedVars:  make(map[string]bool),
		ErrorPathLeaks:     make(map[string]bool),
		AliasGroups:        make(map[int][]string),
		StructFieldOrigins: make(map[string]map[string]string),
		CallbackParams:     make(map[string]map[int]bool),
	}
	cg.analyzeUninitializedVars(body, params, result)
	cg.analyzeErrorPathLeaks(body, params, result)
	cg.analyzePointerAliases(body, params, result)
	cg.analyzeStructFieldOrigins(body, result)
	cg.analyzeCallbackParams(params, paramTypes, result)
	return result
}

// ============================================================================
// Pass 1: 未初始化变量检测
// ============================================================================

func (cg *CodeGenerator) analyzeUninitializedVars(body []ast.Statement, params []string, result *StaticAnalysisResult) {
	initialized := make(map[string]bool)
	for _, p := range params {
		initialized[p] = true
	}
	cg.walkStmtsForInit(body, initialized, result)
}

func (cg *CodeGenerator) walkStmtsForInit(stmts []ast.Statement, initialized map[string]bool, result *StaticAnalysisResult) {
	for _, stmt := range stmts {
		cg.walkStmtForInit(stmt, initialized, result)
	}
}

func (cg *CodeGenerator) walkStmtForInit(stmt ast.Statement, initialized map[string]bool, result *StaticAnalysisResult) {
	if stmt == nil {
		return
	}
	if ai := extractAssignment(stmt); ai.isAssign {
		if ident, ok := ai.target.(*ast.Identifier); ok {
			initialized[ident.Name] = true
		}
		cg.checkExprInit(ai.value, initialized, result)
		return
	}
	switch s := stmt.(type) {
	case *ast.VariableDeclaration:
		if s.Value != nil {
			initialized[s.Name] = true
			cg.checkExprInit(s.Value, initialized, result)
		} else {
			initialized[s.Name] = false
		}
	case *ast.IfStatement:
		cg.checkExprInit(s.Condition, initialized, result)
		thenInit := copyMap(initialized)
		cg.walkStmtsForInit(s.Body, thenInit, result)
		elseInit := copyMap(initialized)
		if len(s.Else) > 0 {
			cg.walkStmtsForInit(s.Else, elseInit, result)
		}
		for k := range thenInit {
			if thenInit[k] && elseInit[k] {
				initialized[k] = true
			}
		}
	case *ast.WhileStatement:
		cg.checkExprInit(s.Condition, initialized, result)
		bodyInit := copyMap(initialized)
		cg.walkStmtsForInit(s.Body, bodyInit, result)
	case *ast.ForInStatement:
		loopInit := copyMap(initialized)
		if s.Variable != nil {
			loopInit[s.Variable.Name] = true
		}
		cg.walkStmtsForInit(s.Body, loopInit, result)
	case *ast.ForStatement:
		loopInit := copyMap(initialized)
		if s.Init != nil {
			cg.walkStmtForInit(s.Init, loopInit, result)
		}
		cg.walkStmtsForInit(s.Body, loopInit, result)
	case *ast.BlockStatement:
		cg.walkStmtsForInit(s.Statements, initialized, result)
	case *ast.ReturnStatement:
		if s.Value != nil {
			cg.checkExprInit(s.Value, initialized, result)
		}
	case *ast.ExpressionStatement:
		cg.checkExprInit(s.Expression, initialized, result)
	case *ast.FunctionStatement:
		nestedInit := make(map[string]bool)
		for _, p := range s.Params {
			nestedInit[p] = true
		}
		cg.walkStmtsForInit(s.Body, nestedInit, result)
	default:
		refs := astutil.CollectIdentifiers([]ast.Statement{stmt})
		for name := range refs.Refs {
			if init, ok := initialized[name]; ok && !init {
				result.UninitializedVars[name] = true
			}
		}
	}
}

func (cg *CodeGenerator) checkExprInit(expr ast.Expression, initialized map[string]bool, result *StaticAnalysisResult) {
	if expr == nil {
		return
	}
	refs := astutil.CollectIdentifiersInExpr(expr)
	for name := range refs {
		if init, ok := initialized[name]; ok && !init {
			result.UninitializedVars[name] = true
		}
	}
}

// ============================================================================
// Pass 2: 错误路径资源泄漏检测
// ============================================================================

func (cg *CodeGenerator) analyzeErrorPathLeaks(body []ast.Statement, params []string, result *StaticAnalysisResult) {
	allocated := make(map[string]bool)
	cg.walkStmtsForLeaks(body, allocated, result)
}

func (cg *CodeGenerator) walkStmtsForLeaks(stmts []ast.Statement, allocated map[string]bool, result *StaticAnalysisResult) {
	for _, stmt := range stmts {
		cg.walkStmtForLeaks(stmt, allocated, result)
	}
}

func (cg *CodeGenerator) walkStmtForLeaks(stmt ast.Statement, allocated map[string]bool, result *StaticAnalysisResult) {
	if stmt == nil {
		return
	}
	if ai := extractAssignment(stmt); ai.isAssign {
		if ident, ok := ai.target.(*ast.Identifier); ok {
			if ai.value != nil && cg.isAllocationExpr(ai.value) {
				allocated[ident.Name] = true
			}
		}
		return
	}
	switch s := stmt.(type) {
	case *ast.VariableDeclaration:
		if s.Value != nil && cg.isAllocationExpr(s.Value) {
			allocated[s.Name] = true
		}
	case *ast.IfStatement:
		thenAlloc := copyMap(allocated)
		cg.walkStmtsForLeaks(s.Body, thenAlloc, result)
		elseAlloc := copyMap(allocated)
		if len(s.Else) > 0 {
			cg.walkStmtsForLeaks(s.Else, elseAlloc, result)
		}
		for k := range thenAlloc {
			if thenAlloc[k] || elseAlloc[k] {
				allocated[k] = true
			}
		}
	case *ast.ReturnStatement:
		returnedVar := ""
		if ident, ok := s.Value.(*ast.Identifier); ok {
			returnedVar = ident.Name
		}
		for varName := range allocated {
			if varName != returnedVar {
				result.ErrorPathLeaks[varName] = true
			}
		}
	case *ast.BlockStatement:
		cg.walkStmtsForLeaks(s.Statements, allocated, result)
	case *ast.WhileStatement:
		bodyAlloc := copyMap(allocated)
		cg.walkStmtsForLeaks(s.Body, bodyAlloc, result)
	case *ast.ForStatement:
		bodyAlloc := copyMap(allocated)
		cg.walkStmtsForLeaks(s.Body, bodyAlloc, result)
	case *ast.ForInStatement:
		bodyAlloc := copyMap(allocated)
		cg.walkStmtsForLeaks(s.Body, bodyAlloc, result)
	case *ast.ExpressionStatement:
		if call, ok := s.Expression.(*ast.CallExpression); ok {
			if ident, ok := call.Function.(*ast.Identifier); ok {
				if ident.Name == "free" || ident.Name == "std_free" || ident.Name == "kmm_v4_free" {
					if len(call.Args) > 0 {
						if argIdent, ok := call.Args[0].(*ast.Identifier); ok {
							delete(allocated, argIdent.Name)
						}
					}
				}
			}
		}
	}
}

func (cg *CodeGenerator) isAllocationExpr(expr ast.Expression) bool {
	call, ok := expr.(*ast.CallExpression)
	if !ok {
		return false
	}
	ident, ok := call.Function.(*ast.Identifier)
	if !ok {
		return false
	}
	allocFuncs := map[string]bool{
		"std_malloc": true, "malloc": true, "kmm_v4_alloc": true,
		"kmm_v4_alloc_auto": true, "kmm_v4_calloc": true,
		"calloc": true, "realloc": true, "new": true,
	}
	return allocFuncs[ident.Name]
}

// ============================================================================
// Pass 3: 指针别名保守分析（流不敏感）
// ============================================================================

func (cg *CodeGenerator) analyzePointerAliases(body []ast.Statement, params []string, result *StaticAnalysisResult) {
	aliasMap := make(map[string]int)
	nextGroupID := 0
	for _, p := range params {
		aliasMap[p] = nextGroupID
		result.AliasGroups[nextGroupID] = []string{p}
		nextGroupID++
	}
	cg.walkStmtsForAliases(body, aliasMap, &nextGroupID, result)
}

func (cg *CodeGenerator) walkStmtsForAliases(stmts []ast.Statement, aliasMap map[string]int, nextGroupID *int, result *StaticAnalysisResult) {
	for _, stmt := range stmts {
		cg.walkStmtForAliases(stmt, aliasMap, nextGroupID, result)
	}
}

func (cg *CodeGenerator) walkStmtForAliases(stmt ast.Statement, aliasMap map[string]int, nextGroupID *int, result *StaticAnalysisResult) {
	if stmt == nil {
		return
	}
	if ai := extractAssignment(stmt); ai.isAssign {
		if ident, ok := ai.target.(*ast.Identifier); ok {
			cg.processAliasAssignment(ident.Name, ai.value, aliasMap, nextGroupID, result)
		}
		return
	}
	switch s := stmt.(type) {
	case *ast.VariableDeclaration:
		if s.Value != nil {
			cg.processAliasAssignment(s.Name, s.Value, aliasMap, nextGroupID, result)
		} else {
			aliasMap[s.Name] = *nextGroupID
			result.AliasGroups[*nextGroupID] = []string{s.Name}
			*nextGroupID++
		}
	case *ast.IfStatement:
		thenMap := copyMapInt(aliasMap)
		cg.walkStmtsForAliases(s.Body, thenMap, nextGroupID, result)
		elseMap := copyMapInt(aliasMap)
		if len(s.Else) > 0 {
			cg.walkStmtsForAliases(s.Else, elseMap, nextGroupID, result)
		}
		for k, v := range thenMap {
			aliasMap[k] = v
		}
		for k, v := range elseMap {
			aliasMap[k] = v
		}
	case *ast.BlockStatement:
		cg.walkStmtsForAliases(s.Statements, aliasMap, nextGroupID, result)
	case *ast.WhileStatement:
		bodyMap := copyMapInt(aliasMap)
		cg.walkStmtsForAliases(s.Body, bodyMap, nextGroupID, result)
	case *ast.ForStatement:
		bodyMap := copyMapInt(aliasMap)
		cg.walkStmtsForAliases(s.Body, bodyMap, nextGroupID, result)
	case *ast.ForInStatement:
		bodyMap := copyMapInt(aliasMap)
		if s.Variable != nil {
			bodyMap[s.Variable.Name] = *nextGroupID
			result.AliasGroups[*nextGroupID] = []string{s.Variable.Name}
			*nextGroupID++
		}
		cg.walkStmtsForAliases(s.Body, bodyMap, nextGroupID, result)
	case *ast.FunctionStatement:
		nestedMap := make(map[string]int)
		for _, p := range s.Params {
			nestedMap[p] = *nextGroupID
			result.AliasGroups[*nextGroupID] = []string{p}
			*nextGroupID++
		}
		cg.walkStmtsForAliases(s.Body, nestedMap, nextGroupID, result)
	}
}

func (cg *CodeGenerator) processAliasAssignment(dst string, src ast.Expression, aliasMap map[string]int, nextGroupID *int, result *StaticAnalysisResult) {
	if srcIdent, ok := src.(*ast.Identifier); ok {
		if srcGroup, ok := aliasMap[srcIdent.Name]; ok {
			aliasMap[dst] = srcGroup
			group := result.AliasGroups[srcGroup]
			if !containsStr(group, dst) {
				result.AliasGroups[srcGroup] = append(group, dst)
			}
			return
		}
	}
	if unary, ok := src.(*ast.UnaryExpression); ok && unary.Operator == "&" {
		if operandIdent, ok := unary.Right.(*ast.Identifier); ok {
			if srcGroup, ok := aliasMap[operandIdent.Name]; ok {
				aliasMap[dst] = srcGroup
				group := result.AliasGroups[srcGroup]
				if !containsStr(group, dst) {
					result.AliasGroups[srcGroup] = append(group, dst)
				}
				return
			}
		}
	}
	aliasMap[dst] = *nextGroupID
	result.AliasGroups[*nextGroupID] = []string{dst}
	*nextGroupID++
}

// GetAliasGroup 返回变量所在的别名组
func (r *StaticAnalysisResult) GetAliasGroup(varName string) []string {
	for _, group := range r.AliasGroups {
		if containsStr(group, varName) {
			return group
		}
	}
	return []string{varName}
}

// ============================================================================
// Pass 4: 结构体字段 origin 追踪
// ============================================================================

func (cg *CodeGenerator) analyzeStructFieldOrigins(body []ast.Statement, result *StaticAnalysisResult) {
	cg.walkStmtsForStructOrigins(body, result)
}

func (cg *CodeGenerator) walkStmtsForStructOrigins(stmts []ast.Statement, result *StaticAnalysisResult) {
	for _, stmt := range stmts {
		cg.walkStmtForStructOrigins(stmt, result)
	}
}

func (cg *CodeGenerator) walkStmtForStructOrigins(stmt ast.Statement, result *StaticAnalysisResult) {
	if stmt == nil {
		return
	}
	if ai := extractAssignment(stmt); ai.isAssign {
		if member, ok := ai.target.(*ast.MemberAccessExpression); ok {
			if objIdent, ok := member.Object.(*ast.Identifier); ok {
				if ai.value != nil && cg.isAllocationExpr(ai.value) {
					structName := objIdent.Name
					if result.StructFieldOrigins[structName] == nil {
						result.StructFieldOrigins[structName] = make(map[string]string)
					}
					result.StructFieldOrigins[structName][member.Member] = cg.inferOriginFromExpr(ai.value)
				}
			}
		}
		return
	}
	switch s := stmt.(type) {
	case *ast.VariableDeclaration:
		if s.Value != nil {
			if lit, ok := s.Value.(*ast.StructLiteral); ok {
				structName := s.Name
				if result.StructFieldOrigins[structName] == nil {
					result.StructFieldOrigins[structName] = make(map[string]string)
				}
				for _, field := range lit.Fields {
					if field.Value != nil && cg.isAllocationExpr(field.Value) {
						result.StructFieldOrigins[structName][field.Name] = cg.inferOriginFromExpr(field.Value)
					}
				}
			}
		}
	case *ast.IfStatement:
		cg.walkStmtsForStructOrigins(s.Body, result)
		if len(s.Else) > 0 {
			cg.walkStmtsForStructOrigins(s.Else, result)
		}
	case *ast.BlockStatement:
		cg.walkStmtsForStructOrigins(s.Statements, result)
	case *ast.WhileStatement:
		cg.walkStmtsForStructOrigins(s.Body, result)
	case *ast.ForStatement:
		cg.walkStmtsForStructOrigins(s.Body, result)
	case *ast.ForInStatement:
		cg.walkStmtsForStructOrigins(s.Body, result)
	case *ast.FunctionStatement:
		cg.walkStmtsForStructOrigins(s.Body, result)
	}
}

func (cg *CodeGenerator) inferOriginFromExpr(expr ast.Expression) string {
	call, ok := expr.(*ast.CallExpression)
	if !ok {
		return OriginUnknown
	}
	if ident, ok := call.Function.(*ast.Identifier); ok {
		name := ident.Name
		if strings.HasPrefix(name, "kmm_") {
			return OriginKmm
		}
		if name == "std_malloc" || name == "malloc" || name == "calloc" || name == "realloc" {
			return OriginLibcMalloc
		}
		return OriginUnknown
	}
	if member, ok := call.Function.(*ast.MemberAccessExpression); ok {
		fullName := member.Member
		if objIdent, ok := member.Object.(*ast.Identifier); ok {
			fullName = objIdent.Name + "." + member.Member
		}
		if strings.Contains(fullName, "malloc") || strings.Contains(fullName, "calloc") {
			return OriginLibcMalloc
		}
		if strings.Contains(fullName, "kmm") {
			return OriginKmm
		}
	}
	return OriginUnknown
}

// GenerateStructDestructor 生成结构体析构代码
func (cg *CodeGenerator) GenerateStructDestructor(structName string, result *StaticAnalysisResult) string {
	fields, ok := result.StructFieldOrigins[structName]
	if !ok || len(fields) == 0 {
		return ""
	}
	var code strings.Builder
	code.WriteString(fmt.Sprintf("/* 结构体 %s 自动析构 */\n", structName))
	fieldNames := make([]string, 0, len(fields))
	for k := range fields {
		fieldNames = append(fieldNames, k)
	}
	sort.Strings(fieldNames)
	for _, field := range fieldNames {
		origin := fields[field]
		switch origin {
		case OriginKmm:
			code.WriteString(fmt.Sprintf("if (%s.%s != NULL) { kmm_v4_free(%s.%s); }\n", structName, field, structName, field))
		case OriginLibcMalloc:
			code.WriteString(fmt.Sprintf("if (%s.%s != NULL) { free(%s.%s); }\n", structName, field, structName, field))
		case OriginExtern:
			code.WriteString(fmt.Sprintf("/* %s.%s 是 extern 来源，不自动释放 */\n", structName, field))
		default:
			code.WriteString(fmt.Sprintf("if (%s.%s != NULL) { survivor_free(%s.%s); }\n", structName, field, structName, field))
		}
	}
	return code.String()
}

// ============================================================================
// Pass 5: 函数指针/回调参数识别
// ============================================================================

func (cg *CodeGenerator) analyzeCallbackParams(params []string, paramTypes []string, result *StaticAnalysisResult) {
	for i, ptype := range paramTypes {
		if i >= len(params) {
			break
		}
		if isFuncPointerType(ptype) {
			funcName := params[i]
			if result.CallbackParams[funcName] == nil {
				result.CallbackParams[funcName] = make(map[int]bool)
			}
			result.CallbackParams[funcName][i] = true
		}
	}
}

func isFuncPointerType(t string) bool {
	t = strings.TrimSpace(t)
	return strings.Contains(t, "(*)") || strings.Contains(t, "fn(")
}

// ============================================================================
// Pass 6: 跨函数 origin 传播（调用图分析）
// ============================================================================

// CrossFuncOriginInfo 跨函数 origin 传播信息
type CrossFuncOriginInfo struct {
	ReturnOrigin      string
	ParamReturnOrigin map[int]bool
	ConsumesParam     map[int]bool
}

// AnalyzeCrossFuncOrigins 预扫描所有函数，构建跨函数 origin 传播信息
func (cg *CodeGenerator) AnalyzeCrossFuncOrigins(program *ast.Program) map[string]*CrossFuncOriginInfo {
	info := make(map[string]*CrossFuncOriginInfo)
	if program == nil {
		return info
	}
	for _, stmt := range program.Statements {
		if fn, ok := stmt.(*ast.FunctionStatement); ok {
			cg.analyzeFuncOrigin(fn, info)
		}
	}
	return info
}

func (cg *CodeGenerator) analyzeFuncOrigin(fn *ast.FunctionStatement, info map[string]*CrossFuncOriginInfo) {
	fi := &CrossFuncOriginInfo{
		ReturnOrigin:      OriginUnknown,
		ParamReturnOrigin: make(map[int]bool),
		ConsumesParam:     make(map[int]bool),
	}
	cg.analyzeReturnOrigins(fn.Body, fn.Params, fi)
	cg.analyzeParamConsumption(fn.Body, fn.Params, fi)
	info[fn.Name] = fi
}

func (cg *CodeGenerator) analyzeReturnOrigins(stmts []ast.Statement, params []string, fi *CrossFuncOriginInfo) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.ReturnStatement:
			if s.Value != nil {
				origin := cg.inferOriginFromExpr(s.Value)
				if origin != OriginUnknown {
					fi.ReturnOrigin = origin
				}
				if ident, ok := s.Value.(*ast.Identifier); ok {
					for i, p := range params {
						if ident.Name == p {
							fi.ParamReturnOrigin[i] = true
						}
					}
				}
			}
		case *ast.IfStatement:
			cg.analyzeReturnOrigins(s.Body, params, fi)
			if len(s.Else) > 0 {
				cg.analyzeReturnOrigins(s.Else, params, fi)
			}
		case *ast.BlockStatement:
			cg.analyzeReturnOrigins(s.Statements, params, fi)
		case *ast.WhileStatement:
			cg.analyzeReturnOrigins(s.Body, params, fi)
		case *ast.ForStatement:
			cg.analyzeReturnOrigins(s.Body, params, fi)
		case *ast.ForInStatement:
			cg.analyzeReturnOrigins(s.Body, params, fi)
		}
	}
}

func (cg *CodeGenerator) analyzeParamConsumption(stmts []ast.Statement, params []string, fi *CrossFuncOriginInfo) {
	for _, stmt := range stmts {
		if ai := extractAssignment(stmt); ai.isAssign {
			if _, ok := ai.target.(*ast.MemberAccessExpression); ok {
				if ai.value != nil {
					if ident, ok := ai.value.(*ast.Identifier); ok {
						for i, p := range params {
							if ident.Name == p {
								fi.ConsumesParam[i] = true
							}
						}
					}
				}
			}
			continue
		}
		switch s := stmt.(type) {
		case *ast.IfStatement:
			cg.analyzeParamConsumption(s.Body, params, fi)
			if len(s.Else) > 0 {
				cg.analyzeParamConsumption(s.Else, params, fi)
			}
		case *ast.BlockStatement:
			cg.analyzeParamConsumption(s.Statements, params, fi)
		case *ast.WhileStatement:
			cg.analyzeParamConsumption(s.Body, params, fi)
		case *ast.ForStatement:
			cg.analyzeParamConsumption(s.Body, params, fi)
		case *ast.ForInStatement:
			cg.analyzeParamConsumption(s.Body, params, fi)
		}
	}
}

// ============================================================================
// 工具函数
// ============================================================================

func copyMap(m map[string]bool) map[string]bool {
	result := make(map[string]bool, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

func copyMapInt(m map[string]int) map[string]int {
	result := make(map[string]int, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// ReportAnalysisWarnings 生成分析警告
func (cg *CodeGenerator) ReportAnalysisWarnings(funcName string, result *StaticAnalysisResult) string {
	if result == nil {
		return ""
	}
	var warnings []string
	for varName := range result.UninitializedVars {
		warnings = append(warnings, fmt.Sprintf("/* WARNING: 变量 '%s' 可能未初始化即使用 */", varName))
	}
	for varName := range result.ErrorPathLeaks {
		warnings = append(warnings, fmt.Sprintf("/* WARNING: 变量 '%s' 在错误返回路径可能泄漏 */", varName))
	}
	if len(warnings) == 0 {
		return ""
	}
	sort.Strings(warnings)
	return "/* 静态分析警告 (" + funcName + ") */\n" + strings.Join(warnings, "\n") + "\n"
}
