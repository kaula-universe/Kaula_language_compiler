package comptime

import (
	"fmt"
	"kaula-compiler/internal/ast"
)

type ValueKind int

const (
	KindNull ValueKind = iota
	KindInt
	KindFloat
	KindBool
	KindString
)

type Value struct {
	Kind      ValueKind
	IntVal    int64
	FloatVal  float64
	BoolVal   bool
	StringVal string
}

func (v *Value) String() string {
	switch v.Kind {
	case KindInt:
		return fmt.Sprintf("%d", v.IntVal)
	case KindFloat:
		return fmt.Sprintf("%f", v.FloatVal)
	case KindBool:
		if v.BoolVal {
			return "true"
		}
		return "false"
	case KindString:
		return v.StringVal
	default:
		return "null"
	}
}

func (v *Value) TypeName() string {
	switch v.Kind {
	case KindInt:
		return "i64"
	case KindFloat:
		return "f64"
	case KindBool:
		return "bool"
	case KindString:
		return "string"
	default:
		return "null"
	}
}

type Evaluator struct {
	variables map[string]*Value
}

func NewEvaluator() *Evaluator {
	return &Evaluator{
		variables: make(map[string]*Value),
	}
}

func (e *Evaluator) Eval(expr ast.Expression) (*Value, error) {
	if expr == nil {
		return nil, fmt.Errorf("nil expression")
	}
	switch ex := expr.(type) {
	case *ast.IntegerLiteral:
		return &Value{Kind: KindInt, IntVal: ex.Value}, nil
	case *ast.FloatLiteral:
		return &Value{Kind: KindFloat, FloatVal: ex.Value}, nil
	case *ast.BooleanLiteral:
		return &Value{Kind: KindBool, BoolVal: ex.Value}, nil
	case *ast.StringLiteral:
		return &Value{Kind: KindString, StringVal: ex.Value}, nil
	case *ast.Identifier:
		return e.evalIdent(ex)
	case *ast.BinaryExpression:
		return e.evalBinary(ex)
	case *ast.UnaryExpression:
		return e.evalUnary(ex)
	case *ast.ParenExpression:
		return e.Eval(ex.Inner)
	case *ast.ConditionalExpression:
		return e.evalConditional(ex)
	case *ast.SizeOfExpression:
		return e.evalSizeOf(ex)
	case *ast.AlignOfExpression:
		return e.evalAlignOf(ex)
	case *ast.ComptimeExpression:
		return e.Eval(ex.Inner)
	default:
		return nil, fmt.Errorf("unsupported expression type for comptime evaluation: %T", expr)
	}
}

func (e *Evaluator) evalIdent(expr *ast.Identifier) (*Value, error) {
	if val, ok := e.variables[expr.Name]; ok {
		return val, nil
	}
	if expr.Name == "true" {
		return &Value{Kind: KindBool, BoolVal: true}, nil
	}
	if expr.Name == "false" {
		return &Value{Kind: KindBool, BoolVal: false}, nil
	}
	if expr.Name == "null" {
		return &Value{Kind: KindNull}, nil
	}
	return nil, fmt.Errorf("undefined comptime variable: %s", expr.Name)
}

func (e *Evaluator) evalBinary(expr *ast.BinaryExpression) (*Value, error) {
	left, err := e.Eval(expr.Left)
	if err != nil {
		return nil, err
	}
	right, err := e.Eval(expr.Right)
	if err != nil {
		return nil, err
	}
	if left.Kind == KindInt && right.Kind == KindInt {
		return e.evalBinaryInt(left.IntVal, right.IntVal, expr.Operator)
	}
	if (left.Kind == KindFloat || right.Kind == KindFloat) &&
		(left.Kind == KindInt || left.Kind == KindFloat) &&
		(right.Kind == KindInt || right.Kind == KindFloat) {
		lf := e.toFloat(left)
		rf := e.toFloat(right)
		return e.evalBinaryFloat(lf, rf, expr.Operator)
	}
	if left.Kind == KindBool && right.Kind == KindBool {
		return e.evalBinaryBool(left.BoolVal, right.BoolVal, expr.Operator)
	}
	if left.Kind == KindString || right.Kind == KindString {
		if expr.Operator == "+" {
			return &Value{
				Kind:      KindString,
				StringVal: e.toString(left) + e.toString(right),
			}, nil
		}
		if expr.Operator == "==" {
			return &Value{
				Kind:    KindBool,
				BoolVal: e.toString(left) == e.toString(right),
			}, nil
		}
		if expr.Operator == "!=" {
			return &Value{
				Kind:    KindBool,
				BoolVal: e.toString(left) != e.toString(right),
			}, nil
		}
	}
	return nil, fmt.Errorf("unsupported binary operation: %s %s %s",
		left.TypeName(), expr.Operator, right.TypeName())
}

func (e *Evaluator) evalBinaryInt(l, r int64, op string) (*Value, error) {
	switch op {
	case "+":
		return &Value{Kind: KindInt, IntVal: l + r}, nil
	case "-":
		return &Value{Kind: KindInt, IntVal: l - r}, nil
	case "*":
		return &Value{Kind: KindInt, IntVal: l * r}, nil
	case "/":
		if r == 0 {
			return nil, fmt.Errorf("comptime division by zero")
		}
		return &Value{Kind: KindInt, IntVal: l / r}, nil
	case "%":
		if r == 0 {
			return nil, fmt.Errorf("comptime modulo by zero")
		}
		return &Value{Kind: KindInt, IntVal: l % r}, nil
	case "==":
		return &Value{Kind: KindBool, BoolVal: l == r}, nil
	case "!=":
		return &Value{Kind: KindBool, BoolVal: l != r}, nil
	case "<":
		return &Value{Kind: KindBool, BoolVal: l < r}, nil
	case ">":
		return &Value{Kind: KindBool, BoolVal: l > r}, nil
	case "<=":
		return &Value{Kind: KindBool, BoolVal: l <= r}, nil
	case ">=":
		return &Value{Kind: KindBool, BoolVal: l >= r}, nil
	case "<<":
		return &Value{Kind: KindInt, IntVal: l << uint64(r)}, nil
	case ">>":
		return &Value{Kind: KindInt, IntVal: l >> uint64(r)}, nil
	case "&":
		return &Value{Kind: KindInt, IntVal: l & r}, nil
	case "|":
		return &Value{Kind: KindInt, IntVal: l | r}, nil
	case "^":
		return &Value{Kind: KindInt, IntVal: l ^ r}, nil
	case "&&":
		return &Value{Kind: KindBool, BoolVal: (l != 0) && (r != 0)}, nil
	case "||":
		return &Value{Kind: KindBool, BoolVal: (l != 0) || (r != 0)}, nil
	default:
		return nil, fmt.Errorf("unsupported integer operator: %s", op)
	}
}

func (e *Evaluator) evalBinaryFloat(l, r float64, op string) (*Value, error) {
	switch op {
	case "+":
		return &Value{Kind: KindFloat, FloatVal: l + r}, nil
	case "-":
		return &Value{Kind: KindFloat, FloatVal: l - r}, nil
	case "*":
		return &Value{Kind: KindFloat, FloatVal: l * r}, nil
	case "/":
		if r == 0 {
			return nil, fmt.Errorf("comptime division by zero")
		}
		return &Value{Kind: KindFloat, FloatVal: l / r}, nil
	case "==":
		return &Value{Kind: KindBool, BoolVal: l == r}, nil
	case "!=":
		return &Value{Kind: KindBool, BoolVal: l != r}, nil
	case "<":
		return &Value{Kind: KindBool, BoolVal: l < r}, nil
	case ">":
		return &Value{Kind: KindBool, BoolVal: l > r}, nil
	case "<=":
		return &Value{Kind: KindBool, BoolVal: l <= r}, nil
	case ">=":
		return &Value{Kind: KindBool, BoolVal: l >= r}, nil
	default:
		return nil, fmt.Errorf("unsupported float operator: %s", op)
	}
}

func (e *Evaluator) evalBinaryBool(l, r bool, op string) (*Value, error) {
	switch op {
	case "&&":
		return &Value{Kind: KindBool, BoolVal: l && r}, nil
	case "||":
		return &Value{Kind: KindBool, BoolVal: l || r}, nil
	case "==":
		return &Value{Kind: KindBool, BoolVal: l == r}, nil
	case "!=":
		return &Value{Kind: KindBool, BoolVal: l != r}, nil
	default:
		return nil, fmt.Errorf("unsupported bool operator: %s", op)
	}
}

func (e *Evaluator) evalUnary(expr *ast.UnaryExpression) (*Value, error) {
	val, err := e.Eval(expr.Right)
	if err != nil {
		return nil, err
	}
	switch expr.Operator {
	case "-":
		if val.Kind == KindInt {
			return &Value{Kind: KindInt, IntVal: -val.IntVal}, nil
		}
		if val.Kind == KindFloat {
			return &Value{Kind: KindFloat, FloatVal: -val.FloatVal}, nil
		}
	case "+":
		return val, nil
	case "!":
		if val.Kind == KindBool {
			return &Value{Kind: KindBool, BoolVal: !val.BoolVal}, nil
		}
		if val.Kind == KindInt {
			return &Value{Kind: KindBool, BoolVal: val.IntVal == 0}, nil
		}
	case "~":
		if val.Kind == KindInt {
			return &Value{Kind: KindInt, IntVal: ^val.IntVal}, nil
		}
	}
	return nil, fmt.Errorf("unsupported unary operation: %s%s", expr.Operator, val.TypeName())
}

func (e *Evaluator) evalConditional(expr *ast.ConditionalExpression) (*Value, error) {
	cond, err := e.Eval(expr.Condition)
	if err != nil {
		return nil, err
	}
	if e.isTruthy(cond) {
		return e.Eval(expr.TrueExpr)
	}
	return e.Eval(expr.FalseExpr)
}

func (e *Evaluator) evalSizeOf(expr *ast.SizeOfExpression) (*Value, error) {
	size := typeSize(expr.TargetType)
	if size < 0 {
		return nil, fmt.Errorf("unknown type for sizeof: %s", expr.TargetType)
	}
	return &Value{Kind: KindInt, IntVal: int64(size)}, nil
}

func (e *Evaluator) evalAlignOf(expr *ast.AlignOfExpression) (*Value, error) {
	align := typeAlign(expr.TargetType)
	if align < 0 {
		return nil, fmt.Errorf("unknown type for alignof: %s", expr.TargetType)
	}
	return &Value{Kind: KindInt, IntVal: int64(align)}, nil
}

func (e *Evaluator) isTruthy(v *Value) bool {
	switch v.Kind {
	case KindBool:
		return v.BoolVal
	case KindInt:
		return v.IntVal != 0
	case KindFloat:
		return v.FloatVal != 0
	case KindString:
		return len(v.StringVal) > 0
	default:
		return false
	}
}

func (e *Evaluator) toFloat(v *Value) float64 {
	if v.Kind == KindFloat {
		return v.FloatVal
	}
	if v.Kind == KindInt {
		return float64(v.IntVal)
	}
	return 0
}

func (e *Evaluator) toString(v *Value) string {
	if v.Kind == KindString {
		return v.StringVal
	}
	return v.String()
}

func typeSize(typeName string) int {
	switch typeName {
	case "i8", "u8", "char", "bool":
		return 1
	case "i16", "u16":
		return 2
	case "i32", "u32", "int", "float", "f32":
		return 4
	case "i64", "u64", "double", "f64":
		return 8
	default:
		return -1
	}
}

func typeAlign(typeName string) int {
	return typeSize(typeName)
}

func (e *Evaluator) DefineVar(name string, val *Value) {
	e.variables[name] = val
}

func (e *Evaluator) GetVar(name string) (*Value, bool) {
	val, ok := e.variables[name]
	return val, ok
}

func ToLiteral(val *Value) ast.Expression {
	switch val.Kind {
	case KindInt:
		return &ast.IntegerLiteral{Value: val.IntVal}
	case KindFloat:
		return &ast.FloatLiteral{Value: val.FloatVal}
	case KindBool:
		return &ast.BooleanLiteral{Value: val.BoolVal}
	case KindString:
		return &ast.StringLiteral{Value: val.StringVal}
	default:
		return &ast.Identifier{Name: "null"}
	}
}
