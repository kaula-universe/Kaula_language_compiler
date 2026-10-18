package core

import (
	"sync"
)

// Task 表示任务
type Task struct {
	Func     func(interface{}) interface{}
	Arg      interface{}
	Priority int
}

// SimpleQueue 表示简单队列
type SimpleQueue struct {
	Tasks    []Task
	Head     int
	Tail     int
	Size     int
	Capacity int
	Mutex    sync.RWMutex
}

// NewSimpleQueue 创建一个新的简单队列
func NewSimpleQueue(capacity int) *SimpleQueue {
	return &SimpleQueue{
		Tasks:    make([]Task, capacity),
		Head:     0,
		Tail:     0,
		Size:     0,
		Capacity: capacity,
	}
}

// IsEmpty 检查队列是否为空
func (q *SimpleQueue) IsEmpty() bool {
	q.Mutex.RLock()
	defer q.Mutex.RUnlock()
	return q.Size == 0
}

// IsFull 检查队列是否已满
func (q *SimpleQueue) IsFull() bool {
	q.Mutex.RLock()
	defer q.Mutex.RUnlock()
	return q.Size == q.Capacity
}

// Enqueue 入队
func (q *SimpleQueue) Enqueue(task Task) bool {
	q.Mutex.Lock()
	defer q.Mutex.Unlock()
	if q.Size == q.Capacity {
		return false
	}
	q.Tasks[q.Tail] = task
	q.Tail = (q.Tail + 1) % q.Capacity
	q.Size++
	return true
}

// Dequeue 出队
func (q *SimpleQueue) Dequeue() (Task, bool) {
	q.Mutex.Lock()
	defer q.Mutex.Unlock()
	if q.Size == 0 {
		return Task{}, false
	}
	task := q.Tasks[q.Head]
	q.Head = (q.Head + 1) % q.Capacity
	q.Size--
	return task, true
}

// GetSize 获取队列大小
func (q *SimpleQueue) GetSize() int {
	q.Mutex.RLock()
	defer q.Mutex.RUnlock()
	return q.Size
}

// PriorityQueue 表示优先级队列
type PriorityQueue struct {
	Queues [3]*SimpleQueue // 高、中、低三级队列
	Mutex  sync.RWMutex
}

// NewPriorityQueue 创建一个新的优先级队列
func NewPriorityQueue(capacityPerQueue int) *PriorityQueue {
	return &PriorityQueue{
		Queues: [3]*SimpleQueue{
			NewSimpleQueue(capacityPerQueue), // 高优先级
			NewSimpleQueue(capacityPerQueue), // 中优先级
			NewSimpleQueue(capacityPerQueue), // 低优先级
		},
	}
}

// Add 添加任务
func (pq *PriorityQueue) Add(priority int, f func(interface{}) interface{}, arg interface{}) bool {
	pq.Mutex.Lock()
	defer pq.Mutex.Unlock()
	if priority < 0 || priority >= 3 {
		priority = 1 // 默认中优先级
	}
	return pq.Queues[priority].Enqueue(Task{
		Func:     f,
		Arg:      arg,
		Priority: priority,
	})
}

// ExecuteNext 执行下一个任务
func (pq *PriorityQueue) ExecuteNext() interface{} {
	pq.Mutex.Lock()
	defer pq.Mutex.Unlock()
	// 按优先级顺序执行任务
	for i := 0; i < 3; i++ {
		if !pq.Queues[i].IsEmpty() {
			task, ok := pq.Queues[i].Dequeue()
			if ok && task.Func != nil {
				return task.Func(task.Arg)
			}
		}
	}
	return nil
}

// BatchAdd 批量添加任务
func (pq *PriorityQueue) BatchAdd(priority int, f func(interface{}) interface{}, args []interface{}) int {
	count := 0
	for _, arg := range args {
		if pq.Add(priority, f, arg) {
			count++
		}
	}
	return count
}

// BatchExecute 批量执行任务
func (pq *PriorityQueue) BatchExecute(maxTasks int) int {
	count := 0
	for i := 0; i < maxTasks; i++ {
		if pq.ExecuteNext() != nil {
			count++
		} else {
			break
		}
	}
	return count
}

// GetSize 获取队列大小
func (pq *PriorityQueue) GetSize() int {
	pq.Mutex.Lock()
	defer pq.Mutex.Unlock()
	size := 0
	for i := 0; i < 3; i++ {
		size += pq.Queues[i].GetSize()
	}
	return size
}
