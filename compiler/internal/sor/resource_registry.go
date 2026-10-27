package sor

// ResourceTypeInfo 描述一个资源类型的元信息。
type ResourceTypeInfo struct {
	// TypeName 是资源类型的名称。
	TypeName string

	// Kind 是资源种类（如 "file", "socket", "lock", "memory" 等）。
	Kind string

	// AcquireFunc 是获取资源的函数名（可选，用于识别资源创建点）。
	AcquireFunc string

	// ReleaseFunc 是释放资源的函数名（可选，用于识别资源释放点）。
	ReleaseFunc string

	// Description 是人类可读的资源描述（用于错误信息）。
	Description string
}

// ResourceRegistry 是资源类型注册表。
// 所有需要被 SOR 追踪生命周期的资源类型都需要在这里注册。
type ResourceRegistry struct {
	types map[string]*ResourceTypeInfo
}

// NewResourceRegistry 创建一个新的资源类型注册表。
func NewResourceRegistry() *ResourceRegistry {
	r := &ResourceRegistry{
		types: make(map[string]*ResourceTypeInfo),
	}
	r.registerBuiltinResources()
	return r
}

// registerBuiltinResources 注册内建的资源类型。
func (r *ResourceRegistry) registerBuiltinResources() {
}

// Register 注册一个资源类型。
func (r *ResourceRegistry) Register(info *ResourceTypeInfo) {
	r.types[info.TypeName] = info
}

// IsResourceType 检查某个类型是否是资源类型。
func (r *ResourceRegistry) IsResourceType(typeName string) bool {
	_, ok := r.types[typeName]
	return ok
}

// GetResourceInfo 获取资源类型的元信息。
func (r *ResourceRegistry) GetResourceInfo(typeName string) (*ResourceTypeInfo, bool) {
	info, ok := r.types[typeName]
	return info, ok
}

// AllResourceTypes 返回所有已注册的资源类型名称列表。
func (r *ResourceRegistry) AllResourceTypes() []string {
	result := make([]string, 0, len(r.types))
	for name := range r.types {
		result = append(result, name)
	}
	return result
}
