package core

import (
	"sync"
	"time"
)

// VOData 表示VO中的数据
type VOData struct {
	Value    interface{}
	Code     func(interface{}) interface{}
	HasCode  bool
	LastAccess int64 // 用于LRU
	CodeIndex  int   // 关联的代码索引
}

// VOModule 表示虚拟现场模块
type VOModule struct {
	DataCache []VOData
	CodeCache [][]func(interface{}) interface{}
	CacheMax  int
	Mutex     sync.RWMutex
}

// NewVOModule 创建一个新的VO模块
func NewVOModule(cacheMax int) *VOModule {
	return &VOModule{
		DataCache: make([]VOData, cacheMax),
		CodeCache: make([][]func(interface{}) interface{}, cacheMax),
		CacheMax:  cacheMax,
	}
}

// DataLoad 加载数据到VO
func (vo *VOModule) DataLoad(index int, value interface{}) {
	vo.Mutex.Lock()
	defer vo.Mutex.Unlock()
	if index >= 0 && index < vo.CacheMax {
		vo.DataCache[index] = VOData{
			Value:    value,
			HasCode:  false,
			LastAccess: time.Now().UnixNano(),
			CodeIndex:  -1,
		}
	} else {
		// 缓存已满，需要LRU淘汰
		evictIndex := vo.findLRUVictim()
		if evictIndex >= 0 {
			vo.DataCache[evictIndex] = VOData{
				Value:    value,
				HasCode:  false,
				LastAccess: time.Now().UnixNano(),
				CodeIndex:  -1,
			}
		}
	}
}

// CodeLoad 加载代码到VO
func (vo *VOModule) CodeLoad(index int, code func(interface{}) interface{}) {
	vo.Mutex.Lock()
	defer vo.Mutex.Unlock()
	if index < 0 && -index < vo.CacheMax {
		codeIndex := -index - 1
		vo.CodeCache[codeIndex] = append(vo.CodeCache[codeIndex], code)
	}
}

// Associate 关联数据和代码
func (vo *VOModule) Associate(dataIndex, codeIndex int) {
	vo.Mutex.Lock()
	defer vo.Mutex.Unlock()
	if dataIndex >= 0 && dataIndex < vo.CacheMax && codeIndex < 0 && -codeIndex < vo.CacheMax {
		vo.DataCache[dataIndex].HasCode = true
		vo.DataCache[dataIndex].CodeIndex = codeIndex
	}
}

// Access 访问VO中的数据
func (vo *VOModule) Access(index int) interface{} {
	vo.Mutex.RLock()
	defer vo.Mutex.RUnlock()
	if index >= 0 && index < vo.CacheMax {
		// 更新访问时间
		vo.DataCache[index].LastAccess = time.Now().UnixNano()
		// 执行绑定的代码
		if vo.DataCache[index].HasCode && vo.DataCache[index].CodeIndex < 0 {
			codeIndex := -vo.DataCache[index].CodeIndex - 1
			if codeIndex >= 0 && codeIndex < vo.CacheMax && len(vo.CodeCache[codeIndex]) > 0 {
				// 执行第一个绑定的代码
				return vo.CodeCache[codeIndex][0](vo.DataCache[index].Value)
			}
		}
		return vo.DataCache[index].Value
	}
	return nil
}

// GetSize 获取VO的大小
func (vo *VOModule) GetSize() int {
	return vo.CacheMax
}

// GetIndexLength 获取单项索引的长度
func (vo *VOModule) GetIndexLength(index int) int {
	vo.Mutex.RLock()
	defer vo.Mutex.RUnlock()
	if index < 0 && -index < vo.CacheMax {
		codeIndex := -index - 1
		return len(vo.CodeCache[codeIndex])
	}
	return 1 // 数据索引长度为1
}

// GetReturnValue 获取VO的返回值
func (vo *VOModule) GetReturnValue() interface{} {
	// 这里简化处理，实际应该根据VO的执行结果返回
	return vo.Access(0)
}

// findLRUVictim 寻找LRU淘汰的受害者
func (vo *VOModule) findLRUVictim() int {
	minAccess := int64(^uint64(0) >> 1) // 最大int64值
	victimIndex := -1
	for i := 0; i < vo.CacheMax; i++ {
		if vo.DataCache[i].LastAccess < minAccess {
			minAccess = vo.DataCache[i].LastAccess
			victimIndex = i
		}
	}
	return victimIndex
}
