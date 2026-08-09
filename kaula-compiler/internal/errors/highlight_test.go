package errors

import (
	"strings"
	"testing"
)

func TestBuildHighlightAutoExtends(t *testing.T) {
	src := "fn main() void {\n    foo(\n}"
	span := BuildHighlight(src, 2, 5, 0, "", ErrorSemantic, "test")
	if span.Length != 3 {
		t.Errorf("expected auto-extended highlight length 3 (foo), got %d", span.Length)
	}
	if span.Column != 5 || span.Line != 2 {
		t.Errorf("unexpected position: line=%d col=%d", span.Line, span.Column)
	}
}

func TestBuildHighlightFallback(t *testing.T) {
	span := BuildHighlight("x", 1, 3, 0, "", ErrorSyntax, "test")
	if span.Length != 1 {
		t.Errorf("expected fallback length 1, got %d", span.Length)
	}
}

func TestHighlightSourceContextExtendsCaret(t *testing.T) {
	src := "fn main() void {\n    foo(\n}"
	ctx, line, _ := ExtractSourceContext(src, 2, 5)
	err := &Error{
		Type:          ErrorSemantic,
		Message:       "test",
		Line:          2,
		Column:        5,
		SourceContext: ctx,
		SourceLine:    line,
		Highlight:     BuildHighlight(src, 2, 5, 0, "", ErrorSemantic, "test"),
	}
	out := FormatErrorWithContext(err)

	if !strings.Contains(out, "^^^") {
		t.Errorf("expected extended caret highlight in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Semantic Error") {
		t.Errorf("expected error type label in output")
	}
}

func TestFormatWarningLabel(t *testing.T) {
	err := &Error{
		Type:    ErrorWarning,
		Message: "unused variable",
		Line:    1,
		Column:  1,
	}
	out := FormatErrorWithContext(err)
	if !strings.Contains(out, "Warning") {
		t.Errorf("expected Warning label, got:\n%s", out)
	}
	if !strings.Contains(out, "unused variable") {
		t.Errorf("expected message in output")
	}
}

func TestCollectorCounts(t *testing.T) {
	ec := NewErrorCollector()
	ec.AddSemanticError("e1", 1, 1, "", "")
	ec.AddSyntaxError("e2", 2, 1, "", "")
	ec.AddWarning("w1", 3, 1, "", "")
	if ec.ErrorCount() != 2 {
		t.Errorf("ErrorCount = %d, want 2", ec.ErrorCount())
	}
	if ec.WarningCount() != 1 {
		t.Errorf("WarningCount = %d, want 1", ec.WarningCount())
	}
	if !ec.HasErrors() {
		t.Error("HasErrors() = false, want true")
	}

	ec.Clear()
	ec.AddWarning("w2", 1, 1, "", "")
	if ec.ErrorCount() != 0 {
		t.Errorf("ErrorCount = %d, want 0", ec.ErrorCount())
	}
	if ec.HasErrors() {
		t.Error("HasErrors() = true for warnings-only, want false")
	}
}

func TestCollectorAutoContext(t *testing.T) {
	src := "fn main() void {\n    foo(\n}"
	ec := NewErrorCollector()
	ec.SetSource(src)
	ec.AddSemanticError("unknown", 2, 5, "", "")
	e := ec.Errors()[0]
	if e.SourceContext == "" {
		t.Error("expected auto-filled source context")
	}
	if e.Highlight == nil {
		t.Error("expected auto-filled highlight span")
	}
	if e.SourceLine != "    foo(" {
		t.Errorf("SourceLine = %q, want %q", e.SourceLine, "    foo(")
	}
}