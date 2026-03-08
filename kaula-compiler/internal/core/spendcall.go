package core

import (
	"sync"
)

// Spendable 表示可消费的对象
type Spendable struct {
	Components   []interface{}
	Count        int
	CallCounter  int
	Mutex        sync.RWMutex
}

// NewSpendable 创建一个新的可消费对象
func NewSpendable(capacity int) *Spendable {
	return &Spendable{
		Components:   make([]interface{}, 0, capacity),
		Count:        0,
		CallCounter:  0,
	}
}

// Add 添加组件到可消费对象
func (sp *Spendable) Add(component interface{}) {
	sp.Mutex.Lock()
	defer sp.Mutex.Unlock()
	sp.Components = append(sp.Components, component)
	sp.Count++
}

// Call 消费一个组件
func (sp *Spendable) Call() interface{} {
	sp.Mutex.Lock()
	defer sp.Mutex.Unlock()
	if sp.CallCounter < sp.Count {
		component := sp.Components[sp.CallCounter]
		sp.CallCounter++
		// 检查是否消费完毕
		if sp.CallCounter >= sp.Count {
			// 自动释放资源
			sp.Components = nil
			sp.Count = 0
			sp.CallCounter = 0
		}
		return component
	}
	return nil
}

// IsConsumed 检查是否已消费完毕
func (sp *Spendable) IsConsumed() bool {
	sp.Mutex.RLock()
	defer sp.Mutex.RUnlock()
	return sp.CallCounter >= sp.Count
}

// GetRemaining 获取剩余未消费的组件数量
func (sp *Spendable) GetRemaining() int {
	sp.Mutex.RLock()
	defer sp.Mutex.RUnlock()
	return sp.Count - sp.CallCounter
}
