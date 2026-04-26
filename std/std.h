#ifndef STD_H
#define STD_H

// 跨平台支持
#if defined(_WIN32) || defined(_WIN64)
    #define STD_PLATFORM_WINDOWS 1
    #define STD_PLATFORM_UNIX 0
#else
    #define STD_PLATFORM_WINDOWS 0
    #define STD_PLATFORM_UNIX 1
#endif

// 基础数据类型
#include "base/types.h"

// 内存管理
#include "memory/memory.h"

// 输入输出
#include "io/io.h"

// 字符串处理
#include "string/string.h"

// 国际化和多语言支持
#include "i18n/i18n.h"

// 格式化库
#include "format/format.h"

// 容器和数据结构
#include "container/container.h"

// 数学函数
#include "math/math.h"

// 时间处理
#include "time/time.h"

// 系统操作
#include "system/system.h"

// 并发和任务处理
#include "concurrent/concurrent.h"
#include "async/async.h"

// Web服务
#include "web/web.h"

// 错误处理
#include "error/error.h"

// Kaula 核心机制
#include "vo/vo.h"
#include "prefix/prefix.h"
#include "task/task.h"

// 对象系统
#include "obj/obj.h"
#include "obj/int_object_ext.h"

#endif // STD_H