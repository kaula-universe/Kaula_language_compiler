package core

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

type PrefixAnnotation int

const (
	PrefixAnnotationNone PrefixAnnotation = iota
	PrefixAnnotationPrefix
	PrefixAnnotationTree
	PrefixAnnotationPrefixTree
)

func (pa PrefixAnnotation) String() string {
	switch pa {
	case PrefixAnnotationNone:
		return "none"
	case PrefixAnnotationPrefix:
		return "prefix"
	case PrefixAnnotationTree:
		return "tree"
	case PrefixAnnotationPrefixTree:
		return "prefix,tree"
	default:
		return "unknown"
	}
}

type PrefixVarType int

const (
	VarTypeUnknown PrefixVarType = iota
	VarTypeInt
	VarTypeFloat
	VarTypeString
	VarTypeBool
	VarTypeObject
)

func (vt PrefixVarType) String() string {
	switch vt {
	case VarTypeUnknown:
		return "unknown"
	case VarTypeInt:
		return "int"
	case VarTypeFloat:
		return "float"
	case VarTypeString:
		return "string"
	case VarTypeBool:
		return "bool"
	case VarTypeObject:
		return "object"
	default:
		return "unknown"
	}
}

type PrefixVariable struct {
	Name  string
	Type  PrefixVarType
	Value interface{}
	IsPrefixVar bool
	Pos   Position
}

func (pv *PrefixVariable) String() string {
	prefix := ""
	if pv.IsPrefixVar {
		prefix = "$"
	}
	return fmt.Sprintf("%s%s:%s", prefix, pv.Name, pv.Type.String())
}

type Position struct {
	Line   int
	Column int
	File   string
}

type PrefixContext struct {
	Name       string
	Annotation PrefixAnnotation
	Variables  map[string]*PrefixVariable
	Parent     *PrefixContext
	Children   []*PrefixContext
	Tree       *Tree
	Mutex      sync.RWMutex
}

func NewPrefixContext(name string) *PrefixContext {
	return &PrefixContext{
		Name:       name,
		Annotation: PrefixAnnotationNone,
		Variables:  make(map[string]*PrefixVariable),
		Children:   make([]*PrefixContext, 0),
	}
}

func (pc *PrefixContext) AddVariable(name string, vartype PrefixVarType, value interface{}, isPrefixVar bool, pos Position) *PrefixVariable {
	pc.Mutex.Lock()
	defer pc.Mutex.Unlock()
	pv := &PrefixVariable{
		Name:         name,
		Type:         vartype,
		Value:        value,
		IsPrefixVar:  isPrefixVar,
		Pos:          pos,
	}
	pc.Variables[name] = pv
	return pv
}

func (pc *PrefixContext) GetVariable(name string) (*PrefixVariable, bool) {
	pc.Mutex.RLock()
	defer pc.Mutex.RUnlock()
	if pv, ok := pc.Variables[name]; ok {
		return pv, true
	}
	if pc.Parent != nil {
		return pc.Parent.GetVariable(name)
	}
	return nil, false
}

func (pc *PrefixContext) HasPrefixVar(name string) bool {
	pc.Mutex.RLock()
	defer pc.Mutex.RUnlock()
	if pv, ok := pc.Variables[name]; ok && pv.IsPrefixVar {
		return true
	}
	if pc.Parent != nil {
		return pc.Parent.HasPrefixVar(name)
	}
	return false
}

func (pc *PrefixContext) SetParent(parent *PrefixContext) {
	pc.Mutex.Lock()
	defer pc.Mutex.Unlock()
	pc.Parent = parent
	parent.Children = append(parent.Children, pc)
}

func (pc *PrefixContext) GetAllVariables() []*PrefixVariable {
	pc.Mutex.RLock()
	defer pc.Mutex.RUnlock()
	vars := make([]*PrefixVariable, 0)
	pc.collectVariables(&vars)
	return vars
}

func (pc *PrefixContext) collectVariables(vars *[]*PrefixVariable) {
	for _, v := range pc.Variables {
		*vars = append(*vars, v)
	}
	if pc.Parent != nil {
		pc.Parent.collectVariables(vars)
	}
}

type PrefixCall struct {
	Name       string
	Params     map[string]interface{}
	Tree       *Tree
	Pos        Position
}

func NewPrefixCall(name string, params map[string]interface{}) *PrefixCall {
	return &PrefixCall{
		Name:   name,
		Params: params,
	}
}

type PrefixManager struct {
	contexts       map[string]*PrefixContext
	activeContext *PrefixContext
	rootContext   *PrefixContext
	calls         map[string]*PrefixCall
	Mutex         sync.RWMutex
}

func NewPrefixManager() *PrefixManager {
	root := NewPrefixContext("root")
	return &PrefixManager{
		contexts:       map[string]*PrefixContext{"root": root},
		activeContext:  root,
		rootContext:    root,
		calls:          make(map[string]*PrefixCall),
	}
}

func (pm *PrefixManager) CreatePrefix(name string, annotation PrefixAnnotation) (*PrefixContext, error) {
	pm.Mutex.Lock()
	defer pm.Mutex.Unlock()
	if _, exists := pm.contexts[name]; exists {
		return nil, fmt.Errorf("prefix '%s' already exists", name)
	}
	ctx := NewPrefixContext(name)
	ctx.Annotation = annotation
	pm.contexts[name] = ctx
	return ctx, nil
}

func (pm *PrefixManager) GetPrefix(name string) *PrefixContext {
	pm.Mutex.RLock()
	defer pm.Mutex.RUnlock()
	return pm.contexts[name]
}

func (pm *PrefixManager) SetActiveContext(name string) error {
	pm.Mutex.Lock()
	defer pm.Mutex.Unlock()
	ctx, exists := pm.contexts[name]
	if !exists {
		return fmt.Errorf("prefix '%s' not found", name)
	}
	pm.activeContext = ctx
	return nil
}

func (pm *PrefixManager) GetActiveContext() *PrefixContext {
	pm.Mutex.RLock()
	defer pm.Mutex.RUnlock()
	return pm.activeContext
}

func (pm *PrefixManager) PushContext(name string) (*PrefixContext, error) {
	pm.Mutex.Lock()
	defer pm.Mutex.Unlock()
	parent := pm.activeContext
	ctx := NewPrefixContext(name)
	ctx.Parent = parent
	ctx.Annotation = parent.Annotation
	parent.Children = append(parent.Children, ctx)
	pm.contexts[name] = ctx
	pm.activeContext = ctx
	return ctx, nil
}

func (pm *PrefixManager) PopContext() error {
	pm.Mutex.Lock()
	defer pm.Mutex.Unlock()
	if pm.activeContext == pm.rootContext {
		return fmt.Errorf("cannot pop root context")
	}
	pm.activeContext = pm.activeContext.Parent
	return nil
}

func (pm *PrefixManager) RegisterCall(call *PrefixCall) {
	pm.Mutex.Lock()
	defer pm.Mutex.Unlock()
	pm.calls[call.Name] = call
}

func (pm *PrefixManager) GetCall(name string) (*PrefixCall, bool) {
	pm.Mutex.RLock()
	defer pm.Mutex.RUnlock()
	call, ok := pm.calls[name]
	return call, ok
}

func (pm *PrefixManager) SetVariable(prefix, name string, vartype PrefixVarType, value interface{}, isPrefixVar bool, pos Position) error {
	pm.Mutex.Lock()
	defer pm.Mutex.Unlock()
	ctx, exists := pm.contexts[prefix]
	if !exists {
		return fmt.Errorf("prefix '%s' not found", prefix)
	}
	ctx.AddVariable(name, vartype, value, isPrefixVar, pos)
	return nil
}

func (pm *PrefixManager) GetVariable(prefix, name string) (*PrefixVariable, bool) {
	pm.Mutex.RLock()
	defer pm.Mutex.RUnlock()
	ctx, exists := pm.contexts[prefix]
	if !exists {
		return nil, false
	}
	return ctx.GetVariable(name)
}

func (pm *PrefixManager) HasPrefixVar(prefix, name string) bool {
	pm.Mutex.RLock()
	defer pm.Mutex.RUnlock()
	ctx, exists := pm.contexts[prefix]
	if !exists {
		return false
	}
	return ctx.HasPrefixVar(name)
}

func (pm *PrefixManager) ListPrefixes() []string {
	pm.Mutex.RLock()
	defer pm.Mutex.RUnlock()
	prefixes := make([]string, 0, len(pm.contexts))
	for name := range pm.contexts {
		prefixes = append(prefixes, name)
	}
	return prefixes
}

func (pm *PrefixManager) GetPrefixVariables(prefix string) []*PrefixVariable {
	pm.Mutex.RLock()
	defer pm.Mutex.RUnlock()
	ctx, exists := pm.contexts[prefix]
	if !exists {
		return nil
	}
	return ctx.GetAllVariables()
}

func (pm *PrefixManager) ApplyPrefixCall(callName string, target interface{}, args map[string]interface{}) error {
	pm.Mutex.RLock()
	call, ok := pm.calls[callName]
	pm.Mutex.RUnlock()
	if !ok {
		return fmt.Errorf("prefix call '%s' not found", callName)
	}
	if call.Tree == nil {
		return fmt.Errorf("prefix call '%s' has no tree", callName)
	}
	return pm.applyTreeToTarget(call.Tree, target, args)
}

func (pm *PrefixManager) applyTreeToTarget(tree *Tree, target interface{}, args map[string]interface{}) error {
	if tree == nil {
		return nil
	}
	switch t := target.(type) {
	case *TreeNode:
		return pm.applyTreeNode(tree.Root, t, args)
	}
	return nil
}

func (pm *PrefixManager) applyTreeNode(treeNode *TreeNode, target *TreeNode, args map[string]interface{}) error {
	if treeNode == nil || target == nil {
		return nil
	}
	for i, child := range treeNode.Children {
		if i < len(target.Children) {
			if err := pm.applyTreeNode(child, target.Children[i], args); err != nil {
				return err
			}
		}
	}
	return nil
}

func (pm *PrefixManager) ResolvePrefixVar(name string) (*PrefixVariable, bool, error) {
	pm.Mutex.RLock()
	defer pm.Mutex.RUnlock()
	if pm.activeContext == nil {
		return nil, false, fmt.Errorf("no active context")
	}
	pv, ok := pm.activeContext.GetVariable(name)
	return pv, ok, nil
}

func (pm *PrefixManager) ResolveAmbiguity(name string) (bool, error) {
	pm.Mutex.RLock()
	defer pm.Mutex.RUnlock()
	if pm.activeContext == nil {
		return false, fmt.Errorf("no active context")
	}
	pv, hasLocal := pm.activeContext.GetVariable(name)
	if hasLocal && pm.activeContext.Parent != nil {
		_, hasParent := pm.activeContext.Parent.GetVariable(name)
		if hasParent {
			return true, fmt.Errorf("ambiguous variable '%s': both local and parent scope have this variable, use $ prefix to disambiguate", name)
		}
	}
	if pv != nil && pv.IsPrefixVar && name[0] != '$' {
		return true, fmt.Errorf("prefix variable '%s' requires $ prefix to access", name)
	}
	return false, nil
}

func (pm *PrefixManager) GenerateInlineCode(prefixName string, args map[string]interface{}) (string, error) {
	pm.Mutex.RLock()
	defer pm.Mutex.RUnlock()
	ctx, exists := pm.contexts[prefixName]
	if !exists {
		return "", fmt.Errorf("prefix '%s' not found", prefixName)
	}
	if ctx.Annotation != PrefixAnnotationPrefix && ctx.Annotation != PrefixAnnotationNone {
		return "", fmt.Errorf("prefix '%s' is not a inline prefix", prefixName)
	}
	return pm.generateInlineRecursive(ctx, args)
}

func (pm *PrefixManager) generateInlineRecursive(ctx *PrefixContext, args map[string]interface{}) (string, error) {
	result := ""
	for name, pv := range ctx.Variables {
		if pv.IsPrefixVar {
			if val, ok := args[name]; ok {
				result += fmt.Sprintf("%s = %v;\n", name, val)
			}
		}
	}
	for _, child := range ctx.Children {
		childCode, err := pm.generateInlineRecursive(child, args)
		if err != nil {
			return "", err
		}
		result += childCode
	}
	return result, nil
}

type ExportedPrefix struct {
	Name       string                  `json:"name"`
	Annotation PrefixAnnotation         `json:"annotation"`
	Variables  []ExportedVariable      `json:"variables"`
	Calls     map[string]ExportCall   `json:"calls"`
}

type ExportedVariable struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	IsPrefixVar  bool   `json:"is_prefix_var"`
}

type ExportCall struct {
	Name   string                 `json:"name"`
	Params map[string]interface{} `json:"params"`
}

type ExportedPrefixTable struct {
	Prefixes []ExportedPrefix `json:"prefixes"`
	Version  string           `json:"version"`
}

func (pm *PrefixManager) ExportToFile(filename string) error {
	pm.Mutex.RLock()
	defer pm.Mutex.RUnlock()

	export := ExportedPrefixTable{
		Version:  "1.0",
		Prefixes: make([]ExportedPrefix, 0, len(pm.contexts)),
	}

	for name, ctx := range pm.contexts {
		if name == "root" {
			continue
		}

		ep := ExportedPrefix{
			Name:       name,
			Annotation: ctx.Annotation,
			Variables:  make([]ExportedVariable, 0),
			Calls:      make(map[string]ExportCall),
		}

		for _, v := range ctx.Variables {
			ep.Variables = append(ep.Variables, ExportedVariable{
				Name:        v.Name,
				Type:        v.Type.String(),
				IsPrefixVar: v.IsPrefixVar,
			})
		}

		for callName, call := range pm.calls {
			ep.Calls[callName] = ExportCall{
				Name:   call.Name,
				Params: call.Params,
			}
		}

		export.Prefixes = append(export.Prefixes, ep)
	}

	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal prefix table: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write prefix table: %w", err)
	}

	return nil
}

func (pm *PrefixManager) ImportFromFile(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read prefix table: %w", err)
	}

	var export ExportedPrefixTable
	if err := json.Unmarshal(data, &export); err != nil {
		return fmt.Errorf("failed to unmarshal prefix table: %w", err)
	}

	pm.Mutex.Lock()
	defer pm.Mutex.Unlock()

	for _, ep := range export.Prefixes {
		ctx, err := pm.CreatePrefix(ep.Name, ep.Annotation)
		if err != nil {
			continue
		}

		for _, ev := range ep.Variables {
			vartype := VarTypeUnknown
			switch ev.Type {
			case "int":
				vartype = VarTypeInt
			case "float":
				vartype = VarTypeFloat
			case "string":
				vartype = VarTypeString
			case "bool":
				vartype = VarTypeBool
			}
			ctx.AddVariable(ev.Name, vartype, nil, ev.IsPrefixVar, Position{})
		}

		for callName, call := range ep.Calls {
			pm.calls[callName] = &PrefixCall{
				Name:   call.Name,
				Params: call.Params,
			}
		}
	}

	return nil
}

func (pm *PrefixManager) ImportPrefix(prefix *ExportedPrefix) error {
	pm.Mutex.Lock()
	defer pm.Mutex.Unlock()

	ctx, err := pm.CreatePrefix(prefix.Name, prefix.Annotation)
	if err != nil {
		return fmt.Errorf("failed to create prefix '%s': %w", prefix.Name, err)
	}

	for _, ev := range prefix.Variables {
		vartype := VarTypeUnknown
		switch ev.Type {
		case "int":
			vartype = VarTypeInt
		case "float":
			vartype = VarTypeFloat
		case "string":
			vartype = VarTypeString
		case "bool":
			vartype = VarTypeBool
		}
		ctx.AddVariable(ev.Name, vartype, nil, ev.IsPrefixVar, Position{})
	}

	for callName, call := range prefix.Calls {
		pm.calls[callName] = &PrefixCall{
			Name:   call.Name,
			Params: call.Params,
		}
	}

	return nil
}

func (pm *PrefixManager) RegisterExternalPrefix(prefixName string, prefix *ExportedPrefix) error {
	return pm.ImportPrefix(prefix)
}

func (pm *PrefixManager) GetAllExportedPrefixes() []*ExportedPrefix {
	pm.Mutex.RLock()
	defer pm.Mutex.RUnlock()

	exports := make([]*ExportedPrefix, 0)
	for name, ctx := range pm.contexts {
		if name == "root" {
			continue
		}

		ep := &ExportedPrefix{
			Name:       name,
			Annotation: ctx.Annotation,
			Variables:  make([]ExportedVariable, 0),
			Calls:      make(map[string]ExportCall),
		}

		for _, v := range ctx.Variables {
			ep.Variables = append(ep.Variables, ExportedVariable{
				Name:        v.Name,
				Type:        v.Type.String(),
				IsPrefixVar: v.IsPrefixVar,
			})
		}

		for callName, call := range pm.calls {
			ep.Calls[callName] = ExportCall{
				Name:   call.Name,
				Params: call.Params,
			}
		}

		exports = append(exports, ep)
	}

	return exports
}
