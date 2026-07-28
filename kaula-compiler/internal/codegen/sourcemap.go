package codegen

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type SourceMapEntry struct {
	GeneratedLine int    `json:"generated_line"`
	SourceFile    string `json:"source_file"`
	SourceLine    int    `json:"source_line"`
	SourceColumn  int    `json:"source_column"`
	Kind          string `json:"kind,omitempty"`
	SymbolName    string `json:"symbol_name,omitempty"`
}

type SourceMap struct {
	Version int              `json:"version"`
	Source  string           `json:"source"`
	Target  string           `json:"target"`
	Entries []SourceMapEntry `json:"entries"`
}

func NewSourceMap(sourceFile, targetFile string) *SourceMap {
	return &SourceMap{
		Version: 1,
		Source:  sourceFile,
		Target:  targetFile,
		Entries: []SourceMapEntry{},
	}
}

func (sm *SourceMap) AddEntry(genLine int, srcFile string, srcLine, srcCol int, kind, symbol string) {
	sm.Entries = append(sm.Entries, SourceMapEntry{
		GeneratedLine: genLine,
		SourceFile:    srcFile,
		SourceLine:    srcLine,
		SourceColumn:  srcCol,
		Kind:          kind,
		SymbolName:    symbol,
	})
}

func (sm *SourceMap) ToJSON() (string, error) {
	data, err := json.MarshalIndent(sm, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func LoadSourceMap(path string) (*SourceMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sm := &SourceMap{}
	if err := json.Unmarshal(data, sm); err != nil {
		return nil, err
	}
	return sm, nil
}

func (sm *SourceMap) LookupGenerated(genLine int) (SourceMapEntry, bool) {
	var best SourceMapEntry
	found := false
	for _, e := range sm.Entries {
		if e.GeneratedLine <= genLine {
			if !found || e.GeneratedLine > best.GeneratedLine {
				best = e
				found = true
			}
		}
	}
	return best, found
}

func (sm *SourceMap) LookupSource(srcLine int) (SourceMapEntry, bool) {
	var best SourceMapEntry
	found := false
	for _, e := range sm.Entries {
		if e.SourceLine <= srcLine {
			if !found || e.SourceLine > best.SourceLine {
				best = e
				found = true
			}
		}
	}
	return best, found
}

type MappedBuilder struct {
	builder strings.Builder
	srcMap  *SourceMap
	curLine int
	srcFile string
}

func NewMappedBuilder(sourceFile, targetFile string) *MappedBuilder {
	return &MappedBuilder{
		srcMap:  NewSourceMap(sourceFile, targetFile),
		curLine: 0,
		srcFile: sourceFile,
	}
}

func (mb *MappedBuilder) Write(s string) {
	mb.builder.WriteString(s)
}

func (mb *MappedBuilder) WriteLine(s string) {
	mb.builder.WriteString(s)
	mb.builder.WriteString("\n")
	mb.curLine++
}

func (mb *MappedBuilder) WriteLineMap(s string, srcLine, srcCol int, kind, symbol string) {
	mb.builder.WriteString(s)
	mb.builder.WriteString("\n")
	mb.curLine++
	if srcLine > 0 {
		mb.srcMap.AddEntry(mb.curLine, mb.srcFile, srcLine, srcCol, kind, symbol)
	}
}

func (mb *MappedBuilder) WriteLinesMap(s string, srcLine, srcCol int, kind, symbol string) {
	startLine := mb.curLine + 1
	mb.builder.WriteString(s)
	lines := strings.Count(s, "\n")
	if len(s) > 0 && s[len(s)-1] != '\n' {
		mb.builder.WriteString("\n")
		lines++
	}
	mb.curLine += lines
	if srcLine > 0 && lines > 0 {
		mb.srcMap.AddEntry(startLine, mb.srcFile, srcLine, srcCol, kind, symbol)
	}
}

func (mb *MappedBuilder) String() string {
	return mb.builder.String()
}

func (mb *MappedBuilder) SourceMap() *SourceMap {
	return mb.srcMap
}

func (mb *MappedBuilder) CurrentLine() int {
	return mb.curLine
}

func (mb *MappedBuilder) Grow(n int) {
	mb.builder.Grow(n)
}

var _ = fmt.Sprintf
