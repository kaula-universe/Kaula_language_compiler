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

// GUI
#include "gui/gui.h"

// 错误处理
#include "error/error.h"

// Kaula 核心机制
#include "vo/vo.h"
#include "prefix/prefix.h"
#include "task/task.h"

// 对象系统 (需要单独编译)
// #include "obj/obj.h"
// #include "obj/int_object_ext.h"

// JSON 解析和序列化
#include "json/json.h"

// 正则表达式 (使用 string 模块中的正则支持)

// 加密算法
#include "crypto/crypto.h"

// 网络编程
#include "net/net.h"

// XML 解析
#include "xml/xml.h"

// TOML 配置解析
#include "toml/toml.h"

// 日志系统
#include "logging/logging.h"

// 单元测试框架
#include "testing/testing.h"

// --- 扩展模块 ---

// 算法库
#include "algorithm/algorithm.h"

// 正则表达式
#include "regex/regex.h"

// 路径处理
#include "path/path.h"

// 文件系统
#include "fs/fs.h"

// 命令行解析
#include "cli/cli.h"

// 数据库
#include "db/db.h"

// 编码转换
#include "encoding/encoding.h"

// 压缩算法
#include "compress/compress.h"

// 序列化
#include "serialize/serialize.h"

// Option/Result 类型
#include "option/option.h"

// Unicode 支持
#include "unicode/unicode.h"

// 类型特征
#include "traits/traits.h"

// 容器扩展
#include "container/container_ext.h"

// 哈希扩展
#include "hash/hash_ext.h"

// 测试框架扩展
#include "testing/testing_ext.h"

// TOML 扩展
#include "toml/toml_ext.h"

// XML 扩展
#include "xml/xml_ext.h"

// Windows 专用
#if defined(_WIN32) || defined(_WIN64)
#include "windows/windows.h"
#endif

// 系统调用
#include "syscall/syscall.h"

// --- 新增模块 ---

// 数据结构
#include "graph/graph.h"
#include "heap/heap.h"
#include "trie/trie.h"

// 时间处理扩展
#include "datetime/datetime.h"
#include "calendar/calendar.h"

// 二进制序列化
#include "protobuf/protobuf.h"
#include "msgpack/msgpack.h"

// 并行计算
#include "parallel/parallel.h"

// 安全
#include "tls/tls.h"
#include "ssh/ssh.h"

#endif // STD_H