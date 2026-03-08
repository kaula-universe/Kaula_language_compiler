package codegen

import (
	"fmt"
	"io/ioutil"
	"regexp"
	"strings"
	"path/filepath"
)

// TemplateManager 表示模板管理器
type TemplateManager struct {
	templates      map[string]string
	templateDirs   []string
	cacheEnabled   bool
	cache          map[string]string
}

// NewTemplateManager 创建一个新的模板管理器
func NewTemplateManager() *TemplateManager {
	return &TemplateManager{
		templates:    make(map[string]string),
		templateDirs: []string{},
		cacheEnabled: true,
		cache:        make(map[string]string),
	}
}

// AddTemplateDir 添加模板目录
func (tm *TemplateManager) AddTemplateDir(dir string) {
	tm.templateDirs = append(tm.templateDirs, dir)
}

// LoadTemplate 加载模板
func (tm *TemplateManager) LoadTemplate(name, path string) error {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	tm.templates[name] = string(data)
	if tm.cacheEnabled {
		tm.cache[name] = string(data)
	}
	return nil
}

// LoadTemplateByName 通过名称加载模板
func (tm *TemplateManager) LoadTemplateByName(name string) error {
	// 先检查缓存
	if tm.cacheEnabled {
		if cached, ok := tm.cache[name]; ok {
			tm.templates[name] = cached
			return nil
		}
	}

	// 从模板目录中查找
	for _, dir := range tm.templateDirs {
		path := filepath.Join(dir, name+(".tmpl"))
		data, err := ioutil.ReadFile(path)
		if err == nil {
			tm.templates[name] = string(data)
			if tm.cacheEnabled {
				tm.cache[name] = string(data)
			}
			return nil
		}
	}

	return fmt.Errorf("template %s not found in any template directory", name)
}

// GetTemplate 获取模板
func (tm *TemplateManager) GetTemplate(name string) (string, bool) {
	template, ok := tm.templates[name]
	if !ok {
		// 尝试加载模板
		err := tm.LoadTemplateByName(name)
		if err == nil {
			template, ok = tm.templates[name]
		}
	}
	return template, ok
}

// SetCacheEnabled 设置缓存是否启用
func (tm *TemplateManager) SetCacheEnabled(enabled bool) {
	tm.cacheEnabled = enabled
	if !enabled {
		tm.cache = make(map[string]string)
	}
}

// ClearCache 清除缓存
func (tm *TemplateManager) ClearCache() {
	tm.cache = make(map[string]string)
}

// FillTemplate 填充模板
func (tm *TemplateManager) FillTemplate(name string, params map[string]string) (string, error) {
	template, ok := tm.GetTemplate(name)
	if !ok {
		return "", fmt.Errorf("template %s not found", name)
	}

	result := template
	
	// 处理模板继承 {{extends parent}}
	extendsRegex := regexp.MustCompile(`\{\{\s*extends\s+([^\}]+)\s*\}\}`)
	extendsMatches := extendsRegex.FindStringSubmatch(result)
	if len(extendsMatches) == 2 {
		parentName := strings.TrimSpace(extendsMatches[1])
		parentTemplate, err := tm.FillTemplate(parentName, params)
		if err == nil {
			// 移除extends指令
			result = extendsRegex.ReplaceAllString(result, "")
			// 处理块替换
			result = tm.processBlocks(result, parentTemplate)
		} else {
			// 如果父模板不存在，继续使用当前模板
		}
	}

	// 处理模板包含 {{include template}}
	includeRegex := regexp.MustCompile(`\{\{\s*include\s+([^\}]+)\s*\}\}`)
	result = includeRegex.ReplaceAllStringFunc(result, func(m string) string {
		matches := includeRegex.FindStringSubmatch(m)
		if len(matches) != 2 {
			return m
		}
		includeName := strings.TrimSpace(matches[1])
		includeContent, err := tm.FillTemplate(includeName, params)
		if err == nil {
			return includeContent
		}
		return m
	})
	
	// 处理条件语句 {{if condition}}...{{endif}}
	ifRegex := regexp.MustCompile(`\{\{\s*if\s+([^\}]+)\s*\}\}(.*?)\{\{\s*endif\s*\}\}`)
	result = ifRegex.ReplaceAllStringFunc(result, func(m string) string {
		matches := ifRegex.FindStringSubmatch(m)
		if len(matches) != 3 {
			return m
		}
		condition := strings.TrimSpace(matches[1])
		content := matches[2]
		
		if value, exists := params[condition]; exists && value != "" && value != "false" && value != "0" {
			return content
		}
		return ""
	})
	
	// 处理if-else语句 {{if condition}}...{{else}}...{{endif}}
	ifElseRegex := regexp.MustCompile(`\{\{\s*if\s+([^\}]+)\s*\}\}(.*?)\{\{\s*else\s*\}\}(.*?)\{\{\s*endif\s*\}\}`)
	result = ifElseRegex.ReplaceAllStringFunc(result, func(m string) string {
		matches := ifElseRegex.FindStringSubmatch(m)
		if len(matches) != 4 {
			return m
		}
		condition := strings.TrimSpace(matches[1])
		ifContent := matches[2]
		elseContent := matches[3]
		
		if value, exists := params[condition]; exists && value != "" && value != "false" && value != "0" {
			return ifContent
		}
		return elseContent
	})
	
	// 处理循环语句 {{each items as item}}...{{endeach}}
	eachRegex := regexp.MustCompile(`\{\{\s*each\s+([^\s]+)\s+as\s+([^\}]+)\s*\}\}(.*?)\{\{\s*endeach\s*\}\}`)
	result = eachRegex.ReplaceAllStringFunc(result, func(m string) string {
		matches := eachRegex.FindStringSubmatch(m)
		if len(matches) != 4 {
			return m
		}
		variable := strings.TrimSpace(matches[1])
		itemName := strings.TrimSpace(matches[2])
		content := matches[3]
		
		// 检查参数是否是切片或数组
		if items, exists := params[variable]; exists {
			// 简化处理，假设items是用逗号分隔的字符串
			itemList := strings.Split(items, ",")
			var result strings.Builder
			for _, item := range itemList {
				item = strings.TrimSpace(item)
				itemParams := make(map[string]string)
				for k, v := range params {
					itemParams[k] = v
				}
				itemParams[itemName] = item
				// 递归填充内容
				itemContent := content
				for key, value := range itemParams {
					placeholder := fmt.Sprintf("{{%s}}", key)
					itemContent = strings.ReplaceAll(itemContent, placeholder, value)
				}
				result.WriteString(itemContent)
			}
			return result.String()
		}
		return ""
	})
	
	// 处理变量替换 {{variable}}
	for key, value := range params {
		placeholder := fmt.Sprintf("{{%s}}", key)
		result = strings.ReplaceAll(result, placeholder, value)
	}

	return result, nil
}

// processBlocks 处理模板块替换
func (tm *TemplateManager) processBlocks(childTemplate, parentTemplate string) string {
	// 提取子模板中的所有块
	blockRegex := regexp.MustCompile(`\{\{\s*block\s+([^\}]+)\s*\}\}(.*?)\{\{\s*endblock\s*\}\}`)
	blocks := make(map[string]string)
	
	blockRegex.ReplaceAllStringFunc(childTemplate, func(m string) string {
		matches := blockRegex.FindStringSubmatch(m)
		if len(matches) == 3 {
			blockName := strings.TrimSpace(matches[1])
			blockContent := matches[2]
			blocks[blockName] = blockContent
		}
		return ""
	})

	// 替换父模板中的块
	result := parentTemplate
	for blockName, blockContent := range blocks {
		blockPattern := fmt.Sprintf(`\{\{\s*block\s+%s\s*\}\}(.*?)\{\{\s*endblock\s*\}\}` , blockName)
		blockPatternRegex := regexp.MustCompile(blockPattern)
		result = blockPatternRegex.ReplaceAllString(result, blockContent)
	}

	return result
}
