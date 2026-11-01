"""
Kaula GDB Pretty-Printers

为 Kaula 语言类型提供 GDB 可读的调试输出。

使用方法:
    # 在 .gdbinit 中添加:
    source /path/to/kaula_pretty_printers.py

    # 或在 GDB 会话中:
    source /path/to/kaula_pretty_printers.py
"""

import gdb


class KaulaStringPrinter:
    """打印 Kaula String 类型: struct { size_t len; char* ptr; }"""

    def __init__(self, val):
        self.val = val

    def to_string(self):
        try:
            length = int(self.val["len"])
            ptr = self.val["ptr"]
            if length == 0:
                return '""'
            # 读取内存内容
            inferior = gdb.selected_inferior()
            mem = inferior.read_memory(ptr, length)
            return mem.decode("utf-8", errors="replace")
        except Exception:
            return "(invalid String)"


class KaulaOptionPrinter:
    """打印 Kaula Option<T> 类型: struct { int tag; union { T value; } }"""

    def __init__(self, val):
        self.val = val

    def to_string(self):
        try:
            tag = int(self.val["tag"])
            if tag == 0:
                return "None"
            else:
                # Some(value) — 尝试读取 value
                try:
                    value = self.val["value"]
                    return f"Some({value})"
                except Exception:
                    return "Some(...)"
        except Exception:
            return "(invalid Option)"


class KaulaResultPrinter:
    """打印 Kaula Result<T,E> 类型: struct { int tag; union { T ok; E err; } }"""

    def __init__(self, val):
        self.val = val

    def to_string(self):
        try:
            tag = int(self.val["tag"])
            if tag == 0:
                try:
                    ok = self.val["ok"]
                    return f"Ok({ok})"
                except Exception:
                    return "Ok(...)"
            else:
                try:
                    err = self.val["err"]
                    return f"Err({err})"
                except Exception:
                    return "Err(...)"
        except Exception:
            return "(invalid Result)"


class KaulaErrorPrinter:
    """打印 Kaula Error 类型"""

    def __init__(self, val):
        self.val = val

    def to_string(self):
        try:
            code = int(self.val["code"])
            msg_ptr = self.val["msg"]["ptr"]
            msg_len = int(self.val["msg"]["len"])
            if msg_len > 0:
                inferior = gdb.selected_inferior()
                mem = inferior.read_memory(msg_ptr, msg_len)
                msg = mem.decode("utf-8", errors="replace")
                return f"Error(code={code}, msg=\"{msg}\")"
            return f"Error(code={code})"
        except Exception:
            return "(invalid Error)"


class KaulaSlicePrinter:
    """打印 Kaula 切片类型 [T]"""

    def __init__(self, val):
        self.val = val

    def to_string(self):
        try:
            ptr = self.val["ptr"]
            len_val = int(self.val["len"])
            return f"[{len_val}] (ptr={ptr})"
        except Exception:
            return "(invalid Slice)"


class KaulaIteratorPrinter:
    """打印 Kaula 迭代器"""

    def __init__(self, val):
        self.val = val

    def to_string(self):
        try:
            pos = int(self.val["pos"])
            len_val = int(self.val["len"])
            return f"Iterator(pos={pos}, len={len_val})"
        except Exception:
            return "(invalid Iterator)"


def lookup_kaula_type(val):
    """GDB pretty-printer 查找入口"""
    # 获取类型名称（去掉命名空间前缀）
    type_str = str(val.type.strip_typedefs())

    if type_str == "String":
        return KaulaStringPrinter(val)

    if type_str.startswith("Option<"):
        return KaulaOptionPrinter(val)

    if type_str.startswith("Result<"):
        return KaulaResultPrinter(val)

    if type_str == "Error":
        return KaulaErrorPrinter(val)

    # 检查是否是 Kaula 的结构体类型（通过字段判断）
    if val.type.code == gdb.TYPE_CODE_STRUCT:
        field_names = [f.name for f in val.type.fields()]
        # Option 模式: tag + value
        if "tag" in field_names and "value" in field_names:
            return KaulaOptionPrinter(val)
        # Result 模式: tag + ok + err
        if "tag" in field_names and "ok" in field_names:
            return KaulaResultPrinter(val)
        # String 模式: len + ptr
        if "len" in field_names and "ptr" in field_names:
            # 进一步检查 ptr 是否是 char*
            for f in val.type.fields():
                if f.name == "ptr":
                    inner = str(f.type.strip_typedefs())
                    if inner.endswith("*") and "char" in inner:
                        return KaulaStringPrinter(val)
                    break
        # Slice 模式: ptr + len (非 String)
        if "len" in field_names and "ptr" in field_names:
            return KaulaSlicePrinter(val)

    return None


# 注册 pretty-printer
gdb.pretty_printers.append(lookup_kaula_type)

print("Kaula GDB pretty-printers loaded.")
